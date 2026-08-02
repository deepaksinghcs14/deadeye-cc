package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestFailOpenOnPanic is INV-5's test: a panic anywhere downstream of
// runHookTo (here, simulated in the daemon-dial seam) must still yield the
// canonical "{}" -- a broken policy layer must never block the user's work.
func TestFailOpenOnPanic(t *testing.T) {
	orig := dialDaemon
	defer func() { dialDaemon = orig }()
	dialDaemon = func(event string, raw []byte) []byte {
		panic("simulated failure downstream of the daemon dial")
	}

	var out bytes.Buffer
	runHookTo(&out, strings.NewReader(`{"hook_event_name":"PreToolUse"}`), "PreToolUse")

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
	dialDaemon = func(event string, raw []byte) []byte {
		called = true
		return []byte(`{"hookSpecificOutput":{"additionalContext":"should not appear"}}`)
	}

	var out bytes.Buffer
	runHookTo(&out, strings.NewReader(`{}`), "PreToolUse")

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
	dialDaemon = func(event string, raw []byte) []byte { return nil }

	var out bytes.Buffer
	runHookTo(&out, strings.NewReader(`{}`), "PreToolUse")

	if out.String() != "{}" {
		t.Fatalf("output = %q, want \"{}\"", out.String())
	}
}
