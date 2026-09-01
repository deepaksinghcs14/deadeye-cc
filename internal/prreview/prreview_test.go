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

// TestFitsWindsurfCap: Windsurf workflows cap at 12000 CHARACTERS. Only the
// Windsurf rendering must fit -- WindsurfBody() drops the opt-in Posting section
// for that host, while the flagship Body() (Claude/Codex/Gemini + the skill) is
// free to grow. The rendered file is WindsurfBody() plus a ~220-char host
// header, so the trimmed body (chars, not bytes -- em-dashes/glyphs are
// multi-byte) must stay under ~11700 to leave header room under 12000.
func TestFitsWindsurfCap(t *testing.T) {
	if n := len([]rune(WindsurfBody())); n > 11700 {
		t.Errorf("Windsurf rubric is %d chars -- trim it; the rendered workflow must stay under 12000", n)
	}
	// Guard against over-trimming: the Windsurf variant must still carry the
	// lenses and the rigor section, not just a header.
	for _, must := range []string{"## The four lenses", "## Verify before reporting", "inject:", "logic:"} {
		if !strings.Contains(WindsurfBody(), must) {
			t.Errorf("WindsurfBody() is missing %q -- over-trimmed", must)
		}
	}
}
