package signals

import (
	"context"
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

func TestAssessAllDropsErroringProviders(t *testing.T) {
	// Empty scope: every builtin provider errors (nothing to assess).
	// AssessAll must degrade to zero evidence, not panic or synthesize
	// zero-complexity evidence in providers' place.
	got := AssessAll(context.Background(), Scope{}, Builtins())
	if len(got) != 0 {
		t.Errorf("expected 0 evidence from an empty scope, got %d: %+v", len(got), got)
	}
}
