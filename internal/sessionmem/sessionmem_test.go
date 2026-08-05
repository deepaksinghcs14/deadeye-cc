package sessionmem

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-q", "-m", "initial")
	return dir
}

func TestWriteSkipsWhenNoActivity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir() // not a git repo
	if err := Write(dir, "sess1", 0); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(Dir())
	if len(entries) != 0 {
		t.Errorf("expected no summary file for a non-git dir with no decisions, got %d", len(entries))
	}
}

func TestWriteAndLoadRecent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initGitRepo(t)

	if err := Write(repo, "sess1", 3); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(Dir())
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one summary file, got %v (err=%v)", entries, err)
	}

	// LoadRecent's freshness guard skips anything written in the last 30s
	// (same-session artifacts) -- back-date the file to get past it rather
	// than sleeping 30s in a test.
	path := filepath.Join(Dir(), entries[0].Name())
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	got := LoadRecent(repo)
	if !strings.Contains(got, "branch:") {
		t.Errorf("LoadRecent missing branch line: %q", got)
	}
	if !strings.Contains(got, "deadeye logged 3 decisions") {
		t.Errorf("LoadRecent missing decision count: %q", got)
	}
}

// TestWriteKeepsOnlyRecentSummaries is the regression test for E4: Write
// created one new file per session forever, and LoadRecent stats every
// matching file on every session start -- so an unbounded count meant
// startup cost grew with how many sessions a project has EVER had.
func TestWriteKeepsOnlyRecentSummaries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initGitRepo(t)

	for i := 0; i < 6; i++ {
		if err := Write(repo, "sess", i+1); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond) // guarantee distinct nanosecond filenames
	}

	entries, err := os.ReadDir(Dir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != keepSummaries {
		t.Errorf("got %d summary files after 6 writes, want %d (pruned)", len(entries), keepSummaries)
	}
}

func TestLoadRecentRespectsFreshnessGuard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initGitRepo(t)
	if err := Write(repo, "sess1", 1); err != nil {
		t.Fatal(err)
	}
	// File was just written -- still within the 30s freshness guard.
	if got := LoadRecent(repo); got != "" {
		t.Errorf("expected freshness guard to suppress a just-written summary, got %q", got)
	}
}

func TestLoadRecentEmptyWhenNone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := LoadRecent(t.TempDir()); got != "" {
		t.Errorf("expected empty result with no summaries at all, got %q", got)
	}
}
