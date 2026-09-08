package vaptreport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var fixedTime = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func sample() Input {
	return Input{
		Repo: "sample-service",
		Findings: []Finding{
			{Severity: "nit", Tag: "config", Title: "missing header"},
			{Severity: "critical", Tag: "authz", Title: "IDOR", Attack: "<script>alert(1)</script>"},
			{Severity: "high", Tag: "inject", Title: "SQLi"},
		},
		Coverage: []CoverageRow{{Category: "authz", State: "findings", Detail: "1"}},
	}
}

// TestBuildSortsWorstFirst: findings must render critical/high/.../nit
// regardless of the order the JSON payload listed them in.
func TestBuildSortsWorstFirst(t *testing.T) {
	d := Build(sample(), fixedTime)
	want := []string{"critical", "high", "nit"}
	for i, f := range d.Findings {
		if f.Severity != want[i] {
			t.Fatalf("Findings[%d].Severity = %q, want %q (order: %v)", i, f.Severity, want[i], d.Findings)
		}
	}
	if d.Tally["critical"] != 1 || d.Tally["high"] != 1 || d.Tally["nit"] != 1 || d.Tally["medium"] != 0 {
		t.Errorf("Tally = %+v, want 1 critical/1 high/1 nit/0 medium", d.Tally)
	}
	if d.Clean {
		t.Error("Clean = true with findings present")
	}
}

// TestBuildCleanWithNoFindings: an empty findings list must report Clean,
// matching the rubric's "Clean line of fire" case.
func TestBuildCleanWithNoFindings(t *testing.T) {
	in := Input{Repo: "clean-service"}
	d := Build(in, fixedTime)
	if !d.Clean {
		t.Error("Clean = false with zero findings")
	}
}

// TestRenderEscapesUntrustedText: finding fields are repo-derived and
// untrusted (route paths, attacker-supplied param values in a proof) --
// html/template's auto-escaping must actually fire, not just be declared.
func TestRenderEscapesUntrustedText(t *testing.T) {
	d := Build(sample(), fixedTime)
	html, err := Render(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("Render did not escape a <script> tag in finding text -- auto-escaping isn't firing")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Error("expected the escaped form &lt;script&gt; in output")
	}
}

// TestGenerateWritesFile: Generate must parse JSON from the reader and
// write a real file at outPath, creating parent directories.
func TestGenerateWritesFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "vapt-reports", "vapt-test.html")
	r := strings.NewReader(`{"repo":"x","findings":[{"severity":"high","tag":"inject","title":"t"}],"coverage":[]}`)

	path, err := Generate(r, out)
	if err != nil {
		t.Fatal(err)
	}
	if path != out {
		t.Errorf("Generate returned %q, want %q", path, out)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("report file not written: %v", err)
	}
	if !strings.Contains(string(b), "sample") && !strings.Contains(string(b), "x") {
		t.Error("written report doesn't contain the repo name")
	}
}

// TestGenerateRejectsMissingRepo: a payload with no "repo" field is
// malformed input, not a silently-empty report.
func TestGenerateRejectsMissingRepo(t *testing.T) {
	_, err := Generate(strings.NewReader(`{"findings":[]}`), filepath.Join(t.TempDir(), "out.html"))
	if err == nil {
		t.Error("Generate accepted a payload with no repo field")
	}
}
