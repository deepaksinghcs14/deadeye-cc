package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
	"github.com/deepaksinghcs14/deadeye-cc/internal/signals"
)

// TestRunRouteUsesAdjustedThreshold is the regression test for the dry
// run silently disagreeing with the real decision: runRoute read
// cfg.DownshiftThreshold raw while decideAgentRouting gated on
// lessons.AdjustedDownshiftThreshold, so for any task shape carrying a
// recorded escalation, `/deadeye-route` explained the decision with a
// threshold no real Agent call would ever use -- while its own comment
// claimed the two could never diverge.
func TestRunRouteUsesAdjustedThreshold(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	t.Chdir(repo)

	// A base with headroom, so the escalation has somewhere to move the
	// bar (at a base sitting on signals.MaxAchievableConfidence there is
	// nothing to scale into -- see internal/lessons).
	if err := configSet("downshift_threshold", "0.5"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()

	// The shape runRoute will compute for this scope, derived the same way
	// it does -- the assertion is that runRoute APPLIES the adjustment,
	// not that we can predict the shape string independently.
	scope := newScope("", repo)
	evidence := signals.AssessAll(context.Background(), scope, signals.Builtins())
	shape := taskShapeKey(scope.Files, scope.Prompt, evidence)

	store := lessons.Open(meta.OutcomesPath())
	if err := store.Append(lessons.Outcome{
		TS: nowRFC3339(), TaskShape: shape, Kind: "escalation", Weight: lessons.WeightEscalation,
	}); err != nil {
		t.Fatal(err)
	}
	outcomes, err := lessons.Scan(meta.OutcomesPath())
	if err != nil {
		t.Fatal(err)
	}
	want := lessons.AdjustedDownshiftThreshold(cfg.DownshiftThreshold, outcomes, shape, time.Now())
	if want <= cfg.DownshiftThreshold {
		t.Fatalf("fixture is inert: adjusted %.4f is not above base %.4f", want, cfg.DownshiftThreshold)
	}

	out := captureStdout(t, func() { runRoute("", "") })

	if !strings.Contains(out, fmt.Sprintf("threshold:  %.2f", want)) {
		t.Errorf("route did not report the adjusted threshold %.2f (base %.2f); output:\n%s",
			want, cfg.DownshiftThreshold, out)
	}
	if !strings.Contains(out, "raised by recorded escalations") {
		t.Errorf("route did not say WHY the threshold moved; output:\n%s", out)
	}
}

// TestNewScopeFallsBackToPromptNamedFiles: on a clean tree the git diff is
// empty, which silenced filescope/gitchurn/testpresence and forced every
// decision to the ceiling -- so a fully-specified one-file task routed
// expensively purely because its code was committed. Scope now falls back
// to files the prompt actually names.
func TestNewScopeFallsBackToPromptNamedFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.com")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "committed, so the diff is empty")

	// Names a real, committed file: scope picks it up even with a clean tree.
	got := newScope("Rename x to count in a.go", dir)
	if len(got.Files) != 1 || got.Files[0] != "a.go" {
		t.Errorf("Files = %v, want [a.go] derived from the prompt on a clean tree", got.Files)
	}

	// Names nothing real: still empty, so the unknown-evidence ceiling
	// guard (TestCleanTreeDoesNotDownshiftABigTask) is untouched.
	if got := newScope("redesign the whole thing", dir); len(got.Files) != 0 {
		t.Errorf("Files = %v, want empty when the prompt names no real file", got.Files)
	}
	if got := newScope("edit nonexistent-file.go", dir); len(got.Files) != 0 {
		t.Errorf("Files = %v, want empty when the named file does not exist", got.Files)
	}
}
