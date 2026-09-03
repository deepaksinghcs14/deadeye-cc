// Package kernel is PLAN.md §3.2's policy kernel: pure functions, no I/O.
// Decide turns evidence into a Decision. See docs/verified.md for what's
// actually enforceable with it (the Agent tool has no effort parameter,
// so Decision.Effort is frontmatter/advisory-only outside the model axis).
package kernel

import (
	"math"

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
	// Unsure is true when this decision is the thin-evidence default (empty,
	// NaN, or below-confidence-threshold evidence) rather than a confident
	// read -- i.e. the case the cheap signals couldn't actually place. The
	// optional AI judge (mode.routing_judge) only re-classifies these.
	Unsure bool
}

// veryHighComplexity triggers an xhigh upshift independent of the
// downshift path below -- upshifting is always free (INV-1), no
// confidence threshold required.
const veryHighComplexity = 0.9

// Two ceilings, because "we don't know" and "we know it's hard" are
// different situations that shouldn't cost the same:
//
//   - unsureCeilingTier (sonnet) is the default when evidence is thin,
//     untrusted, or just below the confidence bar. This fires for MOST
//     subagent spawns (a description-only prompt against a clean tree
//     leaves three of five signals with nothing to assess), so paying opus
//     for it -- the old behavior -- was where the money leaked. Sonnet is a
//     capable middle: conservative enough for an unknown task, far cheaper
//     than opus. INV-1 ("when unsure, go bigger than the evidence alone
//     justifies") still holds -- it just goes to a sane middle, not the
//     priciest model on the shelf.
//   - highCeilingTier (opus) is reserved for evidence that genuinely reads
//     HARD: a very-high-complexity signal (>= veryHighComplexity, upshifted
//     to xhigh) or a confident reading in the 0.75-0.9 band. Capped at tier
//     2 so it never routes to whatever happens to be the catalog's priciest
//     tier (verified live: thin evidence once routed to fable-5 at 2x opus
//     simply for being most expensive).
const unsureCeilingTier = 1
const highCeilingTier = 2

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
	ceiling := ceilingDecision(cat, "default: no evidence, or evidence insufficient to downshift -- a capable middle (sonnet-tier), not the priciest model", 0, "high", unsureCeilingTier)
	ceiling.Unsure = true

	if len(evidence) == 0 {
		return ceiling
	}

	maxComplexity, minConfidence := 0.0, 1.0
	for _, e := range evidence {
		// A NaN Complexity or Confidence must not silently read as "best
		// case" evidence: `>`/`<` against NaN are always false, so without
		// this guard a NaN Complexity would never raise maxComplexity above
		// its 0.0 seed (reads as "definitely simple"), and a NaN Confidence
		// would never lower minConfidence below its 1.0 seed (reads as
		// "fully trustworthy") -- both are the UNSAFE direction for a
		// kernel whose whole design (INV-1) is "when it doesn't know, it
		// goes big". Untrustworthy evidence forces the ceiling outright.
		if math.IsNaN(e.Complexity) || math.IsNaN(e.Confidence) {
			d := ceilingDecision(cat, "NaN evidence value, cannot be trusted -- default (sonnet-tier)", 0, "high", unsureCeilingTier)
			d.Unsure = true
			return d
		}
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
		return ceilingDecision(cat, "evidence indicates very high complexity -- upshifted, no confidence threshold required", minConfidence, "xhigh", highCeilingTier)
	}
	// A NaN downshiftThreshold must block downshift, not disable the gate:
	// `minConfidence < NaN` is always false, so without this guard a NaN
	// threshold would let ANY confidence value (however low) sail through.
	if math.IsNaN(downshiftThreshold) || minConfidence < downshiftThreshold {
		return ceiling
	}

	for _, b := range bands {
		if maxComplexity < b.maxComplexity {
			model, ok := modelAtTier(cat, b.tier)
			if !ok {
				// A catalog override missing the tier this band expects
				// (catalog.Load accepts one with no validation against
				// bands' tiers) hits this path with real, evaluated
				// evidence -- same misreport this function's final
				// fallthrough below was fixed to stop making: report the
				// real minConfidence and an accurate reason, not the
				// generic zero-evidence `ceiling`.
				return ceilingDecision(cat, "no model at the tier this band requires -- catalog gap", minConfidence, "high", highCeilingTier)
			}
			return Decision{
				Model:      model,
				Effort:     b.effort,
				Reason:     "all evidence supports downshift: low complexity, confidence >= threshold",
				Confidence: minConfidence,
			}
		}
	}
	// maxComplexity falls in the gap above the last band's threshold but
	// below veryHighComplexity -- too high to downshift, not high enough to
	// upshift. Report the REAL evidence-derived confidence and an accurate
	// reason via ceilingDecision, rather than returning the generic
	// zero-evidence `ceiling` built above: reusing it here misreported
	// Confidence:0 and "no evidence" even when real, sufficiently-confident
	// evidence existed and was evaluated (caught live: /deadeye-route
	// prints both fields straight from this Decision).
	// Confident, but in the 0.75-0.9 band: genuinely complex, so it earns the
	// high ceiling (opus), distinct from the thin-evidence sonnet default above.
	return ceilingDecision(cat, "evidence complexity too high to downshift -- genuinely complex", minConfidence, "high", highCeilingTier)
}

func ceilingDecision(cat catalog.Catalog, reason string, confidence float64, effort string, maxTier int) Decision {
	model := ""
	best := -1
	for _, m := range cat.Models {
		if m.Tier <= maxTier && m.Tier > best {
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
