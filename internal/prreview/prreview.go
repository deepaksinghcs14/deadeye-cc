// Package prreview holds the canonical PR-review rubric -- the single
// source of truth for the /deadeye-pr capability. The Claude Code skill
// (skills/deadeye-pr/SKILL.md) carries this body verbatim (a canary test
// keeps them identical), and `deadeye init <host>` renders it into each
// other host's on-demand command file. One rubric, many surfaces.
package prreview

import _ "embed"

//go:embed ruleset.md
var body string

// Marker is a stable sentinel present in the rubric (and therefore in every
// rendering and the Claude skill). init greps for it to refuse clobbering a
// file it didn't write; uninstall greps for it to confirm a file is ours.
const Marker = "deadeye-pr: canonical rubric"

// Body returns the canonical host-neutral PR-review rubric -- the exact body
// of the Claude skill, and the shared core every host rendering wraps.
func Body() string { return body }
