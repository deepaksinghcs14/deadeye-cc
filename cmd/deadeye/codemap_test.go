package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/codemap"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/inject"
)

// codemapTestRepo isolates HOME and creates a small committed git repo.
func codemapTestRepo(t *testing.T, files ...string) (*daemonState, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	t.Setenv("HOME", t.TempDir())
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
	run("init", "-q")
	for _, f := range files {
		path := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", ".")
	run("commit", "-q", "-m", "initial")
	return newDaemonState(catalog.Catalog{}, nil), dir
}

func readFixture(t *testing.T, sessionID, path string) hookio.Input {
	t.Helper()
	b, err := json.Marshal(map[string]string{"file_path": path})
	if err != nil {
		t.Fatal(err)
	}
	return hookio.Input{SessionID: sessionID, ToolName: "Read", ToolInput: b}
}

// TestSessionEndPersistsTouchedFilesBeforeEviction: the regression test
// for anyone later reordering the codemap calls relative to
// state.endSession -- the snapshot needs the session's map alive.
func TestSessionEndPersistsTouchedFilesBeforeEviction(t *testing.T) {
	state, repo := codemapTestRepo(t, "a.go", "b.go")
	cfg := config.Default()

	for _, f := range []string{"a.go", "b.go"} {
		decideReadAdvice(readFixture(t, "cm1", filepath.Join(repo, f)), cfg, state)
	}
	decideSessionEnd(hookio.Input{SessionID: "cm1", Cwd: repo}, cfg, state)

	tf := codemap.LoadTouchFrequency(repo)
	if tf.Counts["a.go"] != 1 || tf.Counts["b.go"] != 1 {
		t.Errorf("touched files not persisted at SessionEnd: %v", tf.Counts)
	}
	if _, err := os.Stat(codemap.MapPath(repo)); err != nil {
		t.Errorf("map.md not generated at SessionEnd: %v", err)
	}
}

// TestSessionEndRespectsCodemapOff: "off" must mean NO writes at all, not
// just no injection -- the regression test for the missing-cfg-parameter
// bug the plan review caught.
func TestSessionEndRespectsCodemapOff(t *testing.T) {
	state, repo := codemapTestRepo(t, "a.go")
	cfg := config.Default()
	cfg.Mode.Codemap = "off"

	decideReadAdvice(readFixture(t, "cm2", filepath.Join(repo, "a.go")), cfg, state)
	decideSessionEnd(hookio.Input{SessionID: "cm2", Cwd: repo}, cfg, state)

	if _, err := os.Stat(codemap.MapPath(repo)); !os.IsNotExist(err) {
		t.Error("map.md written with mode.codemap off")
	}
	if _, err := os.Stat(codemap.TouchPath(repo)); !os.IsNotExist(err) {
		t.Error("touched.json written with mode.codemap off")
	}
}

// TestReadAdviceTracksEvenWhenPreprocessOff: the split-gate regression
// test -- tracking runs for codemap even with preprocess off, while the
// visible advisory stays suppressed.
func TestReadAdviceTracksEvenWhenPreprocessOff(t *testing.T) {
	state, repo := codemapTestRepo(t, "a.go")
	cfg := config.Default()
	cfg.Mode.Preprocess = "off"

	in := readFixture(t, "cm3", filepath.Join(repo, "a.go"))
	if out := decideReadAdvice(in, cfg, state); out.HookSpecificOutput != nil {
		t.Errorf("visible advisory should stay gated under preprocess, got %+v", out.HookSpecificOutput)
	}
	// Second identical read: still no VISIBLE advisory (preprocess off)...
	if out := decideReadAdvice(in, cfg, state); out.HookSpecificOutput != nil {
		t.Errorf("advisory leaked with preprocess off: %+v", out.HookSpecificOutput)
	}
	// ...but the tracking must have happened: SessionEnd persists the file.
	decideSessionEnd(hookio.Input{SessionID: "cm3", Cwd: repo}, cfg, state)
	if tf := codemap.LoadTouchFrequency(repo); tf.Counts["a.go"] == 0 {
		t.Error("markFileRead tracking starved by preprocess=off despite codemap=on")
	}
}

// TestUserPromptSubmitComposesCodemapAfterGuidance: the guidance half must
// stay byte-identical to inject.Build's own output -- proving codemap
// composed in decide.go and never leaked into Build.
func TestUserPromptSubmitComposesCodemapAfterGuidance(t *testing.T) {
	state, repo := codemapTestRepo(t, "pkg/a.go", "top.go")
	cfg := config.Default()

	// Seed the map the way production does: a prior session's SessionEnd.
	decideSessionEnd(hookio.Input{SessionID: "seed", Cwd: repo}, cfg, state)

	out := decideUserPromptSubmit(hookio.Input{SessionID: "cm4", Prompt: "hello", Cwd: repo}, cfg, "", "", state)
	if out.HookSpecificOutput == nil {
		t.Fatal("expected an injection on the first prompt")
	}
	got := out.HookSpecificOutput.AdditionalContext
	if !strings.Contains(got, "Codebase map:") {
		t.Errorf("codemap text missing from the composed injection:\n%s", got)
	}
	guidance := inject.Build(state.cat, "", cfg.Mode.Effort != "off", "")
	if !strings.Contains(got, guidance) {
		t.Error("guidance block is not byte-identical to inject.Build's output -- codemap leaked into Build?")
	}
}

// TestCodemapSuppressedOnNativeRestore mirrors sessionmem's own guard: a
// resumed/compacted session already replayed its exploration.
func TestCodemapSuppressedOnNativeRestore(t *testing.T) {
	state, repo := codemapTestRepo(t, "a.go")
	cfg := config.Default()
	decideSessionEnd(hookio.Input{SessionID: "seed", Cwd: repo}, cfg, state)

	state.markNativeRestore("cm5")
	out := decideUserPromptSubmit(hookio.Input{SessionID: "cm5", Prompt: "hello", Cwd: repo}, cfg, "", "", state)
	if out.HookSpecificOutput != nil && strings.Contains(out.HookSpecificOutput.AdditionalContext, "Codebase map:") {
		t.Error("codemap injected on a native-restore session")
	}
}

// TestCodemapInjectionSuppressedWhenModeOff: mode.codemap=off suppresses
// the injection even when map files already exist from earlier sessions.
func TestCodemapInjectionSuppressedWhenModeOff(t *testing.T) {
	state, repo := codemapTestRepo(t, "a.go")
	on := config.Default()
	decideSessionEnd(hookio.Input{SessionID: "seed", Cwd: repo}, on, state)

	off := config.Default()
	off.Mode.Codemap = "off"
	out := decideUserPromptSubmit(hookio.Input{SessionID: "cm6", Prompt: "hello", Cwd: repo}, off, "", "", state)
	if out.HookSpecificOutput != nil && strings.Contains(out.HookSpecificOutput.AdditionalContext, "Codebase map:") {
		t.Error("codemap injected with mode.codemap off")
	}
}
