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

// TestPhase0FourIndependentTracks pins the real fix behind widening
// Phase 0: a target with zero HTTP routes but a real LLM/agent
// context-injection, message/event-driven, or client-side/UI surface
// must not be waved off as "not a service" -- that was the actual gap
// this session's own dogfood run against deadeye-cc found (a
// doc-comment-derived string injected into every session's context with
// no untrusted-content framing), then generalized to the other surface
// shapes a network-route-only gate would also miss: queue/topic
// consumers, and UI-only repos with no backend at all.
func TestPhase0FourIndependentTracks(t *testing.T) {
	for _, must := range []string{
		"Four independent",
		"LLM/agent context-injection",
		"A CLI with no HTTP surface can still be",
		"Message/event-driven",
		"Client-side/UI",
		"None found → say so and stop",
	} {
		if !strings.Contains(Body(), must) {
			t.Errorf("rubric missing %q -- Phase 0's four independent surface tracks regressed", must)
		}
	}
}

// TestNoFramingIsItselfAFinding pins the other half of the same fix: an
// LLM-surface finding doesn't need a demonstrated exploit payload to be
// reportable -- an extraction/injection pipeline with no stated
// data/instruction boundary is the finding by itself, the same way a
// missing authz check is reportable without a working exploit chain.
// Dropping this would silently raise the bar back to "prove it's been
// exploited," which no static, source-only pass can ever do.
func TestNoFramingIsItselfAFinding(t *testing.T) {
	const must = "No framing\nIS the finding"
	if !strings.Contains(Body(), must) {
		t.Error("rubric no longer states that a missing trust-boundary label is reportable on its own, without a demonstrated exploit")
	}
}

// TestAuthnAuthzOverlapDisambiguated pins the third overlap pair the
// message/event-driven acceptance test found live: "sender not
// authenticated before a privilege decision" is genuinely ambiguous
// between authn: and authz: without a stated rule, the same shape as the
// inject:/integrity: and expose:/exceptions: pairs already fixed. A
// future edit that drops this rule reopens exactly the ambiguity the
// acceptance test surfaced.
func TestAuthnAuthzOverlapDisambiguated(t *testing.T) {
	for _, must := range []string{
		"three pairs above share a mechanism",
		"the exploitable defect is the untrusted decision",
	} {
		if !strings.Contains(Body(), must) {
			t.Errorf("rubric missing %q -- the authn:/authz: overlap rule regressed", must)
		}
	}
}

// TestCitationScopeIsAccurate pins the citation-scope fix: the old text
// claimed only four tags were API-only, which was wrong on inspection --
// six tags are API-only, six are Top-10:2025-only, and validation: has
// no dedicated row in ANY table. A live acceptance test hit this
// inaccuracy directly (several tags it needed to cite had no row in the
// table the old text implied they should). This test checks the
// corrected claim is actually present, not just that citation-scope
// text exists at all.
func TestCitationScopeIsAccurate(t *testing.T) {
	for _, must := range []string{
		"don't assume every tag dual-cites",
		"have a Top 10:2025 row only",
		"have an API\nSecurity row only",
		"has no dedicated row anywhere in any of the three tables",
	} {
		if !strings.Contains(Body(), must) {
			t.Errorf("rubric missing %q -- the citation-scope accuracy fix regressed", must)
		}
	}
}

// TestWindsurfKeepsAllFourTracks guards every Windsurf cut this rubric
// makes against ever creeping into Phase 0/1/2 -- that's control flow
// the pass depends on, never a trim candidate the way an illustrative
// example or a reference table is. All four surface-detection tracks
// must survive on the one host with a hard character budget, not just
// the flagship hosts.
func TestWindsurfKeepsAllFourTracks(t *testing.T) {
	for _, must := range []string{
		"Network-facing", "LLM/agent context-injection", "Message/event-driven",
		"Client-side/UI", "Phase 0", "Phase 1", "Phase 2",
	} {
		if !strings.Contains(WindsurfBody(), must) {
			t.Errorf("WindsurfBody() lost %q -- Phase 0/1/2 must never be cut for Windsurf, only reference/illustrative material", must)
		}
	}
}

// TestBoundariesMatchesFourTracks catches the exact drift this session
// introduced and caught by hand: Phase 0 widened from "route/handler
// surface" to four independent tracks, but the closing "## Boundaries"
// section still said "No route/handler surface found... Do not review a
// CLI, library, or static site" -- directly contradicting the new
// LLM-only and client-side/UI tracks a few paragraphs earlier. A rubric
// that contradicts itself is worse than one that's merely incomplete.
func TestBoundariesMatchesFourTracks(t *testing.T) {
	if strings.Contains(Body(), "No route/handler surface found") {
		t.Error(`Boundaries still says "No route/handler surface found" -- stale, contradicts the four-track Phase 0`)
	}
	if !strings.Contains(Body(), "None of Phase 0's four tracks found") {
		t.Error("Boundaries' closing bullet no longer matches Phase 0's four tracks")
	}
}

// TestMessageDrivenTrackHasRealSubstance guards the message/event-driven
// track against becoming a label with no content: it must actually name
// what to inventory (queue, consumer group, trigger, payload validation)
// and the concrete finding a poison-pill/unbounded-retry message
// produces, not just a Phase 0 detection bullet with nothing in Phase 1
// to back it.
func TestMessageDrivenTrackHasRealSubstance(t *testing.T) {
	for _, must := range []string{
		"the consumer", "schema-validated", "claimed sender authenticated",
		"poison-pill", "ratelimit:", "dos:",
	} {
		if !strings.Contains(Body(), must) {
			t.Errorf("rubric missing %q -- the message/event-driven track's Phase 1 substance regressed", must)
		}
	}
}

// TestClientSideTrackHasRealSubstance mirrors the message-driven test for
// the client-side/UI track: postMessage origin checks, token storage
// location, and third-party embeds are the concrete things that make
// this track more than a label.
func TestClientSideTrackHasRealSubstance(t *testing.T) {
	for _, must := range []string{
		"postMessage", "event.origin", "localStorage", "httpOnly cookie",
		"third-party script", "CSP",
	} {
		if !strings.Contains(Body(), must) {
			t.Errorf("rubric missing %q -- the client-side/UI track's Phase 1 substance regressed", must)
		}
	}
}

// TestWindsurfDropsWorkedExample pins the newest Windsurf trim: the
// fenced finding example plus its owasp:/link: walkthrough is
// illustrative, cut for budget, while the mandatory-coverage-matrix
// instruction right after it -- the part that actually governs behavior
// -- must survive.
func TestWindsurfDropsWorkedExample(t *testing.T) {
	const example = "IDOR on GET /api/orders/{id}"
	if !strings.Contains(Body(), example) {
		t.Fatalf("test fixture stale: %q no longer in Body() -- update this test alongside the Report format example", example)
	}
	if strings.Contains(WindsurfBody(), example) {
		t.Error("WindsurfBody() carries the worked finding example -- there's no char budget left to keep it")
	}
	if !strings.Contains(WindsurfBody(), "mandatory coverage matrix") {
		t.Error("WindsurfBody() lost the mandatory-coverage-matrix instruction -- that governs behavior, it must survive the example cut")
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
