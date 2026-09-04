package vapt

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestSkillMatchesRuleset keeps skills/deadeye-vapt/SKILL.md byte-identical
// to the embedded rubric (same consistency-guard as internal/prreview and
// internal/coder): Claude Code reads the file, `deadeye init` renders the
// embedded copy -- drift between them means two different reviews
// depending on the path.
func TestSkillMatchesRuleset(t *testing.T) {
	b, err := os.ReadFile("../../skills/deadeye-vapt/SKILL.md")
	if err != nil {
		t.Fatalf("skills/deadeye-vapt/SKILL.md missing: %v", err)
	}
	parts := strings.SplitN(string(b), "\n---\n\n", 2)
	if len(parts) < 2 {
		t.Fatal("SKILL.md must have frontmatter delimited by ---")
	}
	if parts[1] != Body() {
		t.Error("skills/deadeye-vapt/SKILL.md body drifted from internal/vapt/ruleset.md -- regenerate one from the other")
	}
}

// TestMarkerPresent: the sentinel init/uninstall grep for must actually be
// in the rubric, or the never-clobber guard silently never matches.
func TestMarkerPresent(t *testing.T) {
	if !strings.Contains(Body(), Marker) {
		t.Errorf("rubric missing Marker %q -- init/uninstall guards would never match", Marker)
	}
}

// TestNoTripleSingleQuote: the Gemini rendering wraps the rubric in a TOML
// literal string (”'...”'), so the rubric must never contain ”' itself or
// that file would become invalid TOML.
func TestNoTripleSingleQuote(t *testing.T) {
	if strings.Contains(Body(), "'''") {
		t.Error("rubric contains ''' -- would break the Gemini command's TOML literal string")
	}
}

// TestFitsWindsurfCap mirrors internal/prreview's test of the same name:
// Windsurf workflows cap at 12000 CHARACTERS, and the rendered file is
// WindsurfBody() plus a ~220-char host header, so the trimmed body must
// stay under ~11700 to leave header room.
func TestFitsWindsurfCap(t *testing.T) {
	if n := len([]rune(WindsurfBody())); n > 11700 {
		t.Errorf("Windsurf rubric is %d chars -- trim it; the rendered workflow must stay under 12000", n)
	}
	for _, must := range []string{"## The eighteen tags", "## Report format", "authz:", "inject:"} {
		if !strings.Contains(WindsurfBody(), must) {
			t.Errorf("WindsurfBody() is missing %q -- over-trimmed", must)
		}
	}
}

// TestWindsurfDropsIDTables pins the actual trim contract: the three
// OWASP/API/LLM id-mapping tables are cut for Windsurf's char budget
// (present in Body(), absent from WindsurfBody()) -- but the small
// Reference table of real OWASP URLs must survive the cut, since `link:`
// on every finding is mandatory regardless of host.
func TestWindsurfDropsIDTables(t *testing.T) {
	const marker = "Broken Object Level Authorization"
	if !strings.Contains(Body(), marker) {
		t.Fatalf("test fixture stale: %q no longer in Body() -- update this test alongside the coverage tables", marker)
	}
	if strings.Contains(WindsurfBody(), marker) {
		t.Error("WindsurfBody() carries the OWASP id-mapping tables -- if that's now intentional, update vapt.go's doc comments")
	}
	if !strings.Contains(WindsurfBody(), "owasp.org/Top10/2025") {
		t.Error("WindsurfBody() lost the Reference table of real OWASP URLs -- link: is mandatory on every finding regardless of host")
	}
}

// TestWindsurfDropsLearningLoop mirrors internal/prreview's equivalent: the
// section calls a deadeye CLI Windsurf has no guaranteed binary for.
func TestWindsurfDropsLearningLoop(t *testing.T) {
	const marker = "deadeye lessons priority"
	if !strings.Contains(Body(), marker) {
		t.Fatalf("test fixture stale: %q no longer in Body() -- update this test alongside the Learning loop section", marker)
	}
	if strings.Contains(WindsurfBody(), marker) {
		t.Error("WindsurfBody() carries the Learning loop section -- it calls a deadeye CLI Windsurf has no guaranteed binary for")
	}
}

// TestCoverageComplete is the direct check on this feature's core promise
// ("I want all owasp, no things left behind"): every OWASP Top 10:2025,
// API Security Top 10 2023, and LLM Top 10:2025 category id must appear in
// the rubric, and every one of the eighteen tags must appear too. A
// category present in name only (missing its id) would let coverage silently
// erode as the rubric is edited.
func TestCoverageComplete(t *testing.T) {
	ids := []string{
		"A01:2025", "A02:2025", "A03:2025", "A04:2025", "A05:2025",
		"A06:2025", "A07:2025", "A08:2025", "A09:2025", "A10:2025",
		"API1", "API2", "API3", "API4", "API5", "API6", "API7", "API8", "API9", "API10",
		"LLM01", "LLM02", "LLM03", "LLM04", "LLM05", "LLM06", "LLM07", "LLM08", "LLM09", "LLM10",
	}
	for _, id := range ids {
		if !strings.Contains(Body(), id) {
			t.Errorf("OWASP id %q missing from the rubric -- coverage regressed", id)
		}
	}
	tags := []string{
		"authn:", "authz:", "bizlogic:", "inject:", "ssrf:", "massassign:",
		"expose:", "validation:", "ratelimit:", "crypto:", "config:", "dep:",
		"integrity:", "logging:", "inventory:", "thirdparty:", "llm:", "exceptions:",
	}
	for _, tag := range tags {
		if !strings.Contains(Body(), tag) {
			t.Errorf("tag %q missing from the rubric", tag)
		}
	}
}

// TestCoverageMatrixMandatory pins that the rubric itself requires the
// per-category coverage matrix and forbids treating it as optional --
// dropping this instruction would let a future edit quietly relax the
// "nothing left behind" contract.
func TestCoverageMatrixMandatory(t *testing.T) {
	for _, must := range []string{"mandatory coverage matrix", "never be silently absent", "not reached"} {
		if !strings.Contains(Body(), must) {
			t.Errorf("rubric missing %q -- the mandatory coverage matrix instruction regressed", must)
		}
	}
}

// TestNoLiveTrafficClaim guards the scope boundary from the other
// direction: the rubric must never claim or imply it sends real traffic or
// runs an exploit -- it is a static, source-only pass.
func TestNoLiveTrafficClaim(t *testing.T) {
	if !strings.Contains(Body(), "No traffic is sent") {
		t.Error("rubric no longer states the no-live-traffic scope boundary")
	}
}

// TestSectionHeadingsUnique guards cutSection's contract: each heading
// cutSection targets must appear exactly once, or a cut silently binds to
// the wrong occurrence.
func TestSectionHeadingsUnique(t *testing.T) {
	headingRe := regexp.MustCompile(`(?m)^##[^#].*$`)
	seen := map[string]int{}
	for _, h := range headingRe.FindAllString(Body(), -1) {
		seen[h]++
	}
	for h, n := range seen {
		if n > 1 {
			t.Errorf("heading %q appears %d times -- cutSection targeting it would bind to the wrong occurrence", h, n)
		}
	}
}
