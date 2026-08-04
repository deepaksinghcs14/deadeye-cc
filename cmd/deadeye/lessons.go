package main

import (
	"fmt"
	"os"

	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
	"github.com/deepaksinghcs14/deadeye-cc/internal/signals"
)

func fileCountBucket(n int) string {
	switch {
	case n == 0:
		return "0"
	case n == 1:
		return "1"
	case n <= 3:
		return "2-3"
	case n <= 8:
		return "4-8"
	default:
		return "9+"
	}
}

// taskShapeKey is PLAN.md §8's coarse categorical feature vector,
// collapsed to a short string: file-count bucket, mechanical vs.
// implementation-shaped, and whether adjacent tests exist. Coarse on
// purpose -- priors need density, not precision.
func taskShapeKey(files []string, prompt string, evidence []signals.Evidence) string {
	hasTests := false
	for _, e := range evidence {
		if e.Provider != "testpresence" {
			continue
		}
		if n, ok := e.Facts["files_with_adjacent_test"].(int); ok && n > 0 {
			hasTests = true
		}
	}
	return fmt.Sprintf("files=%s,impl=%v,tests=%v", fileCountBucket(len(files)), looksImplementationShaped(prompt), hasTests)
}

// checkEscalation is Phase 6's one detectable outcome signal (PLAN.md §8):
// if the caller explicitly requests a higher-tier model than deadeye's
// last recommendation for this session, that's evidence the last
// recommendation was too conservative-cheap for this kind of task.
// Recorded against the PRIOR decision's task shape, per the plan's table
// ("1.0 negative on assigned cell").
func checkEscalation(in hookio.Input, ai agentInput, currentShape string, state *daemonState) {
	if ai.Model == "" {
		return // caller didn't request a specific model -- nothing to compare
	}
	prev := state.getLastRouting(in.SessionID)
	if prev == nil {
		return
	}
	requestedTier, ok := state.cat.TierFor(familyToAnyModelID(state, ai.Model))
	if !ok {
		return
	}
	if requestedTier <= prev.tier {
		return // same or cheaper -- not an escalation
	}

	state.recordOutcome(lessons.Outcome{
		TS:        nowRFC3339(),
		SessionID: in.SessionID,
		TaskShape: prev.taskShape,
		Model:     prev.model,
		Effort:    prev.effort,
		Kind:      "escalation",
		Weight:    lessons.WeightEscalation,
	})
	// The prior decision has now been graded -- clear it so a SECOND
	// consecutive explicit-model call doesn't record a second escalation
	// against the same one.
	state.clearLastRouting(in.SessionID)
	state.log(logstore.Record{
		TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/Agent",
		Action: "escalation-detected", Reason: fmt.Sprintf("shape=%s recommended=%s(tier %d) requested-family=%s(tier %d)", prev.taskShape, prev.model, prev.tier, ai.Model, requestedTier),
	})
}

// familyToAnyModelID resolves a family alias (e.g. "opus") to any one
// matching catalog model id, so TierFor can look up its tier -- the
// Agent tool only ever supplies the family, never a full model id.
func familyToAnyModelID(state *daemonState, family string) string {
	for _, m := range state.cat.Models {
		if m.Family == family {
			return m.ID
		}
	}
	return ""
}

// runLessons backs `deadeye lessons [reset]`: the raw view (and the only
// reset) for the outcomes store that biases routing thresholds. One bad
// early escalation otherwise biases a task shape for up to the 30-day
// recency window with no way to inspect or clear it.
func runLessons(args []string) {
	if len(args) > 0 && args[0] == "reset" {
		outcomes, _ := lessons.Scan(meta.OutcomesPath())
		if err := os.Remove(meta.OutcomesPath()); err != nil && !os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "deadeye lessons reset:", err)
			os.Exit(1)
		}
		fmt.Printf("cleared %d recorded outcomes.\n", len(outcomes))
		fmt.Println(cDim("A running daemon keeps its in-memory copy until it restarts;"))
		fmt.Println(cDim("it exits after 30 idle minutes, or stop it with: deadeye uninstall (any hook respawns it)."))
		return
	}

	outcomes, err := lessons.Scan(meta.OutcomesPath())
	if err != nil || len(outcomes) == 0 {
		fmt.Println("No recorded outcomes. Routing thresholds are unbiased.")
		return
	}
	fmt.Println(cHead("Recorded outcomes") + cDim("  (each biases the downshift threshold for its task shape; 30-day recency window)"))
	for _, o := range outcomes {
		fmt.Printf("  %s  %-12s %-40s %s/%s  weight %.1f\n", cDim(o.TS), o.Kind, o.TaskShape, o.Model, o.Effort, o.Weight)
	}
	fmt.Println()
	fmt.Println(cDim("Clear everything: ") + cValue("deadeye lessons reset"))
}
