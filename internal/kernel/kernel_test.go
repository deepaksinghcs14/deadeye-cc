package kernel

import (
	"math"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/signals"
)

func testCatalog() catalog.Catalog {
	return catalog.Catalog{Models: []catalog.Model{
		{ID: "cheap", Tier: 0},
		{ID: "mid", Tier: 1},
		{ID: "top", Tier: 2},
	}}
}

var effortOrder = []string{"low", "medium", "high", "xhigh"}

func effortIndex(e string) int {
	for i, v := range effortOrder {
		if v == e {
			return i
		}
	}
	return len(effortOrder) - 1
}

func decisionRank(d Decision) int {
	// A single ordinal for comparing decisions cheapest-to-most-expensive:
	// tier dominates, effort breaks ties within a tier.
	tier := 0
	for _, m := range testCatalog().Models {
		if m.ID == d.Model {
			tier = m.Tier
		}
	}
	return tier*len(effortOrder) + effortIndex(d.Effort)
}

// TestEmptyEvidenceIsCeiling is INV-1's headline property: missing
// evidence must never produce a cheaper model or lower effort than any
// possible evidence-driven decision.
func TestEmptyEvidenceIsCeiling(t *testing.T) {
	cat := testCatalog()
	ceiling := Decide(nil, cat, 0.8)
	if ceiling.Model != "top" || ceiling.Effort != "high" {
		t.Fatalf("empty evidence = %+v, want top tier / high effort", ceiling)
	}

	// No evidence-driven decision should ever outrank (be MORE expensive
	// than) the ceiling -- confident, bounded-complexity evidence is
	// supposed to downshift BELOW it; that's the whole point. (Very-high-
	// complexity evidence is deliberately excluded from this list: it
	// legitimately upshifts PAST the ceiling's effort, to xhigh -- see
	// TestVeryHighComplexityUpshiftsRegardlessOfConfidence -- so "the
	// ceiling" is an upper bound for these bounded-complexity cases only,
	// not an absolute cap across every possible decision.)
	cases := [][]signals.Evidence{
		{{Complexity: 0.1, Confidence: 0.95}},
		{{Complexity: 0.4, Confidence: 0.9}},
		{{Complexity: 0.6, Confidence: 0.85}},
	}
	for _, ev := range cases {
		d := Decide(ev, cat, 0.8)
		if decisionRank(d) > decisionRank(ceiling) {
			t.Errorf("evidence %+v produced %+v, ranked above the ceiling %+v", ev, d, ceiling)
		}
	}
}

// TestLowConfidenceBlocksDownshift: a single unreliable signal must
// prevent downshifting even when its complexity estimate is low.
func TestLowConfidenceBlocksDownshift(t *testing.T) {
	cat := testCatalog()
	d := Decide([]signals.Evidence{{Complexity: 0.1, Confidence: 0.3}}, cat, 0.8)
	ceiling := Decide(nil, cat, 0.8)
	if d != ceiling {
		t.Errorf("low-confidence evidence downshifted: got %+v, want ceiling %+v", d, ceiling)
	}
}

// TestOneDisagreeingSignalBlocksDownshift: even with other confident,
// low-complexity evidence present, one signal reporting high complexity
// must prevent downshift (max-complexity aggregation). Checks tier/effort
// only, not full struct equality with the zero-evidence ceiling: complexity
// 0.8 falls in the gap above the last band and below veryHighComplexity, so
// the real, evaluated confidence (0.95) is what should be reported here --
// see TestComplexityGapReportsRealConfidence for that behavior directly.
func TestOneDisagreeingSignalBlocksDownshift(t *testing.T) {
	cat := testCatalog()
	d := Decide([]signals.Evidence{
		{Complexity: 0.05, Confidence: 0.95},
		{Complexity: 0.8, Confidence: 0.95},
	}, cat, 0.8)
	ceiling := Decide(nil, cat, 0.8)
	if d.Model != ceiling.Model || d.Effort != ceiling.Effort {
		t.Errorf("disagreeing evidence set downshifted: got %+v, want tier/effort of ceiling %+v", d, ceiling)
	}
}

