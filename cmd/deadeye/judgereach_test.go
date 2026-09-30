package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
)

// TestJudgeVerdictReachesTheAdvisory guards the worst defect this project has
// shipped, at the level where it actually occurred.
//
// From 0.66.0 to 0.66.4, decideAgentRouting took extra return values with a
// four-value `:=` inside the `if ai.Model == ""` block. Three of the four names
// were new, so Go silently redeclared `decision` in that block's scope: the
// judge updated a block-local copy while the advisory text the user reads,
// setLastRouting, and enforce-mode's model rewrite all kept reading the raw
// pre-judge kernel decision. The AI judge -- benchmarked at 48% realized
// savings against 22% without it -- was discarded on every Agent call for
// three releases.
//
// It compiled. `go vet` was silent, because the shadow analyzer is not on by
// default. And crucially, TestJudgeVerdictReachesTheCallThatAskedForIt in
// judge_test.go PASSED throughout, because it exercises applyRoutingJudge in
// isolation and the defect was in how the caller used its return value. Only
// comparing the advisory against the recorded outcome on a live run exposed it.
//
// So this test deliberately asserts at the decideAgentRouting boundary, not
// the applyRoutingJudge one: that the judge's verdict survives all the way
// into the advisory the user sees and into what the session remembers.
func TestJudgeVerdictReachesTheAdvisory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	prev := judgeFunc
	// Counted, not just stubbed. The assertion below has to key off "did the
	// judge actually run", because when the shadowing bug is present the
	// advisory does NOT mention the judge -- so any check gated on the
	// advisory's own text short-circuits and the test passes with the bug in
	// place. That is exactly how the first version of this test failed to
	// catch a deliberately reintroduced regression.
	judgeCalls := 0
	judgeFunc = func(string) (int, bool) { judgeCalls++; return 0, true } // "tier 0" -> cheapest
	t.Cleanup(func() { judgeFunc = prev; judgeCache.Clear() })
	judgeCache.Clear()

	cat := testCatalogForLessons()
	state := newDaemonState(cat, nil)
	toolInput, err := json.Marshal(map[string]any{
		"description": "d",
		"prompt":      "an ambiguous under-specified task the cheap heuristics cannot place",
	})
	if err != nil {
		t.Fatal(err)
	}
	in := hookio.Input{SessionID: "judge-adv", ToolName: "Agent", Cwd: t.TempDir(), ToolInput: toolInput}

	cfg := config.Default()
	cfg.Mode.RoutingJudge = "on"
	out := decideAgentRouting(in, cfg, state)
	advisory := out.HookSpecificOutput.AdditionalContext

	if advisory == "" {
		t.Fatal("no advisory produced")
	}
	cheapest, ok := cat.Cheapest()
	if !ok {
		t.Skip("test catalog has no cheapest model")
	}
	// If the judge ran at all, its verdict MUST reach the advisory. Gating on
	// judgeCalls rather than on the advisory's wording is the whole point: a
	// discarded verdict leaves no trace in the text it was discarded from.
	if judgeCalls == 0 {
		t.Skip("heuristics were confident, so the judge never ran -- nothing to assert")
	}
	if !strings.Contains(advisory, cheapest.ID) {
		t.Errorf("the judge ran %dx and returned tier 0 (%s), but the advisory carries a "+
			"different model -- its verdict is being discarded:\n%s",
			judgeCalls, cheapest.ID, advisory)
	}
	if !strings.Contains(advisory, "AI judge") {
		t.Errorf("the judge ran %dx but the advisory does not credit it, so the caller "+
			"is reading a pre-judge decision:\n%s", judgeCalls, advisory)
	}
	// What the session remembers must be what it advised. setLastRouting read
	// the shadowed copy too, so a disagreement here is the same defect.
	if lr := state.getLastRouting("judge-adv"); lr != nil {
		if !strings.Contains(advisory, lr.model) {
			t.Errorf("setLastRouting recorded %q but the advisory said:\n%s", lr.model, advisory)
		}
	}
}
