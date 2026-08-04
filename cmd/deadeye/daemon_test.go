package main

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// TestDaemonRoundTripP95 is INV-8's benchmark: PreToolUse sits in the
// critical path of every matched tool call, so the daemon round trip must
// stay under 50ms at p95. This runs the daemon in-process (same binary,
// same code path a real hook exercises) rather than as a spawned
// subprocess -- it measures the same JSON-marshal-plus-socket-round-trip
// cost, just without process-spawn overhead on top, which only makes the
// bound tighter. See docs/verified.md V8 for the daemonless baseline this
// is compared against.
func TestDaemonRoundTripP95(t *testing.T) {
	// Unix domain socket paths are capped at ~104 bytes on macOS/BSD;
	// t.TempDir() nests under a long per-test $TMPDIR path that blows past
	// that limit and makes net.Listen fail silently (runDaemon just
	// returns). Use a short directory under /tmp directly instead.
	home, err := os.MkdirTemp("/tmp", "de")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)

	go runDaemon()
	waitForSocket(t, 2*time.Second)

	n := 30
	durations := make([]time.Duration, n)

	for i := 0; i < n; i++ {
		// Vary the command per iteration -- the benchmark measures round-trip
		// cost, and an identical consecutive command now (correctly) draws a
		// repeat-command advisory instead of "{}".
		payload := []byte(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"echo hi ` + strconv.Itoa(i) + `"}}`)
		start := time.Now()
		out := requestDaemon("PreToolUse", payload)
		durations[i] = time.Since(start)
		if string(out) != "{}" {
			t.Fatalf("round trip %d: got %q, want \"{}\"", i, out)
		}
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[int(float64(n)*0.95)]
	t.Logf("daemon round trip: min=%s median=%s p95=%s max=%s",
		durations[0], durations[n/2], p95, durations[n-1])

	if p95 > 50*time.Millisecond {
		t.Fatalf("p95 = %s, want < 50ms (INV-8)", p95)
	}
}

// TestLockIsStaleOnlyWhenDeadAndSilent is the regression test for E1: the
// old mtime-based heuristic (>60s old) called a healthy, merely-idle
// daemon's lock "stale" -- idleTimeout alone lets a daemon go 30 minutes
// without writing anything. Staleness now requires BOTH the recorded pid
// being gone AND nothing answering the socket.
func TestLockIsStaleOnlyWhenDeadAndSilent(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate meta.SocketPath() from any real daemon on this machine
	lockPath := filepath.Join(t.TempDir(), "deadeye.lock")

	// Our own pid is alive; nothing answers the (isolated, nonexistent)
	// socket. Must NOT be stale on the pid-alive signal alone.
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	if lockIsStale(lockPath) {
		t.Error("a lockfile naming our own (definitely alive) pid was reported stale")
	}

	// A definitely-dead pid (a throwaway child process that has already
	// exited) with no socket answering -- must be stale.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skip("could not run a throwaway process for a known-dead pid")
	}
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	if !lockIsStale(lockPath) {
		t.Error("a lockfile naming a dead pid with no socket answering was NOT reported stale")
	}
}

// TestUninstallStopsRunningDaemon is the regression test for E1+E3
// together: `deadeye uninstall --purge` previously could never actually
// find the running daemon's pid, because the lockfile it needed to read
// had already been deleted out from under it by the mtime-staleness bug
// (a later spawn attempt saw the live daemon's lock as "stale," deleted
// it, then deleted its own replacement on exit). With a correct lock, the
// daemon keeps its lockfile for its whole life, so uninstall can actually
// stop it and the purge doesn't race a still-running daemon recreating
// the state dir it just removed.
func TestUninstallStopsRunningDaemon(t *testing.T) {
	// Same short-/tmp-dir requirement as TestDaemonRoundTripP95 -- unix
	// socket paths are capped at ~104 bytes on macOS/BSD.
	home, err := os.MkdirTemp("/tmp", "de")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)

	go runDaemon()
	waitForSocket(t, 2*time.Second)

	runUninstall([]string{"--purge"})

	if probeAlive(meta.SocketPath()) {
		t.Fatal("daemon still answering after uninstall --purge")
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(meta.StateDir()); err == nil {
		t.Error("state dir reappeared after purge -- a still-running daemon must have recreated it")
	}
}

func waitForSocket(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", meta.SocketPath(), 10*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("daemon socket never came up")
}

// TestFreshEmptyLockIsNotStale: acquireLock creates the file then writes
// the pid -- a second daemon arriving in that window must treat the fresh
// empty lock as in-progress, not stale (the two-winners race).
func TestFreshEmptyLockIsNotStale(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	os.MkdirAll(meta.StateDir(), 0o700)
	path := meta.LockPath()
	os.WriteFile(path, nil, 0o600) // empty, mtime = now
	if lockIsStale(path) {
		t.Error("fresh empty lock treated as stale -- two-winners race is open")
	}
	old := time.Now().Add(-time.Minute)
	os.Chtimes(path, old, old)
	if !lockIsStale(path) {
		t.Error("a minute-old empty lock is a corpse and must be stale")
	}
}

// TestSocketPathFallsBackUnderLongHome: sockaddr_un caps around 104
// bytes; a long $HOME must resolve to the deterministic TempDir fallback
// instead of a path net.Listen will reject.
func TestSocketPathFallsBackUnderLongHome(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("verylongdirectoryname", 6))
	os.MkdirAll(long, 0o700)
	t.Setenv("HOME", long)
	p := meta.SocketPath()
	if len(p) > 104 {
		t.Errorf("socket path %d bytes (%s) -- bind would fail with invalid argument", len(p), p)
	}
}
