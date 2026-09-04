// Package vapt holds the canonical whole-service pen-test/VAPT rubric --
// the single source of truth for the /deadeye-vapt capability. Split into
// two embedded files the same way internal/prreview splits its shared
// lenses/fixes fragments out of the wrapper templates: ruleset.md carries
// the method (scope, phases, report format, boundaries) and owasp.md
// carries the long, mechanical OWASP/API/LLM coverage matrices and the
// eighteen-tag reference table, substituted in at {{owasp}}. The Claude
// Code skill (skills/deadeye-vapt/SKILL.md) carries Body() verbatim (a
// canary test keeps them identical), and `deadeye init <host>` renders it
// into every other host's on-demand command file.
package vapt

import (
	_ "embed"
	"strings"
)

//go:embed ruleset.md
var rulesetTemplate string

//go:embed owasp.md
var owaspFragment string

var body string

func init() {
	body = expand(rulesetTemplate)
}

// expand substitutes the shared {{owasp}} fragment into the wrapper
// template. Plain string substitution, not a template engine -- the
// wrapper places the fragment as a whole section, never mid-sentence.
func expand(tmpl string) string {
	return strings.ReplaceAll(tmpl, "{{owasp}}", strings.TrimRight(owaspFragment, "\n"))
}

// Marker is a stable sentinel present in the rubric (and therefore in
// every rendering and the Claude skill). init greps for it to refuse
// clobbering a file it didn't write; uninstall greps for it to confirm a
// file is ours.
const Marker = "deadeye-vapt: canonical rubric"

// Body returns the canonical host-neutral VAPT rubric -- the exact body
// of the Claude skill, and the shared core every host rendering wraps.
func Body() string { return body }

// cutSection removes the text from the start of the "from" heading up to
// (not including) the start of the "to" heading. A no-op if either
// heading isn't present, so callers can chain cuts without checking
// existence. Mirrors internal/prreview's helper of the same name.
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

// WindsurfBody returns the rubric trimmed to fit Windsurf's hard
// 12000-char workflow cap. Every other host gets the full Body().
// Windsurf (experimental, no hook contract, no guaranteed `deadeye`
// binary) drops: the three OWASP/API/LLM id-mapping tables (the model
// already knows the Top 10 by name; the tables are a lookup convenience,
// not something the pass functionally needs), the overlap/citation-scope
// disambiguation notes plus "Beyond the Top 10" and the ASVS reference
// line (all reference prose -- useful, but the tag table alone still
// functions), and the Learning loop section (calls a deadeye CLI
// Windsurf has no guaranteed binary for). The eighteen-tag table, every
// method phase, and the report format all stay -- the pass still
// functions identically, just without the convenience lookups.
func WindsurfBody() string {
	b := body
	b = cutSection(b, "Eighteen tags. Every OWASP", "**Reference —")
	b = cutSection(b, "**OWASP Top 10:2025**", "## The eighteen tags")
	b = cutSection(b, "**Overlap rule**", "## Report format")
	b = cutSection(b, "## Learning loop", "## Boundaries")
	return b
}
