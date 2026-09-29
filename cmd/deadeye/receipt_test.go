package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
	"github.com/deepaksinghcs14/deadeye-cc/internal/receipts"
)

// receiptTestRepo makes a real one-commit git repo and chdirs into it,
// with HOME pointed at a temp dir so the receipt lands in an isolated
// state dir. The same shape codemap_test.go and signals' providers_test.go
// already use for git-backed tests.
func receiptTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "first")

	t.Setenv("HOME", t.TempDir())
	t.Chdir(dir)
	return dir
}

func TestRunReceiptRecordsBaseCommitAndPaths(t *testing.T) {
	receiptTestRepo(t)
	// An uncommitted change, so the derived path list is non-empty.
	if err := os.WriteFile("a.go", []byte("package a\n\nfunc F() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() { runReceipt([]string{"review", "--scope", "diff"}) })
	if !strings.Contains(out, "receipt recorded") {
		t.Fatalf("expected a recorded receipt:\n%s", out)
	}

	got, err := receipts.Scan(meta.ReceiptsPath())
	if err != nil || len(got) != 1 {
		t.Fatalf("Scan = %d receipts, err %v; want 1", len(got), err)
	}
	r := got[0]
	if r.Kind != receipts.KindReview || r.Scope != "diff" {
		t.Errorf("wrong kind/scope: %+v", r)
	}
	if r.Commit == "" {
		t.Error("receipt carries no base commit -- nothing to scan forward from")
	}
	// The whole point of the base commit: it is HEAD as it was BEFORE the
	// reviewed work landed, so a later scan can find what came after.
	if len(r.Paths) != 1 || r.Paths[0] != "a.go" {
		t.Errorf("paths = %v, want the changed file derived from git", r.Paths)
	}
}

func TestRunReceiptAcceptsExplicitPathsBothFlagSpellings(t *testing.T) {
	for _, args := range [][]string{
		{"review", "--scope", "pr", "--paths", "x.go,y.go"},
		{"review", "--scope=pr", "--paths=x.go,y.go"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			receiptTestRepo(t)
			captureStdout(t, func() { runReceipt(args) })
			got, err := receipts.Scan(meta.ReceiptsPath())
			if err != nil || len(got) != 1 {
				t.Fatalf("Scan = %d receipts, err %v; want 1", len(got), err)
			}
			if len(got[0].Paths) != 2 || got[0].Paths[0] != "x.go" {
				t.Errorf("paths = %v, want [x.go y.go]", got[0].Paths)
			}
		})
	}
}

// Outside a repo there is no commit to anchor to. It must say so and exit
// cleanly -- a review has already run by this point, and its bookkeeping
// failing must never look like the review failed.
func TestRunReceiptOutsideRepoSkipsQuietly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	out := captureStdout(t, func() { runReceipt([]string{"review", "--scope", "diff"}) })
	if !strings.Contains(out, "receipt skipped") {
		t.Errorf("want a skip line, got:\n%s", out)
	}
	if got, _ := receipts.Scan(meta.ReceiptsPath()); len(got) != 0 {
		t.Errorf("wrote %d receipts outside a repo, want none", len(got))
	}
}

func TestWriteCoderReceiptRecordsLevel(t *testing.T) {
	dir := receiptTestRepo(t)
	writeCoderReceipt(dir, "sess-1", "sniper")

	got, err := receipts.Scan(meta.ReceiptsPath())
	if err != nil || len(got) != 1 {
		t.Fatalf("Scan = %d receipts, err %v; want 1", len(got), err)
	}
	if got[0].Kind != receipts.KindCoder || got[0].Level != "sniper" {
		t.Errorf("wrong coder receipt: %+v", got[0])
	}
	if got[0].SessionID != "sess-1" || got[0].Commit == "" {
		t.Errorf("coder receipt missing session or commit: %+v", got[0])
	}
}

// The hook path calls this on every session start; outside a repo it must
// be a silent no-op, never a write and never an error.
func TestWriteCoderReceiptOutsideRepoIsNoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeCoderReceipt(t.TempDir(), "sess", "marksman")
	if got, _ := receipts.Scan(meta.ReceiptsPath()); len(got) != 0 {
		t.Errorf("wrote %d receipts outside a repo, want none", len(got))
	}
	writeCoderReceipt("", "sess", "marksman") // no cwd at all
}

func TestSplitPathsTrimsAndDropsEmpties(t *testing.T) {
	got := splitPaths(" a.go , ,b.go,\n")
	if len(got) != 2 || got[0] != "a.go" || got[1] != "b.go" {
		t.Errorf("splitPaths = %v, want [a.go b.go]", got)
	}
}
