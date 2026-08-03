// Package coder is deadeye's lazy-senior-dev persona: a canonical ruleset
// injected at session start, filtered to one of three intensity levels
// (spotter/marksman/sniper), with a prompt-parsed mode tracker.
//
// Adapted from the ponytail plugin v4.8.4 by Dietrich Gebert (MIT,
// https://github.com/dietrichgebert/ponytail) -- the ladder, rules, and
// carve-outs are ported; the persona voice, level vocabulary, and marker
// convention are deadeye's own. See docs/THIRD-PARTY.md.
package coder

import (
	_ "embed"
	"regexp"
	"strings"
)

// ruleset is the canonical persona text. skills/deadeye-coder/SKILL.md
// carries the same body under frontmatter for Claude Code skill discovery;
// TestRulesetMatchesSkill is the canary that keeps the two identical.
//
//go:embed ruleset.md
var ruleset string

// Levels. "review" is session-only -- it can be switched to, never
// persisted as a default (ported guard: a review default would leave every
// new session in a mode that only makes sense for one task).
const (
	LevelOff      = "off"
	LevelSpotter  = "spotter"
	LevelMarksman = "marksman"
	LevelSniper   = "sniper"
	LevelReview   = "review"
)

// legacyAliases lets ponytail muscle-memory keep working -- accepted on
// input, never displayed.
var legacyAliases = map[string]string{
	"lite":  LevelSpotter,
	"full":  LevelMarksman,
	"ultra": LevelSniper,
}

// NormalizeLevel maps user input to a session-settable level. ok=false for
// anything unrecognized (caller decides the fallback -- ported behavior is
// "fall back to the configured default").
func NormalizeLevel(s string) (level string, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if a, hit := legacyAliases[s]; hit {
		return a, true
	}
	switch s {
	case LevelOff, LevelSpotter, LevelMarksman, LevelSniper, LevelReview:
		return s, true
	}
	return "", false
}

// NormalizePersisted is NormalizeLevel minus review -- the only levels a
// config default may hold.
func NormalizePersisted(s string) (level string, ok bool) {
	level, ok = NormalizeLevel(s)
	if !ok || level == LevelReview {
		return "", false
	}
	return level, true
}

// Active reports whether level means the persona is on.
func Active(level string) bool {
	return level == LevelSpotter || level == LevelMarksman || level == LevelSniper || level == LevelReview
}

var (
	// intensityRowRe matches an intensity-table row's level label.
	intensityRowRe = regexp.MustCompile(`^\|\s*\*\*([a-z]+)\*\*\s*\|`)
	// exampleRe matches a per-level worked-example bullet. The trailing
	// quote is REQUIRED (ported guard): without it, an ordinary rule
	// bullet that happens to start with a level word would be silently
	// dropped from the injection.
	exampleRe = regexp.MustCompile(`^-\s*([a-z]+):\s*"`)
)

func isLevelLabel(s string) bool {
	return s == LevelSpotter || s == LevelMarksman || s == LevelSniper
}

