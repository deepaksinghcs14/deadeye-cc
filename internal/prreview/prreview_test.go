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

// TestReviewSkillMatchesRuleset mirrors TestSkillMatchesRuleset for the
// working-diff/whole-repo rubric: skills/deadeye-review/SKILL.md must carry
// SelfBody() verbatim, or Claude Code and every other host diverge.
func TestReviewSkillMatchesRuleset(t *testing.T) {
	b, err := os.ReadFile("../../skills/deadeye-review/SKILL.md")
	if err != nil {
		t.Fatalf("skills/deadeye-review/SKILL.md missing: %v", err)
	}
	parts := strings.SplitN(string(b), "\n---\n\n", 2)
	if len(parts) < 2 {
		t.Fatal("SKILL.md must have frontmatter delimited by ---")
	}
	if parts[1] != SelfBody() {
		t.Error("skills/deadeye-review/SKILL.md body drifted from internal/prreview/review.md -- regenerate one from the other")
	}
}

// TestSelfBodyHasNoGitHub: SelfBody() must stay usable with no GitHub PR in
// sight -- a working-diff or whole-repo review has no `gh` context to
// resolve, no other reviewers' comments to dedup against, and nothing to
// post.
func TestSelfBodyHasNoGitHub(t *testing.T) {
	for _, must_not := range []string{"gh pr", "--post", "Posting back to the PR", "CodeRabbit"} {
		if strings.Contains(SelfBody(), must_not) {
			t.Errorf("SelfBody() contains %q -- PR-only content leaked into the self-review rubric", must_not)
		}
	}
}

// TestBodiesShareLenses pins "identical to pr-review": every tag across all
// four lenses must appear in both Body() and SelfBody(), or the two rubrics
// have drifted apart -- exactly the bug this widening exists to prevent.
func TestBodiesShareLenses(t *testing.T) {
	tags := []string{
		"delete:", "stdlib:", "native:", "yagni:", "shrink:",
		"logic:", "nil:", "race:", "bound:", "contract:", "leak:", "break:", "untested:", "a11y:",
		"alloc:", "nplus1:", "complexity:", "blocking:", "copy:",
		"inject:", "secret:", "authz:", "crypto:", "expose:", "dep:", "dos:",
		"ssrf:", "authn:", "bizlogic:", "massassign:", "validation:", "ratelimit:",
		"config:", "integrity:", "logging:", "inventory:", "thirdparty:", "exceptions:", "llm:",
	}
	for _, tag := range tags {
		inPR := strings.Contains(Body(), tag)
		inSelf := strings.Contains(SelfBody(), tag)
		if !inPR || !inSelf {
			t.Errorf("tag %q: in Body()=%v, in SelfBody()=%v -- both rubrics must carry every tag", tag, inPR, inSelf)
		}
	}
}

// TestPentestTagDisambiguation pins the overlap fixes applied when the
// Security lens grew from 7 to 20 tags -- the vapt acceptance test found
// exactly this ambiguity (inject/integrity on deserialization, expose/
// exceptions on stack traces) after the fact; here it's pinned from the
// start so a future edit can't silently reintroduce it.
func TestPentestTagDisambiguation(t *testing.T) {
	for _, b := range []string{Body(), SelfBody()} {
		if strings.Contains(b, "a URL fetch (SSRF)") {
			t.Error("inject: still carries the SSRF parenthetical -- ssrf: is now a first-class tag, the split wasn't applied")
		}
		if !strings.Contains(b, "not this") {
			t.Error("integrity: is missing its disambiguation clause pointing deserialization-that-executes-code at inject: instead")
		}
		if !strings.Contains(b, "ERROR-path counterpart") {
			t.Error("exceptions: is missing its disambiguation clause distinguishing it from expose:'s normal-path scope")
		}
		if !strings.Contains(b, "not the allocation shape") {
			t.Error("ratelimit: is missing its disambiguation clause distinguishing it from dos:'s allocation shape")
		}
	}
}

