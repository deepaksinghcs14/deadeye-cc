// Package prreview holds the canonical PR-review rubric -- the single
// source of truth for the /deadeye-pr capability. The Claude Code skill
// (skills/deadeye-pr/SKILL.md) carries this body verbatim (a canary test
// keeps them identical), and `deadeye init <host>` renders it into each
// other host's on-demand command file. One rubric, many surfaces.
package prreview

import (
	_ "embed"
	"strings"
)

//go:embed ruleset.md
var body string

// Marker is a stable sentinel present in the rubric (and therefore in every
// rendering and the Claude skill). init greps for it to refuse clobbering a
// file it didn't write; uninstall greps for it to confirm a file is ours.
const Marker = "deadeye-pr: canonical rubric"

// Body returns the canonical host-neutral PR-review rubric -- the exact body
// of the Claude skill, and the shared core every host rendering wraps.
func Body() string { return body }

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
	// Drop the flagship-only "Rigor" section (the base rubric -- lenses,
	// verify, proof, dedup -- still ships to Windsurf; the Rigor section's
	// own sibling-path sweep, mitigation-disproof, and cheap-layer checklist
	// do not).
	if s := strings.Index(b, "## Rigor"); s >= 0 {
		if e := strings.Index(b[s:], "## The four lenses"); e >= 0 {
			b = b[:s] + b[s+e:]
		}
	}
	// Drop "Learning loop" -- it's a `deadeye` CLI call Windsurf has no
	// guaranteed binary to run, same reasoning as the Rigor cut above.
	if s := strings.Index(b, "## Learning loop"); s >= 0 {
		if e := strings.Index(b[s:], "## Output"); e >= 0 {
			b = b[:s] + b[s+e:]
		}
	}
	// Drop "Suggested fixes" and "Copy for AI" -- same reasoning: GitHub
	// suggestion blocks and the deadeye-report loop above are both
	// flagship-only conveniences, not core review correctness. Windsurf
	// still posts findings, just without the one-click-apply snippet.
	if s := strings.Index(b, "## Suggested fixes"); s >= 0 {
		if e := strings.Index(b[s:], "## Posting back to the PR"); e >= 0 {
			b = b[:s] + b[s+e:]
		}
	}
	// Drop the opt-in "Posting back to the PR" section, leave a one-line pointer.
	const h = "## Posting back to the PR"
	if i := strings.Index(b, h); i >= 0 {
		b = b[:i] + h + "\n\nPrint the review. To post it, run `gh pr review <N> --comment` yourself.\n"
	}
	return b
}
