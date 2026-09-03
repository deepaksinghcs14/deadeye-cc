package catalog

import "testing"

// TestBuiltinRankedByBlendedPrice guards the invariant the generator
// enforces: tiers ascend with input+4*output price, so "cheapest tier that
// fits" (PLAN.md §3.2) is always tier 0.
func TestBuiltinRankedByBlendedPrice(t *testing.T) {
	blended := func(m Model) float64 { return m.InputPrice + 4*m.OutputPrice }
	for i := 1; i < len(builtin.Models); i++ {
		prev, cur := builtin.Models[i-1], builtin.Models[i]
		if cur.Tier != prev.Tier+1 {
			t.Errorf("model %d (%s) tier %d is not prev tier+1 (%d)", i, cur.ID, cur.Tier, prev.Tier+1)
		}
		if blended(cur) < blended(prev) {
			t.Errorf("%s (blended %.1f, tier %d) is cheaper than %s (blended %.1f, tier %d) but ranks higher",
				cur.ID, blended(cur), cur.Tier, prev.ID, blended(prev), prev.Tier)
		}
	}
}

func TestCheapestIsTierZero(t *testing.T) {
	m, ok := builtin.Cheapest()
	if !ok {
		t.Fatal("Cheapest() found nothing in builtin catalog")
	}
	if m.Tier != 0 {
		t.Errorf("Cheapest() returned tier %d, want 0", m.Tier)
	}
}

// TestBuiltinHasExactlyOneOfEachCeilingRole guards the compiled-in table
// against a future seed-table edit silently dropping or duplicating a
// role -- UnsureCeiling/HighCeiling only look at the FIRST match, so a
// second model quietly carrying the same role would become unreachable
// dead weight with no error, the same footgun a duplicate tier number is.
func TestBuiltinHasExactlyOneOfEachCeilingRole(t *testing.T) {
	for _, role := range []string{RoleUnsureCeiling, RoleHighCeiling} {
		n := 0
		for _, m := range builtin.Models {
			if m.Role == role {
				n++
			}
		}
		if n != 1 {
			t.Errorf("builtin has %d models with role %q, want exactly 1", n, role)
		}
	}
}

func TestUnsureCeilingPrefersRoleOverFallbackTier(t *testing.T) {
	cat := Catalog{Models: []Model{
		{ID: "cheap", Tier: 0},
		{ID: "fallback-mid", Tier: 1},
		{ID: "actual-mid", Tier: 4, Role: RoleUnsureCeiling},
	}}
	m, ok := cat.UnsureCeiling()
	if !ok || m.ID != "actual-mid" {
		t.Errorf("UnsureCeiling() = (%+v, %v), want the role-tagged model at tier 4, not the tier-1 fallback", m, ok)
	}
}

func TestHighCeilingPrefersRoleOverFallbackTier(t *testing.T) {
	cat := Catalog{Models: []Model{
		{ID: "cheap", Tier: 0},
		{ID: "fallback-top", Tier: 2},
		{ID: "actual-top", Tier: 9, Role: RoleHighCeiling},
	}}
	m, ok := cat.HighCeiling()
	if !ok || m.ID != "actual-top" {
		t.Errorf("HighCeiling() = (%+v, %v), want the role-tagged model at tier 9, not the tier-2 fallback", m, ok)
	}
}

// TestCeilingsFallBackWhenNoRolePresent is the compatibility guarantee:
// a catalog with no Role field set anywhere (any ~/.deadeye/catalog.json
// override written before Role existed) resolves ceilings exactly as
// before -- tier 1 for unsure, tier 2 for high.
func TestCeilingsFallBackWhenNoRolePresent(t *testing.T) {
	cat := Catalog{Models: []Model{
		{ID: "cheap", Tier: 0}, {ID: "mid", Tier: 1}, {ID: "top", Tier: 2}, {ID: "priciest", Tier: 3},
	}}
	if m, ok := cat.UnsureCeiling(); !ok || m.ID != "mid" {
		t.Errorf("UnsureCeiling() = (%+v, %v), want the tier-1 fallback %q", m, ok, "mid")
	}
	if m, ok := cat.HighCeiling(); !ok || m.ID != "top" {
		t.Errorf("HighCeiling() = (%+v, %v), want the tier-2 fallback %q", m, ok, "top")
	}
}

func TestFamilyForMatchesEveryModel(t *testing.T) {
	for _, m := range builtin.Models {
		got, ok := builtin.FamilyFor(m.ID)
		if !ok || got != m.Family {
			t.Errorf("FamilyFor(%q) = (%q, %v), want (%q, true)", m.ID, got, ok, m.Family)
		}
	}
	if _, ok := builtin.FamilyFor("no-such-model"); ok {
		t.Error("FamilyFor matched an unknown model id")
	}
}

// TestTierForMatchesEveryModel guards TierFor -- lessons.go's escalation
// detection and decide.go's routing baseline both key off it, so a broken
// lookup here would silently corrupt the learning loop with no test to
// catch it.
func TestTierForMatchesEveryModel(t *testing.T) {
	for _, m := range builtin.Models {
		got, ok := builtin.TierFor(m.ID)
		if !ok || got != m.Tier {
			t.Errorf("TierFor(%q) = (%d, %v), want (%d, true)", m.ID, got, ok, m.Tier)
		}
	}
	if _, ok := builtin.TierFor("no-such-model"); ok {
		t.Error("TierFor matched an unknown model id")
	}
}

// TestModelAtTierRoundTripsTierFor guards ModelAtTier -- the AI judge's
// tier classification (decide.go, route.go) is translated back into a
// model id through it, so a broken lookup would substitute the wrong model
// into a live routing decision with no test to catch it.
func TestModelAtTierRoundTripsTierFor(t *testing.T) {
	for _, m := range builtin.Models {
		id, ok := builtin.ModelAtTier(m.Tier)
		if !ok {
			t.Errorf("ModelAtTier(%d) found nothing, want a model", m.Tier)
			continue
		}
		if gotTier, _ := builtin.TierFor(id); gotTier != m.Tier {
			t.Errorf("ModelAtTier(%d) = %q, which TierFor reports as tier %d", m.Tier, id, gotTier)
		}
	}
	if _, ok := builtin.ModelAtTier(-1); ok {
		t.Error("ModelAtTier matched a tier that doesn't exist")
	}
}

func TestLoadFallsBackToBuiltinWhenNoOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // meta.StateDir() resolves under $HOME
	c := Load()
	if c.Source != "builtin" {
		t.Errorf("Source = %q, want builtin", c.Source)
	}
	if len(c.Models) != len(builtin.Models) {
		t.Errorf("got %d models, want %d", len(c.Models), len(builtin.Models))
	}
}
