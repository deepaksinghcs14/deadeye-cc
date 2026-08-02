// Package inject builds the plugin's one advisory injection: the model
// tier table, effort/workflow guidance, and anti-waste rules that
// PLAN.md §5.1 originally planned to deliver via SessionStart.
// docs/verified.md confirmed SessionStart cannot put anything in the
// model's context in Claude Code v2.1.220 -- this is delivered instead via
// the first UserPromptSubmit of a session (confirmed working), gated to
// fire exactly once per session so it stays byte-stable (INV-4).
package inject

import (
	"fmt"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
)

// EstimateTokens is a rough 4-chars-per-token heuristic for budget
// checking a short advisory string -- not a real tokenizer, good enough to
// flag "this injection is getting too big" in the decision log.
func EstimateTokens(s string) int { return (len(s) + 3) / 4 }

// Build composes the injection text. sessionMemory, if non-empty, is
// Phase 1.5's prior-session summary head, appended within the same budget.
func Build(cat catalog.Catalog, sessionMemory string) string {
	var b strings.Builder
	b.WriteString("deadeye guidance for this session:\n")
	b.WriteString("- Model tiers (cheapest first) -- when calling the Agent tool, set model to the cheapest tier that fits: mechanical work -> tier 0, standard implementation -> tier 1, deep reasoning -> top tier.\n")
	for _, m := range cat.Models {
		fmt.Fprintf(&b, "  tier %d: %s\n", m.Tier, m.ID)
	}
	b.WriteString("- Request lower effort for mechanical steps; reserve high/xhigh for judgment calls.\n")
	b.WriteString("- Workflow scripts: route indexing/classification phases to the cheapest tier, reserve the strongest model for final verification.\n")
	b.WriteString("- Before editing files for a multi-file or high-radius task, present a short plan and wait for a go-ahead.\n")
	b.WriteString("- Don't re-read a file already read this session; grep before a full-file read; cap verbose command output.\n")
	if sessionMemory != "" {
		b.WriteString("\nPicking up from the last session in this project:\n")
		b.WriteString(sessionMemory)
	}
	return b.String()
}
