package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/signals"
)

func testCatalogForLessons() catalog.Catalog {
	return catalog.Catalog{Models: []catalog.Model{
		{ID: "cheap-id", Family: "haiku", Tier: 0},
		{ID: "mid-id", Family: "sonnet", Tier: 1},
		{ID: "top-id", Family: "opus", Tier: 2},
	}}
}

func TestCheckEscalationDetectsHigherTierRequest(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // newDaemonState seeds its outcome cache from disk -- isolate from the real ~/.deadeye
	state := newDaemonState(testCatalogForLessons(), nil)
	state.setLastRouting("s1", "shape-a", "cheap-id", "low", 0)

	checkEscalation(hookio.Input{SessionID: "s1"}, agentInput{Model: "opus"}, "shape-b", state)

	got := state.outcomesSnapshot()
	if len(got) != 1 {
		t.Fatalf("got %d outcomes, want 1", len(got))
	}
	if got[0].Kind != "escalation" || got[0].TaskShape != "shape-a" || got[0].Model != "cheap-id" {
		t.Errorf("outcome = %+v, want escalation recorded against the PRIOR decision (shape-a/cheap-id)", got[0])
	}
}

func TestCheckEscalationIgnoresSameOrLowerTier(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	state.setLastRouting("s1", "shape-a", "top-id", "high", 2)

	checkEscalation(hookio.Input{SessionID: "s1"}, agentInput{Model: "opus"}, "shape-b", state)  // same tier
	checkEscalation(hookio.Input{SessionID: "s1"}, agentInput{Model: "haiku"}, "shape-b", state) // lower tier

	if got := state.outcomesSnapshot(); len(got) != 0 {
		t.Errorf("got %d outcomes, want 0 for same/lower-tier requests", len(got))
	}
}

func TestCheckEscalationNoopWithoutPriorRouting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	checkEscalation(hookio.Input{SessionID: "new-session"}, agentInput{Model: "opus"}, "shape-a", state)
	if got := state.outcomesSnapshot(); len(got) != 0 {
		t.Errorf("got %d outcomes, want 0 with no prior routing decision to compare against", len(got))
	}
}

func TestCheckEscalationNoopWithoutExplicitModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	state.setLastRouting("s1", "shape-a", "cheap-id", "low", 0)
	checkEscalation(hookio.Input{SessionID: "s1"}, agentInput{}, "shape-b", state)
	if got := state.outcomesSnapshot(); len(got) != 0 {
		t.Errorf("got %d outcomes, want 0 when the caller didn't request a specific model", len(got))
	}
}

// TestCheckEscalationClearsLastRoutingAfterRecording is the regression
// test for C3: an outcome must be consumed once. Before this fix, nothing
// cleared lastRouting after grading it, so two consecutive explicit-model
// Agent calls recorded TWO escalation outcomes against the same single
// prior recommendation.
func TestCheckEscalationClearsLastRoutingAfterRecording(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	state.setLastRouting("s1", "shape-a", "cheap-id", "low", 0)

	checkEscalation(hookio.Input{SessionID: "s1"}, agentInput{Model: "opus"}, "shape-b", state)
	checkEscalation(hookio.Input{SessionID: "s1"}, agentInput{Model: "opus"}, "shape-b", state)

	if got := state.outcomesSnapshot(); len(got) != 1 {
		t.Errorf("got %d outcomes, want exactly 1 -- the prior decision should only be graded once", len(got))
	}
}

// TestDecideAgentRoutingSkipsLastRoutingForExplicitModel is the regression
// test for C3's other half: when the caller supplies an explicit model,
// deadeye's own recommendation was never applied, so there's nothing to
// grade against on the NEXT call. Before this fix, setLastRouting ran
// unconditionally, so two consecutive Agent calls that both passed an
// explicit model (routine for e.g. a workflow script that always pins
// model itself) recorded a phantom escalation nobody triggered.
func TestDecideAgentRoutingSkipsLastRoutingForExplicitModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	toolInput, err := json.Marshal(map[string]any{"description": "d", "prompt": "p", "model": "opus"})
	if err != nil {
		t.Fatal(err)
	}
	in := hookio.Input{SessionID: "s1", ToolName: "Agent", Cwd: t.TempDir(), ToolInput: toolInput}
	decideAgentRouting(in, config.Default(), state)

	if lr := state.getLastRouting("s1"); lr != nil {
		t.Errorf("getLastRouting = %+v, want nil -- an explicit caller model means deadeye's recommendation was never applied", lr)
	}
}

func TestTaskShapeKeyBucketsFileCount(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{{0, "0"}, {1, "1"}, {3, "2-3"}, {8, "4-8"}, {50, "9+"}}
	for _, c := range cases {
		files := make([]string, c.n)
		key := taskShapeKey(files, "add a feature", nil)
		if want := "files=" + c.want; !strings.Contains(key, want) {
			t.Errorf("taskShapeKey with %d files = %q, want it to contain %q", c.n, key, want)
		}
	}
}

func TestTaskShapeKeyReflectsTestPresence(t *testing.T) {
	withTests := taskShapeKey([]string{"a.go"}, "add x", []signals.Evidence{
		{Provider: "testpresence", Facts: map[string]any{"files_with_adjacent_test": 1}},
	})
	withoutTests := taskShapeKey([]string{"a.go"}, "add x", []signals.Evidence{
		{Provider: "testpresence", Facts: map[string]any{"files_with_adjacent_test": 0}},
	})
	if withTests == withoutTests {
		t.Errorf("task shape key did not distinguish test presence: %q == %q", withTests, withoutTests)
	}
}
