package prreview

import (
	"os"
	"strings"
	"testing"
)

// TestSkillMatchesRuleset keeps skills/deadeye-pr/SKILL.md byte-identical to
// the embedded rubric (same consistency-guard as the coder ruleset): Claude
// Code reads the file, `deadeye init` renders the embedded copy -- drift
// between them means two different reviews depending on the path.
func TestSkillMatchesRuleset(t *testing.T) {
	b, err := os.ReadFile("../../skills/deadeye-pr/SKILL.md")
	if err != nil {
		t.Fatalf("skills/deadeye-pr/SKILL.md missing: %v", err)
	}
	// Strip frontmatter: the file opens with "---\n"; the body starts
	// after the closing "\n---\n\n".
	parts := strings.SplitN(string(b), "\n---\n\n", 2)
	if len(parts) < 2 {
		t.Fatal("SKILL.md must have frontmatter delimited by ---")
	}
	if parts[1] != Body() {
		t.Error("skills/deadeye-pr/SKILL.md body drifted from internal/prreview/ruleset.md -- regenerate one from the other")
	}
}

// TestMarkerPresent: the sentinel init/uninstall grep for must actually be in
// the rubric, or the never-clobber guard silently never matches.
func TestMarkerPresent(t *testing.T) {
	if !strings.Contains(Body(), Marker) {
		t.Errorf("rubric missing Marker %q -- init/uninstall guards would never match", Marker)
	}
}

// TestNoTripleSingleQuote: the Gemini rendering wraps the rubric in a TOML
// literal string (”'...”'), so the rubric must never contain ”' itself
// or that file would become invalid TOML.
func TestNoTripleSingleQuote(t *testing.T) {
	if strings.Contains(Body(), "'''") {
		t.Error("rubric contains ''' -- would break the Gemini command's TOML literal string")
	}
}

// TestFitsWindsurfCap: Windsurf workflows cap at 12000 chars. The rendered
// file is the rubric plus a small host header, so the rubric alone must stay
// well under that ceiling.
func TestFitsWindsurfCap(t *testing.T) {
	if n := len(Body()); n > 11000 {
		t.Errorf("rubric is %d bytes -- trim it; the Windsurf workflow rendering must stay under 12000", n)
	}
}
