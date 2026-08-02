//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// detach starts the daemon as a detached process so it survives the hook
// process that spawned it exiting. 0x8 = DETACHED_PROCESS.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008}
}

// processAlive reports whether pid is a live process. Unlike Unix,
// os.FindProcess on Windows actually opens a handle via OpenProcess and
// errors if the pid doesn't exist, so the lookup itself is the check.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	proc.Release()
	return true
}
