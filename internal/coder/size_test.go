package coder

import "testing"

// TestInjectionSizeBudget: the ruleset is injected every session start --
// growth must be deliberate, not drift. 8KB ceiling = the size after the
// "Check your backstop" security section landed (deliberately raised from
// 6.5KB in the same commit as that section, not a silent bump), with
// modest headroom.
func TestInjectionSizeBudget(t *testing.T) {
	for _, l := range []string{LevelSpotter, LevelMarksman, LevelSniper} {
		if n := len(Instructions(l)); n > 8000 {
			t.Errorf("%s injection is %d bytes -- trim before adding more", l, n)
		} else {
			t.Logf("%s: %d bytes", l, n)
		}
	}
}
