// Package vaptreport renders a /deadeye-vapt pass's findings into a
// self-contained, print-optimized HTML report -- the "PDF" deliverable
// (Cmd/Ctrl+P -> Save as PDF; no PDF library, keeping the project's
// zero-dependency policy intact per CONTRIBUTING.md). Mirrors
// internal/report's Build/Render/Generate split and its embed +
// html/template + atomic-write discipline, but the view model is
// findings-shaped, not decision-log-shaped, and the file lands in the
// SCANNED repo (vapt-reports/), not ~/.deadeye/ -- this is the target
// repo's own artifact, not deadeye's state.
package vaptreport

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed vaptreport.html.tmpl
var tmplSrc string

// Finding is one reported vulnerability, matching the rubric's per-finding
// block (internal/vapt/ruleset.md's "Report format" section) field for
// field.
type Finding struct {
	Severity string   `json:"severity"` // critical|high|medium|nit
	Tag      string   `json:"tag"`
	Title    string   `json:"title"`
	Endpoint string   `json:"endpoint"`
	OWASP    []string `json:"owasp"`
	Link     string   `json:"link"`
	Attack   string   `json:"attack"`
	Proof    string   `json:"proof"`
	Fix      string   `json:"fix"`
	FixCode  string   `json:"fix_code,omitempty"`
	FixLang  string   `json:"fix_lang,omitempty"`
}

// CoverageRow is one line of the rubric's mandatory closing coverage
// matrix -- every category ends up exactly one of these. State doubles as
// a CSS class in the template, so it's hyphenated (n-a, not-reached), not
// slash/underscore -- "/" isn't a valid class-name character.
type CoverageRow struct {
	Category string `json:"category"`
	State    string `json:"state"` // findings|clean|n-a|not-reached
	Detail   string `json:"detail"`
}

// Input is the JSON shape `deadeye vapt-report` reads (see
// internal/vapt/ruleset.md's "Report generation" section for the exact
// contract the skill writes to).
type Input struct {
	Repo             string        `json:"repo"`
	Scope            string        `json:"scope,omitempty"` // e.g. "billing service" when Phase 0's ambiguity gate narrowed it; empty means the whole repo
	SurfaceInventory string        `json:"surface_inventory"`
	TrustBoundaryMap string        `json:"trust_boundary_map"`
	Findings         []Finding     `json:"findings"`
	Coverage         []CoverageRow `json:"coverage"`
	OmittedCount     int           `json:"omitted_count,omitempty"`
}

// severityRank orders findings worst-first regardless of input order --
// the rubric already ranks, but a hand-assembled JSON payload shouldn't
// have to get that right for the report to read correctly.
var severityRank = map[string]int{"critical": 0, "high": 1, "medium": 2, "nit": 3}

// Data is the view model the template renders -- all computed in Go, none
// in template syntax, same discipline internal/report follows.
type Data struct {
	Repo             string
	Scope            string // "" means Build already resolved it to "whole repository"
	GeneratedAt      string
	SurfaceInventory string
	TrustBoundaryMap string
	Findings         []Finding
	Coverage         []CoverageRow
	Tally            map[string]int
	OmittedCount     int
	Clean            bool
}

// Build computes the view model from in -- pure, now injected so tests
// don't depend on wall-clock time.
func Build(in Input, now time.Time) Data {
	findings := append([]Finding(nil), in.Findings...)
	sortFindings(findings)

	tally := map[string]int{"critical": 0, "high": 0, "medium": 0, "nit": 0}
	for _, f := range findings {
		tally[f.Severity]++
	}

	scope := in.Scope
	if scope == "" {
		scope = "whole repository"
	}

	return Data{
		Repo:             in.Repo,
		Scope:            scope,
		GeneratedAt:      now.UTC().Format("2006-01-02 15:04 UTC"),
		SurfaceInventory: in.SurfaceInventory,
		TrustBoundaryMap: in.TrustBoundaryMap,
		Findings:         findings,
		Coverage:         in.Coverage,
		Tally:            tally,
		OmittedCount:     in.OmittedCount,
		Clean:            len(findings) == 0,
	}
}

// sortFindings orders worst-first (severityRank), stable on ties so the
// input's own ordering (already worst-first, per the rubric) survives
// within a severity band.
func sortFindings(findings []Finding) {
	// insertion sort: findings lists are small (rubric caps at ~25), and
	// stability matters more than asymptotic cost here.
	for i := 1; i < len(findings); i++ {
		j := i
		for j > 0 && severityRank[findings[j].Severity] < severityRank[findings[j-1].Severity] {
			findings[j], findings[j-1] = findings[j-1], findings[j]
			j--
		}
	}
}

// Render executes vaptreport.html.tmpl against d. html/template auto-escapes
// every field -- finding text (routes, param names, proof snippets) is
// repo-derived and untrusted, the same reasoning internal/report/report.go
// documents for its own Reason strings.
func Render(d Data) (string, error) {
	t, err := template.New("vaptreport").Parse(tmplSrc)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, d); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Generate reads JSON from r, builds and renders the report, and writes it
// to outPath (creating parent directories), returning the written path.
// outPath is a file in the SCANNED repo, not deadeye's own state dir --
// 0o644, matching an ordinary repo file, not the 0o600 deadeye uses under
// ~/.deadeye/.
func Generate(r io.Reader, outPath string) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	var in Input
	if err := json.Unmarshal(b, &in); err != nil {
		return "", fmt.Errorf("parsing findings JSON: %w", err)
	}
	if in.Repo == "" {
		return "", fmt.Errorf("findings JSON has no \"repo\"")
	}

	data := Build(in, time.Now())
	html, err := Render(data)
	if err != nil {
		return "", fmt.Errorf("rendering report: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(outPath), err)
	}
	if err := os.WriteFile(outPath, []byte(html), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", outPath, err)
	}
	return outPath, nil
}

// DefaultOutPath is vapt-reports/vapt-<UTC timestamp>.html under dir
// (normally the scanned repo's cwd) -- timestamped so repeat scans don't
// clobber each other.
func DefaultOutPath(dir string) string {
	ts := time.Now().UTC().Format("20060102-150405")
	return filepath.Join(dir, "vapt-reports", "vapt-"+ts+".html")
}
