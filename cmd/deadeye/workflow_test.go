package main

import (
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
)

func TestWorkflowHintTriggersOnFanOutPhrasing(t *testing.T) {
	state := newDaemonState(config.Default(), catalog.Catalog{}, nil)
	in := hookio.Input{SessionID: "s1", Prompt: "Audit every file across the codebase for dead code"}
	suggestion, fired := decideWorkflowHint(in, state)
	if !fired || suggestion == "" {
		t.Fatal("expected the workflow hint to fire on fan-out phrasing")
	}
}

func TestWorkflowHintSkipsRoutinePrompts(t *testing.T) {
	state := newDaemonState(config.Default(), catalog.Catalog{}, nil)
	in := hookio.Input{SessionID: "s1", Prompt: "Fix the typo in main.go"}
	if _, fired := decideWorkflowHint(in, state); fired {
		t.Error("workflow hint fired on a routine single-file prompt")
	}
}

// TestWorkflowHintFiresAtMostOnceThisSession is INV-2/§5.5: "never more
// than once per task."
func TestWorkflowHintFiresOncePerTask(t *testing.T) {
	state := newDaemonState(config.Default(), catalog.Catalog{}, nil)
	in := hookio.Input{SessionID: "s1", Prompt: "Audit every file across the codebase for dead code"}

	if _, fired := decideWorkflowHint(in, state); !fired {
		t.Fatal("expected the first call to fire")
	}
	if _, fired := decideWorkflowHint(in, state); fired {
		t.Error("workflow hint fired twice for the same task")
	}
}

func TestWorkflowHintRespectsModeOff(t *testing.T) {
	cfg := config.Default()
	cfg.Mode.WorkflowHint = "off"
	state := newDaemonState(cfg, catalog.Catalog{}, nil)
	in := hookio.Input{SessionID: "s1", Prompt: "Audit every file across the codebase for dead code"}
	if _, fired := decideWorkflowHint(in, state); fired {
		t.Error("workflow hint fired with mode.workflow_hint=off")
	}
}