// TestMarkerPresent: the sentinel init/uninstall grep for must actually be in
// the rubric, or the never-clobber guard silently never matches.
func TestMarkerPresent(t *testing.T) {
	if !strings.Contains(Body(), Marker) {
		t.Errorf("rubric missing Marker %q -- init/uninstall guards would never match", Marker)
	}
	if !strings.Contains(SelfBody(), SelfMarker) {
		t.Errorf("self-review rubric missing SelfMarker %q -- init/uninstall guards would never match", SelfMarker)
	}
}

// TestNoTripleSingleQuote: the Gemini rendering wraps the rubric in a TOML
// literal string (”'...”'), so the rubric must never contain ”' itself
// or that file would become invalid TOML.
func TestNoTripleSingleQuote(t *testing.T) {
	if strings.Contains(Body(), "'''") {
		t.Error("rubric contains ''' -- would break the Gemini command's TOML literal string")
	}
	if strings.Contains(SelfBody(), "'''") {
		t.Error("self-review rubric contains ''' -- would break the Gemini command's TOML literal string")
	}
}

// TestFitsWindsurfCap: Windsurf workflows cap at 12000 CHARACTERS. Only the
// Windsurf rendering must fit -- WindsurfBody() drops the Rigor section and
// the opt-in Posting section for that host, while the flagship Body()
// (Claude/Codex/Gemini + the skill) is free to grow. The rendered file is
// WindsurfBody() plus a ~220-char host header, so the trimmed body (chars,
// not bytes -- em-dashes/glyphs are multi-byte) must stay under ~11700 to
// leave header room under 12000.
func TestFitsWindsurfCap(t *testing.T) {
	if n := len([]rune(WindsurfBody())); n > 11700 {
		t.Errorf("Windsurf rubric is %d chars -- trim it; the rendered workflow must stay under 12000", n)
	}
	// Guard against over-trimming: the Windsurf variant must still carry the
	// four lenses and the verify-before-reporting/proof discipline.
	for _, must := range []string{"## The four lenses", "## Verify before reporting", "inject:", "logic:"} {
		if !strings.Contains(WindsurfBody(), must) {
			t.Errorf("WindsurfBody() is missing %q -- over-trimmed", must)
		}
	}
}

// TestSelfFitsWindsurfCap mirrors TestFitsWindsurfCap for the self-review
// rubric.
func TestSelfFitsWindsurfCap(t *testing.T) {
	if n := len([]rune(SelfWindsurfBody())); n > 11700 {
		t.Errorf("Windsurf self-review rubric is %d chars -- trim it; the rendered workflow must stay under 12000", n)
	}
	for _, must := range []string{"## The four lenses", "inject:", "logic:"} {
		if !strings.Contains(SelfWindsurfBody(), must) {
			t.Errorf("SelfWindsurfBody() is missing %q -- over-trimmed", must)
		}
	}
}

// TestWindsurfDropsRigor pins the actual trim contract: the "Rigor -- where
// reviews miss" habits are cut for Windsurf's char budget (present in
// Body(), absent from WindsurfBody()). The strings asserted by
// TestFitsWindsurfCap all sit OUTSIDE the trim window and can never be
// removed by WindsurfBody(), so that test alone would keep passing even if
// the Rigor section were silently restored (blowing the char cap) or its
// removal silently changed shape -- this test fails on either drift.
func TestWindsurfDropsRigor(t *testing.T) {
	const rigor = "Sweep every instance."
	if !strings.Contains(Body(), rigor) {
		t.Fatalf("test fixture stale: %q no longer in Body() -- update this test alongside the Rigor section", rigor)
	}
	if strings.Contains(WindsurfBody(), rigor) {
		t.Error("WindsurfBody() carries the Rigor section -- if that's now intentional, update prreview.go's doc comments and README's Windsurf caveat too")
	}
}