// TestComplexityGapReportsRealConfidence is the regression test for a bug
// where complexity in the gap above the last band's threshold (0.75) and
// below veryHighComplexity (0.9) fell through to the generic zero-evidence
// ceiling, misreporting Confidence:0 and "no evidence" even though real,
// sufficiently-confident evidence was supplied and evaluated -- user-visible
// via /deadeye-route, which prints both fields straight from the Decision.
func TestComplexityGapReportsRealConfidence(t *testing.T) {
	cat := testCatalog()
	d := Decide([]signals.Evidence{{Complexity: 0.8, Confidence: 0.95}}, cat, 0.5)
	if d.Model != "top" || d.Effort != "high" {
		t.Errorf("complexity-gap evidence = %+v, want top tier / high effort", d)
	}
	if d.Confidence != 0.95 {
		t.Errorf("Confidence = %v, want the real evaluated confidence 0.95, not the generic ceiling's 0", d.Confidence)
	}
}

// TestNaNEvidenceForcesCeiling is the regression test for a bug where NaN
// Complexity or Confidence silently read as best-case evidence: `>`/`<`
// comparisons against NaN are always false, so a NaN Complexity never rose
// above the 0.0 seed (read as "definitely simple") and a NaN Confidence
// never lowered the 1.0 seed (read as "fully trustworthy") -- both are the
// unsafe direction for INV-1 ("when it doesn't know, it goes big").
func TestNaNEvidenceForcesCeiling(t *testing.T) {
	cat := testCatalog()
	ceiling := Decide(nil, cat, 0.8)
	cases := []signals.Evidence{
		{Complexity: math.NaN(), Confidence: 0.99},
		{Complexity: 0.1, Confidence: math.NaN()},
	}
	for _, ev := range cases {
		d := Decide([]signals.Evidence{ev}, cat, 0.8)
		if d.Model != ceiling.Model || d.Effort != ceiling.Effort {
			t.Errorf("NaN evidence %+v downshifted: got %+v, want ceiling tier/effort %+v", ev, d, ceiling)
		}
	}
}

// TestNaNDownshiftThresholdBlocksDownshift is the regression test for a bug
// where a NaN downshiftThreshold disabled the confidence gate entirely:
// `minConfidence < NaN` is always false, so evidence with arbitrarily low
// confidence sailed through undetected instead of being blocked.
func TestNaNDownshiftThresholdBlocksDownshift(t *testing.T) {
	cat := testCatalog()
	ceiling := Decide(nil, cat, 0.8)
	d := Decide([]signals.Evidence{{Complexity: 0.1, Confidence: 0.01}}, cat, math.NaN())
	if d.Model != ceiling.Model || d.Effort != ceiling.Effort {
		t.Errorf("NaN downshiftThreshold let low-confidence evidence through: got %+v, want ceiling tier/effort %+v", d, ceiling)
	}
}

// TestVeryHighComplexityUpshiftsRegardlessOfConfidence: upshifting is
// always free (INV-1) -- no confidence threshold gates it. Effort goes all
// the way to xhigh here specifically -- distinct from the plain "we don't
// know" ceiling, which stays at "high" (see TestEmptyEvidenceIsCeiling).
func TestVeryHighComplexityUpshiftsRegardlessOfConfidence(t *testing.T) {
	cat := testCatalog()
	d := Decide([]signals.Evidence{{Complexity: 0.95, Confidence: 0.01}}, cat, 0.8)
	if d.Model != "top" || d.Effort != "xhigh" {
		t.Errorf("very-high-complexity evidence = %+v, want top tier / xhigh effort", d)
	}
}

// TestCeilingCapsBelowTheMostExpensiveTier is the regression test for a
// real bug caught live: with the real builtin catalog (tier 3 =
// claude-fable-5, 2x opus's price), the ceiling picked the highest TIER,
// which happens to be the most EXPENSIVE model -- three read-only
// search-agent calls with thin evidence were all advised fable-5 in one
// real session. ceilingTier caps this at tier 2 (opus-equivalent).
func TestCeilingCapsBelowTheMostExpensiveTier(t *testing.T) {
	cat := catalog.Catalog{Models: []catalog.Model{
		{ID: "haiku-like", Tier: 0},
		{ID: "sonnet-like", Tier: 1},
		{ID: "opus-like", Tier: 2},
		{ID: "priciest", Tier: 3},
	}}
	d := Decide(nil, cat, 0.8)
	if d.Model != "opus-like" {
		t.Errorf("ceiling model = %q, want %q -- ceilingTier=2 should exclude the catalog's most expensive tier", d.Model, "opus-like")
	}
}

