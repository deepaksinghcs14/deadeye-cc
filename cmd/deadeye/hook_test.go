package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

// TestRequestTimeoutScalesWithPayloadSize is the regression test for a bug
// where the client's connection deadline was a fixed 200ms regardless of
// payload size, so a large-but-legitimate request (well within the
// daemon's own maxRequestBytes cap) reproducibly timed out and silently
// fell back to "{}" even though the daemon was healthy and answered fine
// given a moment longer.
func TestRequestTimeoutScalesWithPayloadSize(t *testing.T) {
	if d := requestTimeout(0); d != 200*time.Millisecond {
		t.Errorf("requestTimeout(0) = %v, want exactly the 200ms floor", d)
	}
	small := requestTimeout(1024)
	if small < 200*time.Millisecond || small > 210*time.Millisecond {
		t.Errorf("requestTimeout(1KB) = %v, want close to the 200ms floor", small)
	}
	large := requestTimeout(6 << 20) // 6MB -- the size that reproduced the bug live
	if large <= small {
		t.Errorf("requestTimeout(6MB) = %v, want more than requestTimeout(1KB) = %v", large, small)
	}
	if d := requestTimeout(100 << 20); d != 5*time.Second {
		t.Errorf("requestTimeout(100MB) = %v, want capped at the 5s ceiling", d)
	}
}

// TestFailOpenOnPanic is INV-5's test: a panic anywhere downstream of
// runHookTo (here, simulated in the daemon-dial seam) must still yield the
// canonical "{}" -- a broken policy layer must never block the user's work.
func TestFailOpenOnPanic(t *testing.T) {
	orig := dialDaemon
	defer func() { dialDaemon = orig }()
	dialDaemon = func(event string, raw []byte, host string) []byte {
		panic("simulated failure downstream of the daemon dial")
	}

	var out bytes.Buffer
	runHookTo(&out, strings.NewReader(`{"hook_event_name":"PreToolUse"}`), "PreToolUse", "")

	if out.String() != "{}" {
		t.Fatalf("output = %q, want \"{}\"", out.String())
	}
}

// TestKillSwitchSkipsDaemon is INV-6's per-invocation bypass: DEADEYE=off
// must short-circuit before ever dialing the daemon.
func TestKillSwitchSkipsDaemon(t *testing.T) {
	t.Setenv("DEADEYE", "off")

	called := false
	orig := dialDaemon
	defer func() { dialDaemon = orig }()
	dialDaemon = func(event string, raw []byte, host string) []byte {
		called = true
		return []byte(`{"hookSpecificOutput":{"additionalContext":"should not appear"}}`)
	}

	var out bytes.Buffer
	runHookTo(&out, strings.NewReader(`{}`), "PreToolUse", "")

	if out.String() != "{}" {
		t.Fatalf("output = %q, want \"{}\"", out.String())
	}
	if called {
		t.Fatal("dialDaemon was called despite DEADEYE=off")
	}
}

// TestEmptyDaemonResponseFallsBackToBraces covers a daemon that returns a
// zero-length response (e.g. connection dropped mid-read) -- must still
// print "{}", not nothing.
func TestEmptyDaemonResponseFallsBackToBraces(t *testing.T) {
	orig := dialDaemon
	defer func() { dialDaemon = orig }()
	dialDaemon = func(event string, raw []byte, host string) []byte { return nil }

	var out bytes.Buffer
	runHookTo(&out, strings.NewReader(`{}`), "PreToolUse", "")

	if out.String() != "{}" {
		t.Fatalf("output = %q, want \"{}\"", out.String())
	}
}

// TestColdStartSessionStartWaitsForDaemon is the regression test for a
// bug caught live: the first real session after a daemon exit answered
// "Unknown" to its own coder level -- SessionStart hit the dead socket,
// failed open to {}, and the whole session ran without the persona.
// SessionStart (and only SessionStart) must wait for the daemon it just
// spawned and retry.
func TestColdStartSessionStartWaitsForDaemon(t *testing.T) {
	// Short /tmp HOME: unix socket paths are capped ~104 bytes on macOS.
	home, err := os.MkdirTemp("/tmp", "de")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)

	// Stub the spawn: bring the daemon up ~300ms later, in-process --
	// the real spawn would detach a copy of the TEST binary.
	origSpawn := spawnDaemonFn
	defer func() { spawnDaemonFn = origSpawn }()
	spawned := false
	spawnDaemonFn = func() {
		spawned = true
		go func() {
			time.Sleep(300 * time.Millisecond)
			go runDaemon()
		}()
	}

	out := requestDaemon("SessionStart", []byte(`{"session_id":"cold1","hook_event_name":"SessionStart"}`), "")
	if !spawned {
		t.Fatal("dial against a dead socket did not spawn the daemon")
	}
	if string(out) == "{}" {
		t.Fatal("SessionStart failed open on a cold daemon -- the persona was lost for the whole session")
	}
	if !strings.Contains(string(out), "DEADEYE CODER ACTIVE") {
		t.Errorf("cold-start SessionStart returned %.60q, want the persona injection", out)
	}
}

// TestColdStartHotPathStaysFailOpen: the retry is SessionStart-only --
// PreToolUse sits in every tool call's critical path and must keep
// returning {} immediately on a dead socket (INV-8).
func TestColdStartHotPathStaysFailOpen(t *testing.T) {
	home, err := os.MkdirTemp("/tmp", "de")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)

	origSpawn := spawnDaemonFn
	defer func() { spawnDaemonFn = origSpawn }()
	spawnDaemonFn = func() {} // no daemon will ever come up

	start := time.Now()
	out := requestDaemon("PreToolUse", []byte(`{}`), "")
	elapsed := time.Since(start)
	if string(out) != "{}" {
		t.Errorf("hot path on dead socket = %q, want {}", out)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("hot path blocked %v on a dead socket -- must fail open immediately", elapsed)
	}
}
