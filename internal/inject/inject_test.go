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
	text := Build(catalog.Load(), "")
	tokens := EstimateTokens(text)
	if tokens > 400 {
		t.Errorf("injection estimated at %d tokens, want <= 400:\n%s", tokens, text)
	}
}

func TestBuildIncludesEveryTier(t *testing.T) {
	cat := catalog.Load()
	text := Build(cat, "")
	for _, m := range cat.Models {
		if !strings.Contains(text, m.ID) {
			t.Errorf("injection missing tier line for %s", m.ID)
		}
	}
}