// TestCeilingFallsBackWhenNoModelAtOrUnderCeilingTier covers a catalog
// override with nothing at or below ceilingTier -- must not return an
// empty model id.
func TestCeilingFallsBackWhenNoModelAtOrUnderCeilingTier(t *testing.T) {
	cat := catalog.Catalog{Models: []catalog.Model{{ID: "only-option", Tier: 5}}}
	d := Decide(nil, cat, 0.8)
	if d.Model != "only-option" {
		t.Errorf("ceiling model = %q, want %q (fallback to the catalog's own highest tier)", d.Model, "only-option")
	}
}

// TestBandModelGapReportsRealConfidence is the regression test for a bug
// where a catalog missing the model tier a downshift band requires (a
// plausible ~/.deadeye/catalog.json override, which catalog.Load accepts
// with no validation against the bands table) fell through to the generic
// zero-evidence `ceiling`, misreporting Confidence:0 and "no evidence" even
// though real, sufficiently-confident evidence was supplied and evaluated
// -- the same bug TestComplexityGapReportsRealConfidence covers for the
// loop's fallthrough case, one branch below this one.
func TestBandModelGapReportsRealConfidence(t *testing.T) {
	cat := catalog.Catalog{Models: []catalog.Model{{ID: "top", Tier: 2}}} // no tier 0 or 1
	d := Decide([]signals.Evidence{{Complexity: 0.1, Confidence: 0.95}}, cat, 0.5)
	if d.Model != "top" {
		t.Errorf("model = %q, want fallback to the catalog's own available model %q", d.Model, "top")
	}
	if d.Confidence != 0.95 {
		t.Errorf("Confidence = %v, want the real evaluated confidence 0.95, not the generic ceiling's 0", d.Confidence)
	}
}

func TestAllAgreeingLowComplexityDownshifts(t *testing.T) {
	cat := testCatalog()
	d := Decide([]signals.Evidence{
		{Complexity: 0.1, Confidence: 0.9},
		{Complexity: 0.15, Confidence: 0.85},
	}, cat, 0.8)
	if d.Model != "cheap" || d.Effort != "low" {
		t.Errorf("agreeing low-complexity evidence = %+v, want cheap tier / low effort", d)
	}
}

// TestRemovingASupportingSignalNeverGetsCheaper documents which direction
// of "removing evidence" is actually provable, and why the plan's literal
// phrasing ("removing evidence must never lower the decision", read as
// "for any single element removed from any set") is not achievable by any
// evidence-sensitive kernel:
//
// Counter-example: evidence set S = {A: complexity 0.05 confidence 0.95,
// B: complexity 0.9 confidence 0.95}. B's high complexity forces the
// ceiling (see TestOneDisagreeingSignalBlocksDownshift). Removing B gives
// S' = {A}, which alone satisfies the downshift bands and DOES downshift
// (see TestAllAgreeingLowComplexityDownshifts). So decide(S') is cheaper
// than decide(S) -- removing evidence B lowered the decision. This isn't
// a bug: B is the exact kind of evidence INV-1 wants to be able to block a
// downshift, and blocking things is only meaningful if removing the
// blocker un-blocks them.
//
// What IS true, and what this test checks: removing a downshift-*supporting*
// item (one that was helping push toward a cheaper decision, not blocking
// it) from a set that already agrees on downshifting never makes the
// result cheaper -- it can only make the aggregate complexity estimate
// higher (fewer low readings to pull the max down) or leave it unchanged,
// both of which move toward the ceiling or stay put.
func TestRemovingASupportingSignalNeverGetsCheaper(t *testing.T) {
	cat := testCatalog()
	full := []signals.Evidence{
		{Complexity: 0.05, Confidence: 0.95},
		{Complexity: 0.1, Confidence: 0.9},
	}
	before := Decide(full, cat, 0.8)

	withoutOne := full[:1]
	after := Decide(withoutOne, cat, 0.8)

	if decisionRank(after) < decisionRank(before) {
		t.Errorf("removing a supporting signal got cheaper: before=%+v after=%+v", before, after)
	}
}
