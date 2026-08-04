package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// runUninstall backs `deadeye uninstall [--purge]`. Without --purge it just
// stops the daemon and drops the socket; --purge also removes ~/.deadeye
// (config, catalog override, decision log, captures).
func runUninstall(args []string) {
	purge := false
	for _, a := range args {
		if a == "--purge" {
			purge = true
		}
	}

	stopped, pid := stopDaemon()
	if !stopped {
		// Removing the socket (or the whole state dir) out from under a
		// daemon that's still alive orphans it and lies about the result.
		fmt.Fprintf(os.Stderr, "deadeye uninstall: daemon (pid %d) did not exit within 2s -- nothing removed; retry, or kill the process manually\n", pid)
		os.Exit(1)
	}

	if purge {
		if err := os.RemoveAll(meta.StateDir()); err != nil {
			fmt.Fprintln(os.Stderr, "deadeye uninstall:", err)
			os.Exit(1)
		}
		fmt.Println("removed", meta.StateDir())
		return
	}

	os.Remove(meta.SocketPath())
	fmt.Println("stopped daemon; state kept at", meta.StateDir(), "(use --purge to remove it)")
}

// stopDaemon reads the pid the daemon wrote to its lockfile and asks it
// to exit. Returns stopped=true only when the daemon is confirmed gone
// (socket silent) -- the caller must not destroy state a live daemon
// still owns.
func stopDaemon() (stopped bool, pid int) {
	b, err := os.ReadFile(meta.LockPath())
	if err != nil {
		return !probeAlive(meta.SocketPath()), 0
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return !probeAlive(meta.SocketPath()), 0
	}
	// On Unix, os.FindProcess(pid) never errors regardless of whether pid
	// actually exists -- it's not a real existence check on this
	// platform. Gate the signal on the socket actually answering instead:
	// without this, a daemon that was OOM-killed (lockfile survives) and
	// whose pid was later recycled by the OS for an unrelated process
	// would get SIGINT'd by `deadeye uninstall`.
	if !probeAlive(meta.SocketPath()) {
		return true, pid
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, pid
	}
	_ = proc.Signal(os.Interrupt)

	// Poll rather than a fixed sleep, so --purge's os.RemoveAll below
	// doesn't race a daemon that hasn't actually exited yet -- a
	// still-running daemon's next logstore.Append does MkdirAll and would
	// recreate the state dir the purge just removed.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !probeAlive(meta.SocketPath()) {
			return true, pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false, pid
}
