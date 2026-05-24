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
	syscall.Mount("proc", filepath.Join(newroot, "/proc"), "proc", 0, "")
	putold := filepath.Join(newroot, "/.pivot_root")

	if err := syscall.Mount(newroot, newroot, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		fmt.Printf("Error bind mounting new root: %v\n", err)
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

	if err := os.MkdirAll("/sys/fs/cgroup/system.slice/test-gontainerd", 0777); err != nil {
		fmt.Printf("Error creating cgroup directory: %v\n", err)
		return
	}

	fmt.Printf("Adding process with PID %d to cgroup\n", os.Getpid())
	if err := os.WriteFile("/sys/fs/cgroup/system.slice/test-gontainerd/cgroup.procs", []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
		fmt.Printf("Error writing to cgroup.procs: %v\n", err)
		return
	}

	err := cmd.Run()
	if err != nil {
		fmt.Println("Error:", err)
	}
}
