package main

import (
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
)

func TestWorkflowHintTriggersOnFanOutPhrasing(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	in := hookio.Input{SessionID: "s1", Prompt: "Audit every file across the codebase for dead code"}
	suggestion, fired := decideWorkflowHint(in, config.Default(), "", "", state)
	if !fired || suggestion == "" {
		t.Fatal("expected the workflow hint to fire on fan-out phrasing")
	}
}

func TestWorkflowHintSkipsRoutinePrompts(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	in := hookio.Input{SessionID: "s1", Prompt: "Fix the typo in main.go"}
	if _, fired := decideWorkflowHint(in, config.Default(), "", "", state); fired {
		t.Error("workflow hint fired on a routine single-file prompt")
	}
}

// TestWorkflowHintFiresAtMostOnceThisSession is INV-2/§5.5: "never more
// than once per task."
func TestWorkflowHintFiresOncePerTask(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	in := hookio.Input{SessionID: "s1", Prompt: "Audit every file across the codebase for dead code"}

	if _, fired := decideWorkflowHint(in, config.Default(), "", "", state); !fired {
		t.Fatal("expected the first call to fire")
	}
	if _, fired := decideWorkflowHint(in, config.Default(), "", "", state); fired {
		t.Error("workflow hint fired twice for the same task")
	}
}

func TestWorkflowHintRespectsModeOff(t *testing.T) {
	cfg := config.Default()
	cfg.Mode.WorkflowHint = "off"
	state := newDaemonState(catalog.Catalog{}, nil)
	in := hookio.Input{SessionID: "s1", Prompt: "Audit every file across the codebase for dead code"}
	if _, fired := decideWorkflowHint(in, cfg, "", "", state); fired {
		t.Error("workflow hint fired with mode.workflow_hint=off")
	}
}

// TestWorkflowHintIgnoresSyntheticPrompts mirrors the plan-gate regression:
// a synthetic task-notification "prompt" must not trigger the advisor
// even if it happens to contain fan-out-sounding phrasing.
func TestWorkflowHintIgnoresSyntheticPrompts(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	synthetic := "<task-notification>\n<summary>Audit every file across the codebase for dead code, finished</summary>\n</task-notification>"
	in := hookio.Input{SessionID: "s1", Prompt: synthetic}
	if _, fired := decideWorkflowHint(in, config.Default(), "", "", state); fired {
		t.Error("workflow hint fired on a synthetic task-notification prompt")
	}
}

func TestVersionAtLeast(t *testing.T) {
	for _, tc := range []struct {
		v    string
		want bool
	}{
		{"2.1.220", true}, {"2.1.154", true}, {"2.1.153", false},
		{"2.0.999", false}, {"3.0.0", true}, {"1.9", false},
		{"", true}, {"cli", true}, {"2.x.9", true}, // unknown -> fail open
	} {
		if got := versionAtLeast(tc.v, 2, 1, 154); got != tc.want {
			t.Errorf("versionAtLeast(%q) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

func TestWorkflowHintGatedOnOldClient(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	in := hookio.Input{SessionID: "s1", Prompt: "audit the codebase for dead code"}
	if _, fired := decideWorkflowHint(in, config.Default(), "2.0.100", "", state); fired {
		t.Error("hint must not fire on a client older than 2.1.154")
	}
	if _, fired := decideWorkflowHint(in, config.Default(), "2.1.220", "", state); !fired {
		t.Error("hint should fire on a current client")
	}
}

// TestWorkflowHintSkipsReducedHostsWithNoSideEffects is the phantom-log
// regression: a reduced host (no Agent/subagent surface) must not fire
// AND must not consume the once-per-task dedup or write a decision-log
// entry -- otherwise /deadeye-stats records a "workflow-suggest" the user
// never saw.
func TestWorkflowHintSkipsReducedHostsWithNoSideEffects(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	in := hookio.Input{SessionID: "s1", Prompt: "Audit every file across the codebase for dead code"}
	if _, fired := decideWorkflowHint(in, config.Default(), "", "codex", state); fired {
		t.Error("workflow hint fired on a host with no subagent surface")
	}
	// The dedup key must still be unconsumed: a later call on a supported
	// host for the SAME task must still be able to fire.
	if _, fired := decideWorkflowHint(in, config.Default(), "", "", state); !fired {
		t.Error("reduced-host call consumed the dedup key meant for a supporting host")
	}
}
