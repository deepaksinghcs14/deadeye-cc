//go:build !windows

package main

import (
	"errors"
	"os/exec"
	"syscall"
)

// detach starts the daemon in its own session so it survives the hook
// process that spawned it exiting.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// processAlive reports whether pid is a live process. On Unix,
// os.FindProcess always succeeds regardless of whether the pid actually
// exists (verified: it never returns an error here) -- signal 0 is the
// real existence check: ESRCH means gone, EPERM means it exists but is
// owned by someone else (still alive, from our point of view).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
