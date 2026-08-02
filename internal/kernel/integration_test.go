package kernel_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/kernel"
	"github.com/deepaksinghcs14/deadeye-cc/internal/signals"
)

// TestDownshiftIsReachableThroughRealProviders is the regression test for
// a real bug: promptshape's confidence is deliberately capped at 0.35 (the
// plan's "weakest signal"), and kernel.Decide originally required the
// MINIMUM confidence across ALL evidence to clear downshift_threshold
// (0.8 by default). Since any nonempty prompt makes promptshape
// participate, that one perpetually-low-confidence signal vetoed every
// downshift, for every real task, regardless of what the other three
// providers found -- caught by trying to reproduce escalation detection
// live and getting a ceiling decision from a scenario that should have
// downshifted (a single well-tested, freshly-committed file). Fixed by
// giving promptshape high confidence specifically for its "found nothing"
// result (a fairly reliable negative), keeping the low-confidence cap
// only for when it actually flags something.
func TestDownshiftIsReachableThroughRealProviders(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a_test.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "well-tested single file")

	scope := signals.Scope{
		Prompt: "Rename the variable x to count in a.go",
		Files:  []string{filepath.Join(dir, "a.go")},
		Repo:   dir,
	}
	evidence := signals.AssessAll(context.Background(), scope, signals.Builtins())
	if len(evidence) == 0 {
		t.Fatal("expected the builtin providers to produce evidence for a real git repo")
	}

	cat := catalog.Load()
	ceiling := kernel.Decide(nil, cat, 0.8)
	decision := kernel.Decide(evidence, cat, 0.8)

	if decision == ceiling {
		t.Fatalf("a specific, single-file, well-tested, freshly-committed task never downshifts from the ceiling -- "+
			"evidence: %+v, decision: %+v", evidence, decision)
	}
}
