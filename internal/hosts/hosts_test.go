package hosts

import "testing"

// TestHostCapabilities pins the contract the deciders rely on: Claude Code
// (empty/"claude") has the full surface; every reduced host -- Codex AND
// Gemini -- lacks both the subagent surface and raw injection, so Gemini
// inherits Codex's reduced behavior for free.
func TestHostCapabilities(t *testing.T) {
	cases := []struct {
		host     string
		subagent bool
		rawInj   bool
	}{
		{"", true, true},         // default = Claude Code
		{"claude", true, true},   // explicit Claude
		{"codex", false, false},  // reduced host
		{"gemini", false, false}, // reduced host -- same as codex
	}
	for _, c := range cases {
		if got := HasSubagentSurface(c.host); got != c.subagent {
			t.Errorf("HasSubagentSurface(%q) = %v, want %v", c.host, got, c.subagent)
		}
		if got := UsesRawInjection(c.host); got != c.rawInj {
			t.Errorf("UsesRawInjection(%q) = %v, want %v", c.host, got, c.rawInj)
		}
	}
}
