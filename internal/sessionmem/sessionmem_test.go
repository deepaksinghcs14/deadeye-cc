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
	return initGitRepoNamed(t, t.TempDir())
}

// initGitRepoNamed inits a git repo at an exact path -- for tests where
// ProjectKey's basename (derived from the repo root) has to be a specific
// value, not whatever name t.TempDir() picks.
func initGitRepoNamed(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
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

// TestProjectKeyPrefixCollisionDoesNotCrossMatch is the regression test for
// a bug where a project key that is itself a valid prefix of another
// project's key (e.g. "app" vs "app_api", both legal since gitutil.sanitize
// allows "_") would have its summaries pruned and overridden by the other
// project's, because the old "_" filename separator couldn't distinguish
// "app_<nano>.md" from "app_api_<nano>.md".
func TestProjectKeyPrefixCollisionDoesNotCrossMatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	repoApp := initGitRepoNamed(t, filepath.Join(root, "app"))
	repoAppAPI := initGitRepoNamed(t, filepath.Join(root, "app_api"))

	if err := Write(repoApp, "sess1", 1); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	// Outnumber app's summary so a cross-matching prefix would prune it:
	// pruneOldSummaries keeps only the newest keepSummaries files it thinks
	// belong to "app".
	for i := 0; i < keepSummaries+1; i++ {
		if err := Write(repoAppAPI, "sess2", i+1); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}

	entries, err := os.ReadDir(Dir())
	if err != nil {
		t.Fatal(err)
	}
	appPrefix, apiPrefix := summaryPrefix("app"), summaryPrefix("app_api")
	found := false
	for _, e := range entries {
		if e.Name() == "" {
			continue
		}
		isApp := strings.HasPrefix(e.Name(), appPrefix) && !strings.HasPrefix(e.Name(), apiPrefix)
		if isApp {
			found = true
			p := filepath.Join(Dir(), e.Name())
			old := time.Now().Add(-time.Minute)
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !found {
		t.Fatal("app's summary was pruned by app_api's writes (cross-match)")
	}

	got := LoadRecent(repoApp)
	if !strings.Contains(got, "branch:") || strings.Contains(got, "app_api") {
		t.Errorf("LoadRecent(app) returned the wrong project's summary: %q", got)
	}
}

func TestLoadRecentEmptyWhenNone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := LoadRecent(t.TempDir()); got != "" {
		t.Errorf("expected empty result with no summaries at all, got %q", got)
	}
}
