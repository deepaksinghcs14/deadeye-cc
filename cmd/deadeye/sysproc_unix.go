//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detach starts the daemon in its own session so it survives the hook
// process that spawned it exiting.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
