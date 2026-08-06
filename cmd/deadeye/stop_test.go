package main

import (
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
)

func TestStopStaysQuietWithNoSavingsYet(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	out := decideStop(hookio.Input{SessionID: "s1"}, config.Default(), state)
	if out.HookSpecificOutput != nil {
		t.Errorf("Stop fired with nothing saved yet: %+v", out)
	}
}

func TestStopShowsSummaryAfterARewrite(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	state.log(logstore.Record{SessionID: "s1", Surface: "PreToolUse/Bash", Action: "rewrite", BytesBeforeEst: 30000, BytesAfter: 9600})

	out := decideStop(hookio.Input{SessionID: "s1"}, config.Default(), state)
	if out.HookSpecificOutput == nil {
		t.Fatal("expected a Stop summary after a real rewrite")
	}
	if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "20400") &&
		!strings.Contains(out.HookSpecificOutput.AdditionalContext, "20,400") {
		t.Errorf("summary = %q, want it to mention the 20400 bytes saved (30000-9600)", out.HookSpecificOutput.AdditionalContext)
	}
}

// TestStopDoesNotRepeatAStaleTotal is the "subtle, blends in" requirement:
// once shown, the same cumulative total must not repeat on a later turn
// where nothing new was saved.
func TestStopDoesNotRepeatAStaleTotal(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	state.log(logstore.Record{SessionID: "s1", Surface: "PreToolUse/Bash", Action: "rewrite", BytesBeforeEst: 30000, BytesAfter: 9600})

	first := decideStop(hookio.Input{SessionID: "s1"}, config.Default(), state)
	if first.HookSpecificOutput == nil {
		t.Fatal("expected the first Stop after a rewrite to show a summary")
	}

	second := decideStop(hookio.Input{SessionID: "s1"}, config.Default(), state)
	if second.HookSpecificOutput != nil {
		t.Errorf("second Stop with no new savings repeated the summary: %+v", second)
	}

	// A further rewrite should surface again with the new cumulative total.
	state.log(logstore.Record{SessionID: "s1", Surface: "PreToolUse/Bash", Action: "rewrite", BytesBeforeEst: 15000, BytesAfter: 9600})
	third := decideStop(hookio.Input{SessionID: "s1"}, config.Default(), state)
	if third.HookSpecificOutput == nil {
		t.Fatal("expected a new Stop summary after a second rewrite")
	}
}

// TestCompactTimingAdvises: once ~compactAdviseBytes of tool responses
// have arrived, the NEXT Stop (a natural task boundary) carries the
// one-per-cycle /compact suggestion -- and a compact resets the cycle so
// a long session can be advised again after re-accumulating.
func TestCompactTimingAdvises(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	cfg := config.Default()

	// Below threshold: silent.
	state.noteArrivalBytes("s1", compactAdviseBytes/2)
	if out := decideStop(hookio.Input{SessionID: "s1"}, cfg, state); out.HookSpecificOutput != nil {
		t.Errorf("below-threshold Stop advised: %+v", out.HookSpecificOutput)
	}

	// Crossing fires once at the next Stop.
	state.noteArrivalBytes("s1", compactAdviseBytes/2+1024)
	out := decideStop(hookio.Input{SessionID: "s1"}, cfg, state)
	if out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "/compact now") {
		t.Fatalf("threshold crossing not advised: %+v", out.HookSpecificOutput)
	}

	// Second Stop in the same cycle: silent.
	state.noteArrivalBytes("s1", 4096)
	if out := decideStop(hookio.Input{SessionID: "s1"}, cfg, state); out.HookSpecificOutput != nil {
		t.Errorf("same cycle re-advised: %+v", out.HookSpecificOutput)
	}

	// A compact resets the cycle; re-accumulating re-advises.
	state.resetArrivalTracking("s1")
	state.noteArrivalBytes("s1", compactAdviseBytes+1)
	if out := decideStop(hookio.Input{SessionID: "s1"}, cfg, state); out.HookSpecificOutput == nil {
		t.Error("post-compact re-accumulation not advised")
	}
}

// TestCompactTimingJoinsSavingsLine: both Stop lines share the single
// additionalContext slot, savings first.
func TestCompactTimingJoinsSavingsLine(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	state.log(logstore.Record{SessionID: "s1", Surface: "PreToolUse/Bash", Action: "rewrite", BytesBeforeEst: 30000, BytesAfter: 9600})
	state.noteArrivalBytes("s1", compactAdviseBytes+1)

	out := decideStop(hookio.Input{SessionID: "s1"}, config.Default(), state)
	if out.HookSpecificOutput == nil {
		t.Fatal("expected a combined Stop context")
	}
	ctx := out.HookSpecificOutput.AdditionalContext
	saved := strings.Index(ctx, "kept out of context")
	compact := strings.Index(ctx, "/compact now")
	if saved < 0 || compact < 0 || saved > compact {
		t.Errorf("combined Stop context wrong (savings first, then compact): %q", ctx)
	}
}

func TestCompactTimingStaysQuiet(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)

	// Muted: silent.
	state.setMuted("s1", true)
	state.noteArrivalBytes("s1", compactAdviseBytes+1)
	if out := decideStop(hookio.Input{SessionID: "s1"}, config.Default(), state); out.HookSpecificOutput != nil {
		t.Errorf("muted session advised: %+v", out.HookSpecificOutput)
	}

	// Rule disabled: silent.
	cfg := config.Default()
	cfg.Preprocess.DisabledRules = []string{"compact-timing"}
	state.noteArrivalBytes("s2", compactAdviseBytes+1)
	if out := decideStop(hookio.Input{SessionID: "s2"}, cfg, state); out.HookSpecificOutput != nil {
		t.Errorf("disabled rule advised: %+v", out.HookSpecificOutput)
	}

	// Preprocess off: silent.
	cfg = config.Default()
	cfg.Mode.Preprocess = "off"
	state.noteArrivalBytes("s3", compactAdviseBytes+1)
	if out := decideStop(hookio.Input{SessionID: "s3"}, cfg, state); out.HookSpecificOutput != nil {
		t.Errorf("preprocess-off advised: %+v", out.HookSpecificOutput)
	}
}

// TestSessionEndEvictsSessionState is the regression test for E4: nothing
// ever removed a session's in-memory state, so across a long-lived daemon
// (idle timeout resets on every connection, so in practice a daily user's
// daemon runs for as long as the machine is up) every session id the
// machine has ever seen accumulated here forever.
func TestSessionEndEvictsSessionState(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	state.markInjectedIfFirst("s1") // any call that touches session state
	if len(state.sessions) != 1 {
		t.Fatalf("expected 1 session tracked before SessionEnd, got %d", len(state.sessions))
	}

	decideSessionEnd(hookio.Input{SessionID: "s1", Cwd: t.TempDir()}, config.Default(), state)

	if len(state.sessions) != 0 {
		t.Errorf("expected session state evicted after SessionEnd, got %d entries remaining", len(state.sessions))
	}
}
