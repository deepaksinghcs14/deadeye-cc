//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detach starts the daemon as a detached process so it survives the hook
// process that spawned it exiting. 0x8 = DETACHED_PROCESS.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008}
}
