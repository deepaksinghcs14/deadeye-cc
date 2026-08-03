package coder

import (
	"os"
	"strings"
	"testing"
)

// TestRulesetMatchesSkill is the canary keeping the embedded ruleset and
// skills/deadeye-coder/SKILL.md byte-identical (ported consistency-guard
// pattern): the daemon injects the embedded copy, Claude Code's skill
// loader reads the file -- drift between them means the user sees two
// different personas depending on the path.
func TestRulesetMatchesSkill(t *testing.T) {
	b, err := os.ReadFile("../../skills/deadeye-coder/SKILL.md")
	if err != nil {
		t.Fatalf("skills/deadeye-coder/SKILL.md missing: %v", err)
	}
	s := string(b)
	// Strip frontmatter: the file opens with "---\n"; the body starts
	// after the closing "\n---\n\n" (the newline prefix distinguishes the
	// closing delimiter from the opening line).
	parts := strings.SplitN(s, "\n---\n\n", 2)
	if len(parts) < 2 {
		t.Fatal("SKILL.md must have frontmatter delimited by ---")
	}
	body := parts[1]
	if body != Ruleset() {
		t.Error("skills/deadeye-coder/SKILL.md body has drifted from internal/coder/ruleset.md -- regenerate one from the other")
	}
}

// TestRulesetCarriesInvariants: load-bearing phrases that must never be
// edited away (ported INVARIANTS-canary pattern). These are the safety
// carve-outs and core discipline, not style.
func TestRulesetCarriesInvariants(t *testing.T) {
	for _, inv := range []string{
		"input validation at trust boundaries",
		"prevents data loss",
		"security",
		"accessibility",
		"ONE runnable check",
		"root cause",
		"deadeye:",
		"Already in this codebase?",
	} {
		if !strings.Contains(ruleset, inv) {
			t.Errorf("ruleset lost invariant phrase %q", inv)
		}
	}
}

func TestFilterKeepsOnlyActiveLevel(t *testing.T) {
	for _, level := range []string{LevelSpotter, LevelMarksman, LevelSniper} {
		got := FilterForLevel(ruleset, level)
		if !strings.Contains(got, "| **"+level+"** |") {
			t.Errorf("%s: own intensity row missing", level)
		}
		if !strings.Contains(got, "- "+level+`: "`) {
			t.Errorf("%s: own worked example missing", level)
		}
		for _, other := range []string{LevelSpotter, LevelMarksman, LevelSniper} {
			if other == level {
				continue
			}
			if strings.Contains(got, "| **"+other+"** |") {
				t.Errorf("%s: leaked %s's intensity row", level, other)
			}
			if strings.Contains(got, "- "+other+`: "`) {
				t.Errorf("%s: leaked %s's worked example", level, other)
			}
		}
	}
}

// TestFilterTrailingQuoteGuard is the ported bug guard: a normal bullet
// that merely STARTS with a level word (no quoted example) must survive
// every level's filter.
func TestFilterTrailingQuoteGuard(t *testing.T) {
	body := "- spotter duty is calling shots, not taking them\n- sniper: \"quoted example\"\n"
	got := FilterForLevel(body, LevelMarksman)
	if !strings.Contains(got, "spotter duty is calling shots") {
		t.Error("unquoted bullet starting with a level word was wrongly dropped")
	}
	if strings.Contains(got, "quoted example") {
		t.Error("other level's quoted example survived")
	}
}

// TestFilterIgnoresNonLevelTableRows: bold table labels that aren't levels
// (e.g. a features table) must pass through untouched.
func TestFilterIgnoresNonLevelTableRows(t *testing.T) {
	body := "| **output** | trims stuff |\n| **sniper** | one shot |\n"
	got := FilterForLevel(body, LevelMarksman)
	if !strings.Contains(got, "**output**") {
		t.Error("non-level table row was dropped")
	}
	if strings.Contains(got, "**sniper**") {
		t.Error("sniper row survived a marksman filter")
	}
}

func TestInstructionsBannerAndReview(t *testing.T) {
	got := Instructions(LevelSniper)
	if !strings.HasPrefix(got, "DEADEYE CODER ACTIVE — level: sniper") {
		t.Errorf("banner wrong: %q", got[:60])
	}
	review := Instructions(LevelReview)
	if strings.Contains(review, "The ladder") {
		t.Error("review instructions must be the one-liner, not the ruleset")
	}
	if !strings.Contains(review, "deadeye-review") {
		t.Errorf("review one-liner must point at the review skill: %q", review)
	}
}

func TestNormalizeLevelAndAliases(t *testing.T) {
	cases := map[string]string{
		"spotter": LevelSpotter, "MARKSMAN": LevelMarksman, " sniper ": LevelSniper,
		"off": LevelOff, "review": LevelReview,
		// ponytail muscle-memory aliases
		"lite": LevelSpotter, "full": LevelMarksman, "ultra": LevelSniper,
	}
	for in, want := range cases {
		got, ok := NormalizeLevel(in)
		if !ok || got != want {
			t.Errorf("NormalizeLevel(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	if _, ok := NormalizeLevel("maximum"); ok {
		t.Error("garbage level normalized")
	}
	if _, ok := NormalizePersisted("review"); ok {
		t.Error("review must never be persistable as a default")
	}
}

func TestParseCommand(t *testing.T) {
	cases := []struct {
		prompt string
		kind   CommandKind
		level  string
	}{
		{"/deadeye-coder", KindReport, ""},
		{"/deadeye-coder sniper", KindSwitch, LevelSniper},
		{"/deadeye-coder ultra", KindSwitch, LevelSniper}, // legacy alias
		{"/deadeye:deadeye-coder spotter", KindSwitch, LevelSpotter},
		{"@deadeye-coder marksman", KindSwitch, LevelMarksman},
		{"/deadeye-coder off", KindOff, ""},
		{"/deadeye-coder blastoff", KindSwitchBad, ""},
		{"/deadeye-coder default spotter", KindDefault, LevelSpotter},
		{"/deadeye-coder default review", KindDefaultBad, ""},
		{"/deadeye-coder default", KindDefaultBad, ""},
		{"/deadeye-review", KindReviewSwitch, LevelReview},
		{"/deadeye-review this diff please", KindReviewSwitch, LevelReview},
		{"normal mode", KindOff, ""},
		{"Stop coder!", KindOff, ""},
		{"stop deadeye coder.", KindOff, ""},
		{"add a normal mode toggle", KindNone, ""}, // ported substring-bug guard
		{"fix the bug in main.go", KindNone, ""},
		{"/deadeye-audit", KindNone, ""}, // sibling command, not ours
	}
	for _, c := range cases {
		got := ParseCommand(c.prompt)
		if got.Kind != c.kind {
			t.Errorf("ParseCommand(%q).Kind = %v, want %v", c.prompt, got.Kind, c.kind)
		}
		if c.level != "" && got.Level != c.level {
			t.Errorf("ParseCommand(%q).Level = %q, want %q", c.prompt, got.Level, c.level)
		}
	}
}

func TestIsShellSafe(t *testing.T) {
	if !IsShellSafe("/Users/dev/.claude/plugins/deadeye/hooks/deadeye-statusline.sh") {
		t.Error("ordinary path rejected")
	}
	for _, bad := range []string{"", `/tmp/x"; rm -rf ~; "`, "/tmp/$(whoami)", "/tmp/a'b"} {
		if IsShellSafe(bad) {
			t.Errorf("unsafe path accepted: %q", bad)
		}
	}
}