// FilterForLevel keeps only the active level's intensity-table row and
// worked example; every other line passes through verbatim.
func FilterForLevel(body, level string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if m := intensityRowRe.FindStringSubmatch(line); m != nil && isLevelLabel(m[1]) && m[1] != level {
			continue
		}
		if m := exampleRe.FindStringSubmatch(line); m != nil && isLevelLabel(m[1]) && m[1] != level {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// Instructions returns the injectable persona text for level: a banner
// line plus the level-filtered ruleset. Review mode is one line -- its
// behavior lives entirely in the deadeye-review skill.
func Instructions(level string) string {
	banner := "DEADEYE CODER ACTIVE — level: " + level
	if level == LevelReview {
		return banner + ". Behavior defined by the deadeye-review skill."
	}
	return banner + "\n\n" + FilterForLevel(ruleset, level)
}

// Ruleset returns the unfiltered canonical body (canary test, docs).
func Ruleset() string { return ruleset }

// Command is one parsed /deadeye-coder invocation.
type Command struct {
	Kind  CommandKind
	Level string // set for KindSwitch/KindDefault; already normalized
	Raw   string // the raw level token as typed (for error reporting)
}

type CommandKind int

const (
	KindNone         CommandKind = iota // not a coder command
	KindReport                          // bare /deadeye-coder
	KindSwitch                          // /deadeye-coder <level>
	KindSwitchBad                       // /deadeye-coder <garbage> -- caller falls back to default
	KindOff                             // /deadeye-coder off, or a deactivation phrase
	KindDefault                         // /deadeye-coder default <level>
	KindDefaultBad                      // /deadeye-coder default <garbage or review>
	KindReviewSwitch                    // /deadeye-review -- session level becomes review
)

// commandRe accepts /deadeye-coder, @deadeye-coder, $deadeye-coder, and
// the plugin-qualified /deadeye:deadeye-coder form. Captured live
// (testdata/payloads/): known slash commands arrive in UserPromptSubmit as
// the literal typed string.
var (
	coderCmdRe  = regexp.MustCompile(`^[/@$](?:deadeye:)?deadeye-coder(?:\s+(.*))?$`)
	reviewCmdRe = regexp.MustCompile(`^[/@$](?:deadeye:)?deadeye-review(?:\s|$)`)
)

// ParseCommand classifies a prompt. Deactivation phrases match by
// WHOLE-MESSAGE equality only -- ported bug guard: substring matching
// killed the mode mid-task on prompts like "add a normal mode toggle".
func ParseCommand(prompt string) Command {
	t := strings.TrimSpace(prompt)

	if IsDeactivation(t) {
		return Command{Kind: KindOff}
	}
	if reviewCmdRe.MatchString(t) {
		return Command{Kind: KindReviewSwitch, Level: LevelReview}
	}

	m := coderCmdRe.FindStringSubmatch(t)
	if m == nil {
		return Command{Kind: KindNone}
	}
	rest := strings.TrimSpace(m[1])
	if rest == "" {
		return Command{Kind: KindReport}
	}

	fields := strings.Fields(rest)
	if strings.EqualFold(fields[0], "default") {
		if len(fields) < 2 {
			return Command{Kind: KindDefaultBad, Raw: ""}
		}
		level, ok := NormalizePersisted(fields[1])
		if !ok {
			return Command{Kind: KindDefaultBad, Raw: fields[1]}
		}
		return Command{Kind: KindDefault, Level: level}
	}

	level, ok := NormalizeLevel(fields[0])
	if !ok {
		return Command{Kind: KindSwitchBad, Raw: fields[0]}
	}
	if level == LevelOff {
		return Command{Kind: KindOff}
	}
	return Command{Kind: KindSwitch, Level: level}
}

// deactivationPhrases end coder mode when a message consists of exactly
// one of them (after lowercasing and stripping trailing punctuation).
var deactivationPhrases = []string{
	"normal mode",
	"stop coder",
	"stop deadeye coder",
}

var trailingPunct = regexp.MustCompile(`[.!?\s]+$`)

// IsDeactivation reports whether prompt is a whole-message deactivation.
func IsDeactivation(prompt string) bool {
	t := strings.ToLower(strings.TrimSpace(prompt))
	t = trailingPunct.ReplaceAllString(t, "")
	for _, p := range deactivationPhrases {
		if t == p {
			return true
		}
	}
	return false
}

// shellSafeRe is the ported allowlist for paths embedded into a suggested
// shell command (the statusline nudge) -- anything outside it gets the
// manual-setup message instead of an embedded path.
var shellSafeRe = regexp.MustCompile(`^[A-Za-z0-9 _.\-:/\\~]+$`)

// IsShellSafe reports whether path can be embedded in a suggested command
// without quoting hazards.
func IsShellSafe(path string) bool {
	return path != "" && shellSafeRe.MatchString(path)
}
