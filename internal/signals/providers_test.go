package signals

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPromptShapeVagueVsSpecific(t *testing.T) {
	vague, err := PromptShape{}.Assess(context.Background(), Scope{
		Prompt: "Not sure, maybe look into a redesign of the architecture across the codebase?",
	})
	if err != nil {
		t.Fatal(err)
	}
	specific, err := PromptShape{}.Assess(context.Background(), Scope{
		Prompt: "Rename the variable x to count in main.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if vague.Complexity <= specific.Complexity {
		t.Errorf("vague prompt complexity %v should exceed specific prompt complexity %v", vague.Complexity, specific.Complexity)
	}
}

// TestPromptShapeQuestionMarkAloneStaysHighConfidence is the regression
// test for a real bug: a plain, specific prompt that happens to end in
// "?" (extremely common -- "what is 2+2?", "does X handle Y?") must not
// lose confidence just for that. Confidence should only drop when an
// actual complexity/vague keyword fires; question-mark count and word
// count are objective facts, not fuzzy guesses.
func TestPromptShapeQuestionMarkAloneStaysHighConfidence(t *testing.T) {
	got, err := PromptShape{}.Assess(context.Background(), Scope{Prompt: "What is 2+2?"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Confidence != 0.85 {
		t.Errorf("confidence = %v, want 0.85 for a plain question with no complexity/vague keyword", got.Confidence)
	}
}

func TestPromptShapeKeywordMatchLowersConfidence(t *testing.T) {
	got, err := PromptShape{}.Assess(context.Background(), Scope{Prompt: "Please refactor this module"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Confidence != 0.35 {
		t.Errorf("confidence = %v, want 0.35 once a complexity keyword (refactor) fires", got.Confidence)
	}
}

func TestPromptShapeErrorsOnEmpty(t *testing.T) {
	if _, err := (PromptShape{}).Assess(context.Background(), Scope{}); err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestFileScopeScalesWithCount(t *testing.T) {
	one, _ := FileScope{}.Assess(context.Background(), Scope{Files: []string{"a.go"}})
	many, _ := FileScope{}.Assess(context.Background(), Scope{Files: make([]string, 20)})
	if one.Complexity >= many.Complexity {
		t.Errorf("single-file complexity %v should be less than many-file complexity %v", one.Complexity, many.Complexity)
	}
}

func TestFileScopeErrorsOnEmpty(t *testing.T) {
	if _, err := (FileScope{}).Assess(context.Background(), Scope{}); err == nil {
		t.Fatal("expected error for no files")
	}
}

func TestGitChurnErrorsOutsideRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	_, err := GitChurn{}.Assess(context.Background(), Scope{Repo: t.TempDir(), Files: []string{"f.go"}})
	if err == nil {
		t.Fatal("expected error for a non-git directory")
	}
}

func TestGitChurnErrorsWithoutFilesOrRepo(t *testing.T) {
	if _, err := (GitChurn{}).Assess(context.Background(), Scope{}); err == nil {
		t.Fatal("expected error for empty scope")
	}
}

// TestGitChurnErrorsOnUntrackedFile is the regression test for the other
// half of the subdirectory bug: `git log -- <path>` doesn't error on a
// pathspec that matches nothing, so a brand-new, never-committed file
// previously read as "0 commits, confidence 0.82" -- the calmest possible
// reading, with high confidence, for a file with NO history to read at all.
func TestGitChurnErrorsOnUntrackedFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	dir := initTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "never-committed.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := GitChurn{}.Assess(context.Background(), Scope{Repo: dir, Files: []string{"never-committed.go"}})
	if err == nil {
		t.Fatal("expected an error for a file that was never committed -- no history to read is UNKNOWN, not calm")
	}
}

// TestGitChurnFindsCommitsFromASubdirectoryScope is the regression test for
// the actual live bug: scopedFiles reports paths relative to the repo
// ROOT, but Scope.Repo was previously whatever subdirectory a session
// happened to be in. `git log -- <root-relative-path>` run with cmd.Dir
// set to a subdirectory matches nothing and exits 0 -- silently
// indistinguishable from "genuinely 0 commits". Repo must be the toplevel
// (which newScope in cmd/deadeye now resolves); this asserts the provider
// itself reports real churn once given a correctly-rooted scope.
func TestGitChurnFindsCommitsFromASubdirectoryScope(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	dir := initTestRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "pkg", "a.go")
	for i := 0; i < 5; i++ {
		content := []byte(fmt.Sprintf("package pkg\n// rev %d\n", i))
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "pkg/a.go")
		runGit(t, dir, "commit", "-q", "-m", fmt.Sprintf("rev %d", i))
	}

	evidence, err := GitChurn{}.Assess(context.Background(), Scope{Repo: dir, Files: []string{"pkg/a.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := evidence.Facts["commits_last_30d"].(int); got != 5 {
		t.Errorf("commits_last_30d = %v, want 5 -- Repo=%s (the toplevel) with a root-relative file path must find its real history", evidence.Facts["commits_last_30d"], dir)
	}
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestTestPresenceDetectsAdjacentTest(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "foo.go")
	test := filepath.Join(dir, "foo_test.go")
	if err := os.WriteFile(src, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(test, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	covered, err := TestPresence{}.Assess(context.Background(), Scope{Files: []string{src}})
	if err != nil {
		t.Fatal(err)
	}
	uncovered, err := TestPresence{}.Assess(context.Background(), Scope{Files: []string{filepath.Join(dir, "bar.go")}})
	if err != nil {
		t.Fatal(err)
	}
	if covered.Complexity >= uncovered.Complexity {
		t.Errorf("covered complexity %v should be less than uncovered %v", covered.Complexity, uncovered.Complexity)
	}
}

// TestTestPresenceResolvesRelativePathsAgainstRepoNotProcessCwd is the
// regression test for hasAdjacentTest previously stat'ing a relative path
// against whatever directory the calling PROCESS happened to be in --
// wrong for a daemon that may have been spawned from an entirely different
// project. This test's own process cwd is wherever `go test` runs from,
// deliberately unrelated to dir, to prove resolution goes through s.Repo.
func TestTestPresenceResolvesRelativePathsAgainstRepoNotProcessCwd(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "a.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "a_test.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := TestPresence{}.Assess(context.Background(), Scope{Repo: dir, Files: []string{"pkg/a.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := got.Facts["files_with_adjacent_test"].(int); n != 1 {
		t.Errorf("files_with_adjacent_test = %v, want 1 -- a relative path must resolve against Scope.Repo, not this test process's own cwd", got.Facts["files_with_adjacent_test"])
	}
}

// TestSkippedProvidersBecomeUnknownEvidence is the regression test for
// C1: an empty scope means every builtin provider errors (nothing to
// assess). Rather than degrading to zero evidence (which the kernel
// can't distinguish from "somehow zero providers were even asked"),
// AssessAll must emit exactly one explicit zero-confidence "unknown" item
// naming what was skipped -- see AssessAll's comment for why this matters:
// it's what makes a low-confidence single-signal read (e.g. a clean
// working tree) route to the ceiling instead of silently downshifting.
func TestSkippedProvidersBecomeUnknownEvidence(t *testing.T) {
	got := AssessAll(context.Background(), Scope{}, Builtins())
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 (unknown) evidence item from an empty scope, got %d: %+v", len(got), got)
	}
	if got[0].Provider != "unknown" || got[0].Confidence != 0 {
		t.Errorf("evidence = %+v, want Provider=unknown Confidence=0", got[0])
	}
	skipped, _ := got[0].Facts["skipped"].([]string)
	if len(skipped) != len(Builtins()) {
		t.Errorf("skipped = %v, want all %d builtins named", skipped, len(Builtins()))
	}
}
