// Package kernel is PLAN.md §3.2's policy kernel: pure functions, no I/O.
// Decide turns evidence into a Decision. See docs/verified.md for what's
// actually enforceable with it (the Agent tool has no effort parameter,
// so Decision.Effort is frontmatter/advisory-only outside the model axis).
package kernel

import (
	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/signals"
)

type Decision struct {
	Model      string
	Effort     string // low|medium|high|xhigh -- never max, session-only (non-goal §13)
	PlanGate   bool
	Workflow   string // "none" | "recommend" -- never "trigger" (INV-2)
	Reason     string
	Confidence float64
}

// veryHighComplexity triggers an xhigh upshift independent of the
// downshift path below -- upshifting is always free (INV-1), no
// confidence threshold required.
const veryHighComplexity = 0.9

// ceilingTier caps the "we don't know" decision at this tier or below.
// The highest tier in the catalog is the most EXPENSIVE model, not
// necessarily the best default -- verified live: with thin evidence (a
// clean working tree, a repo-wide search prompt with no keyword match),
// the ceiling routed to claude-fable-5 at $10/$50 per MTok, 2x opus,
// simply because it happened to be the priciest model in the catalog.
// Routing every "unknown" case to whatever is most expensive makes a
// cost-optimizing plugin cost-negative. Raise to a higher tier (or
// remove the cap) to restore "always the single most capable model".
const ceilingTier = 2

// Grid search, not sequential choice (PLAN.md §3.2): conceptually this
// enumerates (tier, effort) cells, estimates cost from catalog pricing,
// and picks the cheapest cell clearing the evidence-derived capability
// floor. It's implemented as picking (minTier, minEffort) directly rather
// than a literal nested loop, because cost is monotonic increasing in
// both tier and effort independently for this catalog -- any cell with a
// higher tier OR higher effort than the minimum-satisfying one can only
// cost more, never less, so the argmin is provably (minTier, minEffort)
// itself. A literal grid loop would compute the same answer with more
// code.
var bands = []struct {
	maxComplexity float64
	tier          int
	effort        string
}{
	{0.25, 0, "low"},
	{0.5, 0, "medium"},
	{0.75, 1, "medium"},
}

// Decide is deliberately conservative by construction, not by a bolt-on
// check (INV-1): the empty-evidence path and the "some evidence disagrees"
// path both return the same ceiling value, so there is no separate code
// path that could regress to something cheaper.
//
// Property tested in kernel_test.go: this design does NOT satisfy "removing
// ANY single element from ANY evidence set never lowers the decision" --
// that's mathematically impossible for a kernel where evidence can
// legitimately swing the decision either direction (proof in the test file's
// doc comment). What it does guarantee, and what's tested: empty evidence
// always yields the ceiling; any single low-confidence item blocks
// downshift entirely; any single very-high-complexity item forces the
// ceiling (upshifted), regardless of everything else; and removing a
// downshift-*supporting* item from an otherwise-agreeing set never makes
// the result cheaper.
func Decide(evidence []signals.Evidence, cat catalog.Catalog, downshiftThreshold float64) Decision {
	ceiling := ceilingDecision(cat, "default ceiling: no evidence, or evidence insufficient to downshift", 0, "high")

	if len(evidence) == 0 {
		return ceiling
	}

	maxComplexity, minConfidence := 0.0, 1.0
	for _, e := range evidence {
		if e.Complexity > maxComplexity {
			maxComplexity = e.Complexity
		}
		if e.Confidence < minConfidence {
			minConfidence = e.Confidence
		}
	}

	if maxComplexity >= veryHighComplexity {
		// A genuinely very-high-complexity reading upshifts past even the
		// default ceiling's effort, to xhigh -- distinct from "we don't
		// know" (which stays at "high"). Upshifting is always free
		// (INV-1): no confidence threshold gates this branch.
		return ceilingDecision(cat, "evidence indicates very high complexity -- upshifted, no confidence threshold required", minConfidence, "xhigh")
	}
	if minConfidence < downshiftThreshold {
		return ceiling
	}

	for _, b := range bands {
		if maxComplexity < b.maxComplexity {
			model, ok := modelAtTier(cat, b.tier)
			if !ok {
				return ceiling
			}
			return Decision{
				Model:      model,
				Effort:     b.effort,
				Reason:     "all evidence supports downshift: low complexity, confidence >= threshold",
				Confidence: minConfidence,
			}
		}
	}
	return ceiling
}

func ceilingDecision(cat catalog.Catalog, reason string, confidence float64, effort string) Decision {
	model := ""
	best := -1
	for _, m := range cat.Models {
		if m.Tier <= ceilingTier && m.Tier > best {
			best = m.Tier
			model = m.ID
		}
	}
	if best == -1 {
		// No model at or under ceilingTier -- a catalog override with only
		// higher tiers, say. Fall back to the catalog's own highest tier
		// rather than returning an empty model id.
		for _, m := range cat.Models {
			if m.Tier > best {
				best = m.Tier
				model = m.ID
			}
		}
	}
	return Decision{Model: model, Effort: effort, Reason: reason, Confidence: confidence}
}

func modelAtTier(cat catalog.Catalog, tier int) (string, bool) {
	for _, m := range cat.Models {
		if m.Tier == tier {
			return m.ID, true
		}
	}
	return "", false
}
