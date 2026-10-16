package coder

import "testing"

// TestInjectionSizeBudget: the ruleset is injected every session start --
// growth must be deliberate, not drift. Ceiling raised in steps, each in the
// same commit as the section that needed it, never silently: 6.5KB -> 8KB for
// the "Check your backstop" security section, 8KB -> 8.4KB for the
// observability keep, 8.4KB -> 8.6KB for the concurrency rule, 8.6KB -> 9.35KB
// for the three lessons from a real SSRF-review miss (review feedback is a
// claim to audit; bound the root-cause fix to the change; a guard covers one
// path -- grep the siblings), 9.35KB -> 9.6KB for "Match the toolchain" (never
// write syntax the project's declared go.mod/package.json/pyproject.toml/
// Cargo.toml version can't run -- a build break shipped as a fix), 9.6KB ->
// 10.1KB for the logging rule (code that can fail leaves a log behind,
// unasked, at error or warn -- never info/debug unasked, and never per item
// or per layer; it replaced the observability keep, which only protected
// logs from deletion and so never made Claude add one), 10.1KB -> 10.3KB for
// its two security clauses (a privileged action gets one info audit line, so
// coder mode stops writing what /deadeye-guard's logging: tag then flags;
// user input goes in a field, never spliced into the message -- log forging).
// With modest headroom.
func TestInjectionSizeBudget(t *testing.T) {
	for _, l := range []string{LevelSpotter, LevelMarksman, LevelSniper} {
		if n := len(Instructions(l)); n > 10300 {
			t.Errorf("%s injection is %d bytes -- trim before adding more", l, n)
		} else {
			t.Logf("%s: %d bytes", l, n)
		}
	}
}
