package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/receipts"
)

func coderReceipt(ts, commit, level string) receipts.Receipt {
	return receipts.Receipt{
		TS: ts, Repo: "deadeye-cc", Commit: commit,
		Kind: receipts.KindCoder, Level: level,
	}
}

// logLine builds a `git log --format=%H%x00%at%x00%s` row.
func logLine(sha string, at time.Time, subject string) string {
	return fmt.Sprintf("%s\x00%d\x00%s", sha, at.Unix(), subject)
}

func TestRenderAdherenceNoReceiptsExplainsNoBackfill(t *testing.T) {
	out := captureStdout(t, func() {
		renderAdherence(writeReceipts(t), "deadeye-cc", fakeGit{})
	})
	if !strings.Contains(out, "No coder-mode sessions recorded") {
		t.Errorf("missing empty state:\n%s", out)
	}
	if !strings.Contains(out, "nothing in git records which persona") {
		t.Errorf("must explain why this can't be backfilled:\n%s", out)
	}
}

func TestRenderAdherenceMeasuresRungsFiveAndSeven(t *testing.T) {
	start := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
	rp := writeReceipts(t, coderReceipt(start.Format(time.RFC3339), "base0000", "marksman"))
	inWindow := start.Add(time.Hour)

	g := fakeGit{out: map[string]string{
		"log --format=%H%x00%at%x00%s base0000..HEAD": strings.Join([]string{
			logLine("c1111111", inWindow, "add the feature"),
			logLine("c2222222", inWindow.Add(time.Minute), "wire it up"),
		}, "\n"),
		"show --numstat --format= c1111111":   "10\t2\tinternal/a.go\n4\t0\tpackage.json",
		"show --numstat --format= c2222222":   "3\t1\tinternal/b.go",
		"show --name-only --format= c1111111": "internal/a.go\npackage.json",
		"show --name-only --format= c2222222": "internal/b.go",
		"show --unified=0 --format= c1111111 -- package.json": `--- a/package.json
+++ b/package.json
@@ -5,0 +6,1 @@
+    "left-pad": "1.3.0",
`,
	}}

	out := captureStdout(t, func() { renderAdherence(rp, "deadeye-cc", g) })

	if !strings.Contains(out, "Rung 5") || !strings.Contains(out, "left-pad") {
		t.Errorf("rung 5 should name the dependency added under coder mode:\n%s", out)
	}
	if !strings.Contains(out, "Rung 7") || !strings.Contains(out, "median") {
		t.Errorf("rung 7 should report the distribution:\n%s", out)
	}
	if !strings.Contains(out, "marksman") {
		t.Errorf("commits should be grouped by coder level:\n%s", out)
	}
}

// Commits outside the session window aren't attributable to coder mode,
// and claiming them would inflate every figure in the report.
func TestRenderAdherenceExcludesCommitsOutsideWindow(t *testing.T) {
	start := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second)
	rp := writeReceipts(t, coderReceipt(start.Format(time.RFC3339), "base0000", "sniper"))
	g := fakeGit{out: map[string]string{
		// 24h after the receipt: past the 12h window.
		"log --format=%H%x00%at%x00%s base0000..HEAD": logLine("late1111", start.Add(24*time.Hour), "much later work"),
	}}
	out := captureStdout(t, func() { renderAdherence(rp, "deadeye-cc", g) })
	if !strings.Contains(out, "no commits landed inside any") {
		t.Errorf("a commit 24h after the session should not be attributed:\n%s", out)
	}
}

// Overlapping sessions must not count the same commit twice.
func TestRenderAdherenceDeduplicatesAcrossOverlappingSessions(t *testing.T) {
	start := time.Now().Add(-4 * time.Hour).UTC().Truncate(time.Second)
	rp := writeReceipts(t,
		coderReceipt(start.Format(time.RFC3339), "base0000", "marksman"),
		coderReceipt(start.Add(time.Minute).Format(time.RFC3339), "base0000", "marksman"),
	)
	g := fakeGit{out: map[string]string{
		"log --format=%H%x00%at%x00%s base0000..HEAD": logLine("c1111111", start.Add(time.Hour), "one commit"),
		"show --numstat --format= c1111111":           "5\t1\ta.go",
		"show --name-only --format= c1111111":         "a.go",
	}}
	out := captureStdout(t, func() { renderAdherence(rp, "deadeye-cc", g) })
	if !strings.Contains(out, "Commits") || !strings.Contains(out, " 1 in deadeye-cc") {
		t.Errorf("the shared commit should be counted once, across 2 sessions:\n%s", out)
	}
}

// The report must name what it does NOT measure -- rung 2 needs a symbol
// index that doesn't exist, and "lines not written" has no baseline at all.
func TestRenderAdherenceNamesWhatItCannotMeasure(t *testing.T) {
	start := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	rp := writeReceipts(t, coderReceipt(start.Format(time.RFC3339), "base0000", "marksman"))
	g := fakeGit{out: map[string]string{
		"log --format=%H%x00%at%x00%s base0000..HEAD": logLine("c1111111", start.Add(time.Hour), "work"),
		"show --numstat --format= c1111111":           "5\t1\ta.go",
		"show --name-only --format= c1111111":         "a.go",
	}}
	out := captureStdout(t, func() { renderAdherence(rp, "deadeye-cc", g) })
	for _, want := range []string{
		"Not measured, deliberately",
		"rung 2",
		"lines NOT written",
		"no baseline exists",
		"Attribution is by time window",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing boundary %q:\n%s", want, out)
		}
	}
}

func TestAddedLinesStripsMarkersAndHeaders(t *testing.T) {
	patch := `--- a/go.mod
+++ b/go.mod
@@ -3,0 +4,1 @@
+	github.com/x/y v1.2.3
-	github.com/gone/z v0.1.0
 	unchanged
`
	got := addedLines(patch)
	if !strings.Contains(got, "github.com/x/y v1.2.3") {
		t.Errorf("added line missing: %q", got)
	}
	if strings.Contains(got, "+++") || strings.Contains(got, "gone/z") || strings.Contains(got, "unchanged") {
		t.Errorf("only added content should survive: %q", got)
	}
}

func TestMedian(t *testing.T) {
	cases := []struct {
		in   []int
		want int
	}{
		{nil, 0},
		{[]int{5}, 5},
		{[]int{1, 9}, 9},
		{[]int{3, 1, 2}, 2},
		{[]int{10, 1, 5, 2}, 5},
	}
	for _, c := range cases {
		if got := median(c.in); got != c.want {
			t.Errorf("median(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}
