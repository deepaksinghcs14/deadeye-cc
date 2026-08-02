package main

import (
	"net"
	"os"
	"sort"
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
	payload := []byte(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"echo hi"}}`)

	for i := 0; i < n; i++ {
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
