package inject

import (
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
)

// TestBuildStaysUnderBudget guards INV-4's 400-token injection ceiling
// using the builtin catalog (the common case). Session-memory text is
// caller-capped separately (Phase 1.5 truncates to a fixed line count)
// since it's the one open-ended input here.
func TestBuildStaysUnderBudget(t *testing.T) {
	text := Build(catalog.Load(), "", true)
	tokens := EstimateTokens(text)
	if tokens > 400 {
		t.Errorf("injection estimated at %d tokens, want <= 400:\n%s", tokens, text)
	}
}

func TestBuildIncludesEveryTier(t *testing.T) {
	cat := catalog.Load()
	text := Build(cat, "", true)
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
	withEffort := Build(catalog.Load(), "", true)
	if !strings.Contains(withEffort, "lower effort") {
		t.Error("effort guidance missing with includeEffort=true")
	}
	withoutEffort := Build(catalog.Load(), "", false)
	if strings.Contains(withoutEffort, "lower effort") {
		t.Error("effort guidance still present with includeEffort=false")
	}
}
