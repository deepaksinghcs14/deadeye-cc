package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
)

// TestPlanGateThresholdDiscipline is PLAN.md §5.4's explicit requirement:
// "single-file, specific, low-radius prompts must pass through with zero
// friction." Vague prompts should trigger regardless of the working tree;
// purely exploratory prompts (no implementation verb) never should.
func TestPlanGateThresholdDiscipline(t *testing.T) {
	cfg := config.PlanGate{MinFiles: 2, RadiusTrigger: true}

	cases := []struct {
		name     string
		prompt   string
		wantFire bool
	}{
		{"specific single-file edit", "Rename the variable x to count in main.go", false},
		{"pure question, no implementation verb", "What does the daemon do when the socket already exists?", false},
		{"vague implementation request", "Not sure, maybe look into a redesign of the architecture across the codebase?", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := hookio.Input{Prompt: c.prompt, Cwd: t.TempDir()}
			_, _, fire := planGateSoftTrigger(in, cfg)
			if fire != c.wantFire {
				t.Errorf("planGateSoftTrigger(%q) fired=%v, want %v", c.prompt, fire, c.wantFire)
			}
		})
	}
}

// TestPlanGateFiresOnMultiFileWorkingTree: filescope's signal is about the
// CURRENT working tree's modified/staged files, not filenames mentioned in
// the prompt text -- an implementation-shaped prompt against a working
// tree with several already-touched files should trigger even without any
// vague wording.
func TestPlanGateFiresOnMultiFileWorkingTree(t *testing.T) {
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
	for _, f := range []string{"a.go", "b.go", "c.go"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("package p\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", ".")
	run("commit", "-q", "-m", "initial")
	for _, f := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("package p\n// changed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", ".")

	cfg := config.PlanGate{MinFiles: 2, RadiusTrigger: true}
	in := hookio.Input{Prompt: "Update these files to add a new field", Cwd: dir}
	_, _, fire := planGateSoftTrigger(in, cfg)
	if !fire {
		t.Error("expected the gate to fire against a working tree with 2 staged files (MinFiles=2)")
	}
}

func TestPlanGateHardDefaultsOff(t *testing.T) {
	state := newDaemonState(config.Default(), catalog.Catalog{}, nil)
	state.setPendingPlan("sess1", "some pending task")

	out := decidePlanGateHard(hookio.Input{SessionID: "sess1", ToolName: "Edit"}, state)
	if out.HookSpecificOutput != nil {
		t.Errorf("hard layer fired with default config (plan_gate=%q), want no-op", config.Default().Mode.PlanGate)
	}
}

func TestPlanGateHardAsksOnceThenClears(t *testing.T) {
	cfg := config.Default()
	cfg.Mode.PlanGate = "hard"
	state := newDaemonState(cfg, catalog.Catalog{}, nil)
	state.setPendingPlan("sess1", "pending task")

	in := hookio.Input{SessionID: "sess1", ToolName: "Edit"}
	first := decidePlanGateHard(in, state)
	if first.HookSpecificOutput == nil || first.HookSpecificOutput.PermissionDecision != hookio.PermissionAsk {
		t.Fatalf("first Edit with a pending plan = %+v, want permissionDecision=ask", first)
	}

	second := decidePlanGateHard(in, state)
	if second.HookSpecificOutput != nil {
		t.Errorf("second Edit after the gate already asked once = %+v, want no-op (gate clears after asking)", second)
	}
}
