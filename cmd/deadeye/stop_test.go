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
	state := newDaemonState(config.Default(), catalog.Catalog{}, nil)
	out := decideStop(hookio.Input{SessionID: "s1"}, state)
	if out.HookSpecificOutput != nil {
		t.Errorf("Stop fired with nothing saved yet: %+v", out)
	}
}

func TestStopShowsSummaryAfterARewrite(t *testing.T) {
	state := newDaemonState(config.Default(), catalog.Catalog{}, nil)
	state.log(logstore.Record{SessionID: "s1", Surface: "PreToolUse/Bash", Action: "rewrite", BytesBeforeEst: 30000, BytesAfter: 9600})

	out := decideStop(hookio.Input{SessionID: "s1"}, state)
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
	state := newDaemonState(config.Default(), catalog.Catalog{}, nil)
	state.log(logstore.Record{SessionID: "s1", Surface: "PreToolUse/Bash", Action: "rewrite", BytesBeforeEst: 30000, BytesAfter: 9600})

	first := decideStop(hookio.Input{SessionID: "s1"}, state)
	if first.HookSpecificOutput == nil {
		t.Fatal("expected the first Stop after a rewrite to show a summary")
	}

	second := decideStop(hookio.Input{SessionID: "s1"}, state)
	if second.HookSpecificOutput != nil {
		t.Errorf("second Stop with no new savings repeated the summary: %+v", second)
	}

	// A further rewrite should surface again with the new cumulative total.
	state.log(logstore.Record{SessionID: "s1", Surface: "PreToolUse/Bash", Action: "rewrite", BytesBeforeEst: 15000, BytesAfter: 9600})
	third := decideStop(hookio.Input{SessionID: "s1"}, state)
	if third.HookSpecificOutput == nil {
		t.Fatal("expected a new Stop summary after a second rewrite")
	}
}
