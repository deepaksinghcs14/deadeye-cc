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
