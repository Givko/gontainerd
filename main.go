package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func main() {
	newroot := "/tmp/rootfs"
	if len(os.Args) > 1 && os.Args[1] == "child" {
		fmt.Printf("In child process with PID %d\n", os.Getpid())
		// Child process can do some work here
		setupProc(newroot)

		err := syscall.Exec("/bin/sh", []string{"/bin/sh"}, os.Environ())
		if err != nil {
			fmt.Printf("Error executing bash: %v\n", err)
		}
	} else {
		rexec()
	}
}

func setupProc(newroot string) {
	putold := filepath.Join(newroot, "/.pivot_root")

	if err := syscall.Mount(newroot, newroot, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		fmt.Printf("Error bind mounting new root: %v\n", err)
		return
	}

	// Make the new root private to avoid affecting the host's mount namespace
	// We can combine it with the MS_BIND because it will be silently ignored
	// this can be tested by running cat /proc/self/mountinfo | grep /tmp/rootfs
	// inside the shell of the container. We will see masteer:1 which meens it is a slave to the host's mount namespace
	if err := syscall.Mount("", newroot, "", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
		fmt.Printf("Error remounting new root as private: %v\n", err)
		return
	}

	if err := os.MkdirAll(putold, 0700); err != nil {
		fmt.Printf("Error creating putold directory: %v\n", err)
		return
	}

	if err := syscall.PivotRoot(newroot, putold); err != nil {
		fmt.Printf("Error pivoting root: %v\n", err)
		return
	}

	if err := os.Chdir("/"); err != nil {
		fmt.Printf("Error changing directory to new root: %v\n", err)
		return
	}

	putold = "/.pivot_root"

	// Must mount proc before unmounting the old root (/.pivot_root).
	// In a user namespace, the kernel only allows mounting procfs if it can find
	// a proc mount from the parent namespace still visible in the current mount
	// namespace (fs_fully_visible check).
	// Once we detach the old root, that reference disappears and the mount fails
	// with EPERM.
	err := syscall.Mount("proc", "/proc", "proc", 0, "")
	if err != nil {
		fmt.Printf("Error mounting proc filesystem: %v\n", err)
		return
	}
	if err := syscall.Unmount(putold, syscall.MNT_DETACH); err != nil {
		fmt.Printf("Error unmounting old root: %v\n", err)
		return
	}

	if err := os.RemoveAll(putold); err != nil {
		fmt.Printf("Error removing putold directory: %v\n", err)
		return
	}
}

func rexec() {
	fmt.Printf("In parent process with PID %d\n", os.Getpid())
	cmd := exec.Command("/proc/self/exe", "child")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWNS |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWIPC |
			syscall.CLONE_NEWUSER |
			syscall.CLONE_NEWNET,
		UidMappings: []syscall.SysProcIDMap{
			{
				ContainerID: 0,
				HostID:      os.Getuid(),
				Size:        1,
			},
		},
		GidMappings: []syscall.SysProcIDMap{
			{
				ContainerID: 0,
				HostID:      os.Getgid(),
				Size:        1,
			},
		},
	}

	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin

	start_err := cmd.Start()

	if start_err != nil {
		fmt.Printf("Error starting child process: %v\n", start_err)
		return
	}
	cgroupPath := "/sys/fs/cgroup/mini-runc/test-gontainerd-" + fmt.Sprintf("%d", cmd.Process.Pid)
	if err := os.MkdirAll(cgroupPath, 0644); err != nil {
		fmt.Printf("Error creating cgroup directory: %v\n", err)
		return
	}

	// Here we can enable cgroup v2 controller for the runtime/parent cgroup which will apply to all child cgroups
	subtree_controll_path := filepath.Join("/sys/fs/cgroup/mini-runc", "cgroup.subtree_control")
	if err := os.WriteFile(subtree_controll_path, []byte("+cpu +memory"), 0644); err != nil {
		fmt.Printf("Error writing to cgroup.subtree_control: %v\n", err)
		return
	}

	// cmd.Process.Pid is the child's PID in the host namespace
	// os.Getpid() would return the parent's (runtime's) PID — wrong process
	// os.Getpid() inside the child would return 1 — wrong namespace
	hostPid := cmd.Process.Pid
	fmt.Printf("Adding process with PID %d to cgroup\n", hostPid)
	procPath := filepath.Join(cgroupPath, "cgroup.procs")
	if err := os.WriteFile(procPath, []byte(fmt.Sprintf("%d", hostPid)), 0644); err != nil {
		fmt.Printf("Error writing to cgroup.procs: %v\n", err)
		return
	}

	max_mem_bytes := "100000" // 10MB
	memoryLimitPath := filepath.Join(cgroupPath, "memory.max")
	if err := os.WriteFile(memoryLimitPath, []byte(max_mem_bytes), 0644); err != nil {
		fmt.Printf("Error writing to memory.max: %v\n", err)
		return
	}

	err := cmd.Wait()
	fmt.Println("finished successfully")
	if err != nil {
		fmt.Println("Error:", err)
	}
}
