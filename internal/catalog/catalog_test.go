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
