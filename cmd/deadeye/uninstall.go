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

	stopDaemon()

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

// stopDaemon reads the pid the daemon wrote to its lockfile and asks it to
// exit. Best-effort: if the lockfile is stale or the signal isn't
// supported (Windows), the caller still proceeds -- purge removes the
// socket file regardless, and a dead daemon holding no lock is harmless.
func stopDaemon() {
	b, err := os.ReadFile(meta.LockPath())
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return
	}
	// On Unix, os.FindProcess(pid) never errors regardless of whether pid
	// actually exists -- it's not a real existence check on this
	// platform. Gate the signal on the socket actually answering instead:
	// without this, a daemon that was OOM-killed (lockfile survives) and
	// whose pid was later recycled by the OS for an unrelated process
	// would get SIGINT'd by `deadeye uninstall`.
	if !probeAlive(meta.SocketPath()) {
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Signal(os.Interrupt)

	// Poll rather than a fixed sleep, so --purge's os.RemoveAll below
	// doesn't race a daemon that hasn't actually exited yet -- a
	// still-running daemon's next logstore.Append does MkdirAll and would
	// recreate the state dir the purge just removed.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !probeAlive(meta.SocketPath()) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}