// TestWindsurfDropsLearningLoop pins the "Learning loop" trim the same way
// TestWindsurfDropsRigor pins Rigor: the section calls a `deadeye` CLI
// Windsurf has no guaranteed binary for, so it's cut there (present in
// Body(), absent from WindsurfBody()) rather than eating into the ~18-char
// headroom TestFitsWindsurfCap enforces.
func TestWindsurfDropsLearningLoop(t *testing.T) {
	const marker = "deadeye lessons priority"
	if !strings.Contains(Body(), marker) {
		t.Fatalf("test fixture stale: %q no longer in Body() -- update this test alongside the Learning loop section", marker)
	}
	if strings.Contains(WindsurfBody(), marker) {
		t.Error("WindsurfBody() carries the Learning loop section -- it calls a deadeye CLI Windsurf has no guaranteed binary for, and there's no char budget left to keep it")
	}
}

// TestWindsurfDropsSuggestedFixes pins the "Suggested fixes"/"Copy for AI"
// trim the same way as Rigor and Learning loop: GitHub suggestion blocks
// and the deadeye-report loop are flagship-only conveniences, cut for
// Windsurf's char budget rather than eating the ~18-char headroom
// TestFitsWindsurfCap enforces.
func TestWindsurfDropsSuggestedFixes(t *testing.T) {
	const marker = "one-click \"Apply suggestion\" button"
	if !strings.Contains(Body(), marker) {
		t.Fatalf("test fixture stale: %q no longer in Body() -- update this test alongside the Suggested fixes section", marker)
	}
	if strings.Contains(WindsurfBody(), marker) {
		t.Error("WindsurfBody() carries the Suggested fixes section -- there's no char budget left to keep it")
	}
	if strings.Contains(WindsurfBody(), "## Copy for AI") {
		t.Error("WindsurfBody() carries the Copy for AI section -- there's no char budget left to keep it")
	}
}

// TestWindsurfDropsPentestTags pins the 13-tag pentest-tags trim the same
// way as Rigor/Learning loop/Suggested fixes: the block is large enough
// (~1000 chars) to blow the char cap on its own, and the original 7-tag
// Security lens is fully functional without it -- present in Body(),
// absent from WindsurfBody().
func TestWindsurfDropsPentestTags(t *testing.T) {
	const marker = "ssrf:"
	if !strings.Contains(Body(), marker) {
		t.Fatalf("test fixture stale: %q no longer in Body() -- update this test alongside the pentest-tags block", marker)
	}
	if strings.Contains(WindsurfBody(), marker) {
		t.Error("WindsurfBody() carries the pentest-tags block -- there's no char budget left to keep it")
	}
	// The original 7 must survive the cut -- Windsurf keeps a fully
	// functional Security lens, just the pre-widening one.
	for _, tag := range []string{"inject:", "secret:", "authz:", "crypto:", "expose:", "dep:", "dos:"} {
		if !strings.Contains(WindsurfBody(), tag) {
			t.Errorf("WindsurfBody() is missing original Security tag %q -- over-trimmed", tag)
		}
	}
}

// TestSelfWindsurfDropsSections mirrors the WindsurfDrops* tests above for
// SelfBody(): Rigor, Learning loop, and Suggested fixes/Copy for AI are cut
// for the same reasons; the whole-repo `--repo` section is cut too, as the
// single largest section and the least useful on a workflow surface with no
// persistent CLI access.
func TestSelfWindsurfDropsSections(t *testing.T) {
	cases := []struct {
		name   string
		marker string
	}{
		{"Rigor", "Sweep every instance."},
		{"whole-repo mode", "Scope cheaply"},
		{"pentest tags", "ssrf:"},
		{"Learning loop", "deadeye lessons priority"},
		{"Suggested fixes", "concrete and mechanical"},
	}
	for _, c := range cases {
		if !strings.Contains(SelfBody(), c.marker) {
			t.Fatalf("test fixture stale: %q no longer in SelfBody() -- update this test alongside the %s section", c.marker, c.name)
		}
		if strings.Contains(SelfWindsurfBody(), c.marker) {
			t.Errorf("SelfWindsurfBody() carries the %s section -- there's no char budget left to keep it", c.name)
		}
	}
	for _, tag := range []string{"inject:", "secret:", "authz:", "crypto:", "expose:", "dep:", "dos:"} {
		if !strings.Contains(SelfWindsurfBody(), tag) {
			t.Errorf("SelfWindsurfBody() is missing original Security tag %q -- over-trimmed", tag)
		}
	}
}
