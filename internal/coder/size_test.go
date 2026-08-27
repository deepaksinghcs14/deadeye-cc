package coder

import "testing"

// TestInjectionSizeBudget: the ruleset is injected every session start --
// growth must be deliberate, not drift. Ceiling raised in steps, each in the
// same commit as the section that needed it, never silently: 6.5KB -> 8KB for
// the "Check your backstop" security section, 8KB -> 8.4KB for the
// observability keep. With modest headroom.
func TestInjectionSizeBudget(t *testing.T) {
	for _, l := range []string{LevelSpotter, LevelMarksman, LevelSniper} {
		if n := len(Instructions(l)); n > 8400 {
			t.Errorf("%s injection is %d bytes -- trim before adding more", l, n)
		} else {
			t.Logf("%s: %d bytes", l, n)
		}
	}
}
