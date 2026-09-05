package inject

import (
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
)

// TestBuildStaysUnderBudget guards INV-4's 400-token injection ceiling
// using the builtin catalog (the common case). Session-memory text is
// caller-capped separately (Phase 1.5 truncates to a fixed line count)
// since it's the one open-ended input here. judgeOn=true exercises the
// larger of the two variants -- the real ceiling to guard.
func TestBuildStaysUnderBudget(t *testing.T) {
	text := Build(catalog.Load(), "", true, true, "")
	tokens := EstimateTokens(text)
	if tokens > 400 {
		t.Errorf("injection estimated at %d tokens, want <= 400:\n%s", tokens, text)
	}
}

func TestBuildIncludesEveryTier(t *testing.T) {
	cat := catalog.Load()
	text := Build(cat, "", true, true, "")
	for _, m := range cat.Models {
		if !strings.Contains(text, m.ID) {
			t.Errorf("injection missing tier line for %s", m.ID)
		}
	}
}

// TestBuildOmitsEffortGuidanceWhenOff is the regression test for the
// mode.effort knob: it existed and was printed by /deadeye-status, but
// nothing ever read it -- setting it to "off" silently did nothing.
func TestBuildOmitsEffortGuidanceWhenOff(t *testing.T) {
	withEffort := Build(catalog.Load(), "", true, true, "")
	if !strings.Contains(withEffort, "lower effort") {
		t.Error("effort guidance missing with includeEffort=true")
	}
	withoutEffort := Build(catalog.Load(), "", false, true, "")
	if strings.Contains(withoutEffort, "lower effort") {
		t.Error("effort guidance still present with includeEffort=false")
	}
}

// TestBuildJudgeCarveOut is the regression test for the actual bug this
// change fixes: the old unconditional "ALWAYS set the Agent tool's model
// explicitly" left applyRoutingJudge (cmd/deadeye/decide.go, gated on
// `ai.Model == ""`) structurally unable to ever fire against a caller
// that follows deadeye's own coaching -- confirmed live against this
// machine's real decision log (177 real Agent-routing calls, zero judge
// firings). The carve-out must appear only when the judge is actually
// on; claiming "deadeye's judge classifies it" when routing_judge is off
// would be a false promise -- unset would just fall through to the Agent
// tool's own default, no informed recommendation at all.
func TestBuildJudgeCarveOut(t *testing.T) {
	const carveOut = "Leave model unset"
	withJudge := Build(catalog.Load(), "", true, true, "")
	if !strings.Contains(withJudge, carveOut) {
		t.Error("judge carve-out missing with judgeOn=true -- the judge can never fire against a caller following this guidance")
	}
	withoutJudge := Build(catalog.Load(), "", true, false, "")
	if strings.Contains(withoutJudge, carveOut) {
		t.Error("judge carve-out present with judgeOn=false -- false promise, no judge will actually classify an unset model")
	}
	if !strings.Contains(withoutJudge, "set the Agent tool's `model` explicitly") {
		t.Error("base tier guidance missing when judge is off")
	}
}
