package coder

import "testing"

// TestInjectionSizeBudget: the ruleset is injected every session start --
// growth must be deliberate, not drift. 6.5KB ceiling = the size after the
// comments-and-docs block landed (v0.8.0), with modest headroom.
func TestInjectionSizeBudget(t *testing.T) {
	for _, l := range []string{LevelSpotter, LevelMarksman, LevelSniper} {
		if n := len(Instructions(l)); n > 6500 {
			t.Errorf("%s injection is %d bytes -- trim before adding more", l, n)
		} else {
			t.Logf("%s: %d bytes", l, n)
		}
	}
}
