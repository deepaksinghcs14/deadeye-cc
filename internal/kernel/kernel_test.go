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

// TestEmptyEvidenceIsUnsureDefault: missing evidence yields the UNSURE
// default -- a capable middle (tier 1 / sonnet), not the cheapest and not
// the priciest. INV-1 still holds (unsure goes bigger than the low-
// complexity evidence alone would justify), it just no longer defaults to
// opus. Confident, low-complexity evidence downshifts BELOW this default;
// genuinely-hard evidence rises ABOVE it (tested separately).
func TestEmptyEvidenceIsUnsureDefault(t *testing.T) {
	cat := testCatalog()
	ceiling := Decide(nil, cat, 0.8)
	if ceiling.Model != "mid" || ceiling.Effort != "high" {
		t.Fatalf("empty evidence = %+v, want mid tier (sonnet) / high effort", ceiling)
	}

	// Confident, low-complexity evidence downshifts to at or below the unsure
	// default -- never more expensive than it.
	cases := [][]signals.Evidence{
		{{Complexity: 0.1, Confidence: 0.95}},
		{{Complexity: 0.4, Confidence: 0.9}},
		{{Complexity: 0.6, Confidence: 0.85}},
	}
	for _, ev := range cases {
		d := Decide(ev, cat, 0.8)
		if decisionRank(d) > decisionRank(ceiling) {
			t.Errorf("low-complexity evidence %+v produced %+v, ranked above the unsure default %+v", ev, d, ceiling)
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
// must prevent downshift (max-complexity aggregation). Complexity 0.8 is a
// confident reading in the 0.75-0.9 band -- genuinely complex -- so it rises
// to the HIGH ceiling (opus/tier 2), distinct from the thin-evidence unsure
// default (sonnet/tier 1). The point is it did NOT downshift to the cheap
// tier.
func TestOneDisagreeingSignalBlocksDownshift(t *testing.T) {
	cat := testCatalog()
	d := Decide([]signals.Evidence{
		{Complexity: 0.05, Confidence: 0.95},
		{Complexity: 0.8, Confidence: 0.95},
	}, cat, 0.8)
	if d.Model != "top" || d.Effort != "high" {
		t.Errorf("disagreeing evidence set = %+v, want the high ceiling (top tier / high effort), not a downshift", d)
	}
}

// TestVagueTaskBlocksDownshiftEvenOnAQuietRepo is the regression test for a
// real live bug found while testing routing directly: "fix it" and a fully
// file-and-line-anchored bug report, spawned against the identical quiet,
// low-churn, tested file, routed IDENTICALLY to the cheap tier -- nothing
// was checking whether the delegated task itself was well-scoped, only
// whether the repo around it looked safe. This is the evidence set a real
// vague delegation against a favorable repo produces: filescope/gitchurn/
// testpresence all agree it's safe (mirroring TestAllAgreeingLowComplexity-
// Downshifts below), but taskspecificity reads the prompt as vague. One
// low-confidence signal must still block downshift -- the whole point of
// adding a signal that measures the prompt itself, not just repo state.
func TestVagueTaskBlocksDownshiftEvenOnAQuietRepo(t *testing.T) {
	cat := testCatalog()
	quietRepoAgrees := []signals.Evidence{
		{Complexity: 0.15, Confidence: 0.85}, // filescope: one file
		{Complexity: 0.3, Confidence: 0.82},  // gitchurn: low churn
		{Complexity: 0.1, Confidence: 0.8},   // testpresence: adjacent test exists
	}
	vague := append(append([]signals.Evidence{}, quietRepoAgrees...),
		signals.Evidence{Complexity: 0, Confidence: 0.2}, // taskspecificity: "fix it"
	)
	d := Decide(vague, cat, 0.8)
	ceiling := Decide(nil, cat, 0.8)
	if d != ceiling {
		t.Errorf("vague task against a quiet repo downshifted: got %+v, want ceiling %+v -- a favorable repo must not launder a vague delegation into a trusted downshift", d, ceiling)
	}

	// Same repo evidence, but taskspecificity now reads the prompt as
	// anchored (a real file:line reference) -- this MUST downshift, proving
	// the block above is genuinely about the vague signal, not some other
	// change.
	anchored := append(append([]signals.Evidence{}, quietRepoAgrees...),
		signals.Evidence{Complexity: 0, Confidence: 0.85}, // taskspecificity: anchored
	)
	d2 := Decide(anchored, cat, 0.8)
	if d2 == ceiling {
		t.Errorf("anchored task against the same quiet repo did not downshift: got %+v", d2)
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

// TestCeilingsCapBelowTheMostExpensiveTier: neither ceiling ever routes to
// the catalog's priciest tier. The unsure default lands at tier 1 (sonnet);
// the high-complexity ceiling lands at tier 2 (opus) -- both below tier 3
// (fable), the regression this guards (thin evidence once routed to fable-5
// at 2x opus in a real session, just for being most expensive).
func TestCeilingsCapBelowTheMostExpensiveTier(t *testing.T) {
	cat := catalog.Catalog{Models: []catalog.Model{
		{ID: "haiku-like", Tier: 0},
		{ID: "sonnet-like", Tier: 1},
		{ID: "opus-like", Tier: 2},
		{ID: "priciest", Tier: 3},
	}}
	if d := Decide(nil, cat, 0.8); d.Model != "sonnet-like" {
		t.Errorf("unsure default = %q, want %q (tier 1)", d.Model, "sonnet-like")
	}
	if d := Decide([]signals.Evidence{{Complexity: 0.95, Confidence: 0.9}}, cat, 0.8); d.Model != "opus-like" {
		t.Errorf("high-complexity ceiling = %q, want %q (tier 2, below the priciest tier 3)", d.Model, "opus-like")
	}
}

// TestExplicitRoleReachesThePriciestTierOnPurpose is the deliberate
// opposite of TestCeilingsCapBelowTheMostExpensiveTier, proving the two
// don't conflict: an UN-roled catalog still never accidentally reaches the
// priciest tier (that test, unmodified, still passes), but a catalog that
// explicitly tags its priciest tier "high_ceiling" now CAN reach it -- a
// human said so in the data, the kernel didn't default there on its own.
func TestExplicitRoleReachesThePriciestTierOnPurpose(t *testing.T) {
	cat := catalog.Catalog{Models: []catalog.Model{
		{ID: "haiku-like", Tier: 0},
		{ID: "sonnet-like", Tier: 1, Role: catalog.RoleUnsureCeiling},
		{ID: "opus-like", Tier: 2},
		{ID: "priciest", Tier: 3, Role: catalog.RoleHighCeiling},
	}}
	if d := Decide([]signals.Evidence{{Complexity: 0.95, Confidence: 0.9}}, cat, 0.8); d.Model != "priciest" {
		t.Errorf("high-complexity ceiling with an explicit role = %q, want %q (the role-tagged tier 3)", d.Model, "priciest")
	}
	// The unsure default is untouched by the high_ceiling tag -- still the
	// role-tagged tier 1, not the priciest.
	if d := Decide(nil, cat, 0.8); d.Model != "sonnet-like" {
		t.Errorf("unsure default = %q, want %q (the role-tagged tier 1)", d.Model, "sonnet-like")
	}
}

// TestRoleWorksAtUnusualTierNumbers directly answers "can this go beyond
// tier 2": a sparse catalog with roles on non-adjacent, unusual tier
// numbers (3 and 7, with gaps and no tier 0/1/2 at all) still resolves
// correctly through the role lookup -- proving the ceiling no longer
// depends on any particular tier NUMBER, only on which model is tagged.
func TestRoleWorksAtUnusualTierNumbers(t *testing.T) {
	cat := catalog.Catalog{Models: []catalog.Model{
		{ID: "mid-ish", Tier: 3, Role: catalog.RoleUnsureCeiling},
		{ID: "top-ish", Tier: 7, Role: catalog.RoleHighCeiling},
	}}
	if d := Decide(nil, cat, 0.8); d.Model != "mid-ish" {
		t.Errorf("unsure default = %q, want %q (role-tagged, tier 3)", d.Model, "mid-ish")
	}
	if d := Decide([]signals.Evidence{{Complexity: 0.95, Confidence: 0.9}}, cat, 0.8); d.Model != "top-ish" {
		t.Errorf("high-complexity ceiling = %q, want %q (role-tagged, tier 7)", d.Model, "top-ish")
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

// TestUnsureFlag: Decision.Unsure marks exactly the thin-evidence defaults
// (what the optional AI judge re-classifies), not confident reads.
func TestUnsureFlag(t *testing.T) {
	cat := testCatalog()
	if !Decide(nil, cat, 0.8).Unsure {
		t.Error("empty evidence should be Unsure")
	}
	if !Decide([]signals.Evidence{{Complexity: 0.1, Confidence: 0.3}}, cat, 0.8).Unsure {
		t.Error("low-confidence evidence should be Unsure")
	}
	if Decide([]signals.Evidence{{Complexity: 0.1, Confidence: 0.95}}, cat, 0.8).Unsure {
		t.Error("a confident downshift should NOT be Unsure")
	}
	if Decide([]signals.Evidence{{Complexity: 0.95, Confidence: 0.9}}, cat, 0.8).Unsure {
		t.Error("a very-high-complexity read should NOT be Unsure")
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
