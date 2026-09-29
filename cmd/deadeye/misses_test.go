package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/receipts"
)

// fakeGit answers canned output per joined arg vector. Anything it wasn't
// primed for returns "" -- exactly how gitutil.Output fails open, so an
// unprimed call can't silently look like real data.
type fakeGit struct{ out map[string]string }

func (f fakeGit) run(args ...string) string { return f.out[strings.Join(args, " ")] }

func writeReceipts(t *testing.T, rs ...receipts.Receipt) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "receipts.jsonl")
	s := receipts.Open(path)
	for _, r := range rs {
		if err := s.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func writeOutcomes(t *testing.T, outs ...lessons.Outcome) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "outcomes.jsonl")
	s := lessons.Open(path)
	for _, o := range outs {
		if err := s.Append(o); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestRenderMissesNoReceiptsExplainsGoingForward(t *testing.T) {
	out := captureStdout(t, func() {
		renderMisses(writeReceipts(t), writeOutcomes(t), "deadeye-cc", fakeGit{}, regexFixShaped)
	})
	if !strings.Contains(out, "No review receipts") {
		t.Errorf("missing the empty-state line:\n%s", out)
	}
	// The honesty point: this can't be backfilled, and the report has to
	// say so rather than looking like a clean bill of health.
	if !strings.Contains(out, "never backfilled") {
		t.Errorf("empty state must state that accuracy is not backfilled:\n%s", out)
	}
}

func TestRenderMissesFindsCandidateOnLineOverlap(t *testing.T) {
	rp := writeReceipts(t, receipts.Receipt{
		TS: "2026-09-01T00:00:00Z", Repo: "deadeye-cc", Commit: "base0000",
		Kind: receipts.KindReview, Scope: "diff", Paths: []string{"internal/a.go"},
	})
	g := fakeGit{out: map[string]string{
		"log --format=%H%x00%s --reverse base0000..HEAD -- internal/a.go": "land1111\x00add the thing",
		"show --unified=0 --format= land1111": `--- a/internal/a.go
+++ b/internal/a.go
@@ -10,0 +11,2 @@
+	one()
+	two()
`,
		"log -s --format=%H%x00%s -L11,12:internal/a.go land1111..HEAD": "fix22222\x00fix the thing that broke",
	}}
	out := captureStdout(t, func() { renderMisses(rp, writeOutcomes(t), "deadeye-cc", g, regexFixShaped) })

	if !strings.Contains(out, "Candidate misses") || !strings.Contains(out, "fix22222") {
		t.Errorf("expected the fix commit as a candidate:\n%s", out)
	}
	// Severity language is load-bearing: never "confirmed".
	if !strings.Contains(out, "likely") {
		t.Errorf("candidate must be labeled likely:\n%s", out)
	}
	if strings.Contains(out, "(confirmed)") {
		t.Errorf("a heuristic candidate must never print as confirmed:\n%s", out)
	}
	// The proof clause names the exact lines, the way every finding in this
	// product carries its evidence.
	if !strings.Contains(out, "internal/a.go:11-12") {
		t.Errorf("proof must name the overlapping line range:\n%s", out)
	}
	// Nothing is recorded automatically -- it points at the manual command.
	if !strings.Contains(out, "lessons record external-miss") {
		t.Errorf("report must leave recording to the human:\n%s", out)
	}
}

// A commit touching the same lines but not claiming to fix anything is not
// a candidate: the fix-shaped subject is the first filter.
func TestRenderMissesIgnoresNonFixCommits(t *testing.T) {
	rp := writeReceipts(t, receipts.Receipt{
		TS: "2026-09-01T00:00:00Z", Repo: "deadeye-cc", Commit: "base0000",
		Kind: receipts.KindReview, Scope: "diff", Paths: []string{"a.go"},
	})
	g := fakeGit{out: map[string]string{
		"log --format=%H%x00%s --reverse base0000..HEAD -- a.go": "land1111\x00add the thing",
		"show --unified=0 --format= land1111": `--- a/a.go
+++ b/a.go
@@ -1,0 +2,1 @@
+	one()
`,
		"log -s --format=%H%x00%s -L2,2:a.go land1111..HEAD": "ref33333\x00rename the helper for clarity",
	}}
	out := captureStdout(t, func() { renderMisses(rp, writeOutcomes(t), "deadeye-cc", g, regexFixShaped) })
	if !strings.Contains(out, "No candidate misses") {
		t.Errorf("a non-fix commit must not be a candidate:\n%s", out)
	}
}

// Reviewed work that never got committed is excluded from the denominator
// rather than counted as a clean pass -- the report would otherwise
// flatter itself with reviews nothing could contradict yet.
func TestRenderMissesExcludesUnlandedWork(t *testing.T) {
	rp := writeReceipts(t, receipts.Receipt{
		TS: "2026-09-01T00:00:00Z", Repo: "deadeye-cc", Commit: "base0000",
		Kind: receipts.KindReview, Scope: "diff", Paths: []string{"a.go"},
	})
	out := captureStdout(t, func() { renderMisses(rp, writeOutcomes(t), "deadeye-cc", fakeGit{}, regexFixShaped) })
	if !strings.Contains(out, "Pending") || !strings.Contains(out, "not counted as passes") {
		t.Errorf("unlanded review should be reported as pending and excluded:\n%s", out)
	}
}

// Both error directions appear together: candidate false negatives from
// git, confirmed false positives from what the user disputed.
func TestRenderMissesShowsDisputesAlongside(t *testing.T) {
	rp := writeReceipts(t, receipts.Receipt{
		TS: "2026-09-01T00:00:00Z", Repo: "deadeye-cc", Commit: "b", Kind: receipts.KindReview, Paths: []string{"a.go"},
	})
	op := writeOutcomes(t,
		lessons.Outcome{TS: "2026-09-02T00:00:00Z", Repo: "deadeye-cc", Kind: "review-false-positive", TaskShape: "correctness:race"},
		lessons.Outcome{TS: "2026-09-02T00:00:00Z", Repo: "other-repo", Kind: "review-false-positive", TaskShape: "security:inject"},
	)
	out := captureStdout(t, func() { renderMisses(rp, op, "deadeye-cc", fakeGit{}, regexFixShaped) })
	if !strings.Contains(out, "False positives") || !strings.Contains(out, "1 finding") {
		t.Errorf("want this repo's 1 dispute counted, not the other repo's:\n%s", out)
	}
}

func TestFixShapedMatching(t *testing.T) {
	yes := []string{"fix: nil deref", "Fix the parser", "hotfix for prod", "revert the bad commit", "this fixes #12", "bugfix", "fixed a bug"}
	no := []string{"add prefix handling", "refactor the suffix parser", "add a feature", "docs: explain affix rules"}
	for _, s := range yes {
		if !fixShapedRe.MatchString(s) {
			t.Errorf("fixShaped(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if fixShapedRe.MatchString(s) {
			t.Errorf("fixShaped(%q) = true, want false", s)
		}
	}
}
