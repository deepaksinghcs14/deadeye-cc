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
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Signal(os.Interrupt)
	time.Sleep(100 * time.Millisecond)
}
