// Package prreview holds the canonical review rubric -- the single source
// of truth for both the /deadeye-pr and /deadeye-review capabilities. The
// four lenses, verify-before-reporting/proof discipline, suggested fixes,
// and copy-for-AI block are GitHub-agnostic and shared verbatim (lenses.md,
// fixes.md) between a PR wrapper (ruleset.md) and a working-diff/whole-repo
// wrapper (review.md). The Claude Code skills (skills/deadeye-pr/SKILL.md,
// skills/deadeye-review/SKILL.md) carry these bodies verbatim (canary tests
// keep them identical), and `deadeye init <host>` renders both into each
// other host's on-demand command file. One rubric, many surfaces.
package prreview

import (
	_ "embed"
	"strings"
)

//go:embed ruleset.md
var prTemplate string

//go:embed review.md
var reviewTemplate string

//go:embed lenses.md
var lensesFragment string

//go:embed fixes.md
var fixesFragment string

var body string
var selfBody string

func init() {
	body = expand(prTemplate)
	selfBody = expand(reviewTemplate)
}

// expand substitutes the shared {{lenses}}/{{fixes}} fragments into a
// wrapper template. Plain string substitution, not a template engine --
// the wrappers place these fragments as whole sections, never mid-sentence.
func expand(tmpl string) string {
	s := strings.ReplaceAll(tmpl, "{{lenses}}", strings.TrimRight(lensesFragment, "\n"))
	s = strings.ReplaceAll(s, "{{fixes}}", strings.TrimRight(fixesFragment, "\n"))
	return s
}

// Marker is a stable sentinel present in the PR rubric (and therefore in
// every rendering and the Claude skill). init greps for it to refuse
// clobbering a file it didn't write; uninstall greps for it to confirm a
// file is ours.
const Marker = "deadeye-pr: canonical rubric"

// SelfMarker is Marker's counterpart for the working-diff/whole-repo rubric.
const SelfMarker = "deadeye-review: canonical rubric"

// Body returns the canonical host-neutral PR-review rubric -- the exact body
// of the Claude skill, and the shared core every host rendering wraps.
func Body() string { return body }

// SelfBody returns the canonical host-neutral self-review rubric (working
// diff by default, whole repo with --repo) -- the exact body of the
// deadeye-review Claude skill, and the shared core every host rendering
// wraps. Runs the identical four lenses as Body(), scoped locally instead
// of at a GitHub PR.
func SelfBody() string { return selfBody }

// cutSection removes the text from the start of the "from" heading up to
// (not including) the start of the "to" heading. A no-op if either heading
// isn't present, so callers can chain cuts without checking existence.
func cutSection(s, from, to string) string {
	i := strings.Index(s, from)
	if i < 0 {
		return s
	}
	j := strings.Index(s[i:], to)
	if j < 0 {
		return s
	}
	return s[:i] + s[i+j:]
}

// WindsurfBody returns the rubric trimmed to fit Windsurf's hard 12000-char
// workflow cap. Every other host gets the full Body(); Windsurf (experimental,
// no hook contract) drops FOUR sections to fit: the "Rigor -- where reviews
// miss" habits (the largest single section), the "Learning loop" section,
// "Suggested fixes"/"Copy for AI" (all three call a `deadeye` CLI or lean on
// `gh`'s suggestion rendering Windsurf has no guaranteed binary/UI for, so
// they have nothing to read there anyway), and the opt-in "Posting back to
// the PR" section (the least relevant there, reduced to a one-line pointer).
// This lets the flagship rubric grow without the weakest host capping the
// best reviewer -- Windsurf still gets the four lenses and the
// verify-before-reporting/proof discipline, just not the Rigor upgrade, the
// cross-session learning loop, or the fix-acceleration extras.
func WindsurfBody() string {
	b := body
	b = cutSection(b, "## Rigor", "## The four lenses")
	b = cutSection(b, "## Learning loop", "## Output")
	b = cutSection(b, "## Suggested fixes", "## Posting back to the PR")
	// Drop the opt-in "Posting back to the PR" section, leave a one-line pointer.
	const h = "## Posting back to the PR"
	if i := strings.Index(b, h); i >= 0 {
		b = b[:i] + h + "\n\nPrint the review. To post it, run `gh pr review <N> --comment` yourself.\n"
	}
	return b
}

// SelfWindsurfBody is WindsurfBody's counterpart for SelfBody(): drops
// Rigor, the whole-repo `--repo` mode (the single largest section and the
// least useful on a workflow surface with no persistent CLI access),
// Learning loop, and Suggested fixes/Copy for AI -- same reasoning as
// WindsurfBody, no posting section to trim here since self-review never
// posts anywhere.
func SelfWindsurfBody() string {
	b := selfBody
	b = cutSection(b, "## Rigor", "## The four lenses")
	b = cutSection(b, "## Whole-repo mode", "## Learning loop")
	b = cutSection(b, "## Learning loop", "## Output")
	b = cutSection(b, "## Suggested fixes", "## Boundaries")
	return b
}
