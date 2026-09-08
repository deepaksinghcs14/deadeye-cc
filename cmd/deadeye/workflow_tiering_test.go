package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
)

func TestScanWorkflowTieringCountsUntieredCalls(t *testing.T) {
	script := `
const tiered = agent(prompt, {label: 'x', model: 'claude-haiku-4-5-20251001'})
const spread = agent(prompt, {...baseOpts, label: 'y'})
const untiered = agent(prompt, {label: 'z'})
const alsoUntiered = agent("do the thing (carefully)")
`
	total, untiered := scanWorkflowTiering(script)
	if total != 4 {
		t.Errorf("total = %d, want 4", total)
	}
	if untiered != 2 {
		t.Errorf("untiered = %d, want 2 (only the two calls with neither model: nor a spread)", untiered)
	}
}

func TestScanWorkflowTieringNoAgentCalls(t *testing.T) {
	total, untiered := scanWorkflowTiering(`export const meta = {name: 'x'}\nreturn parallel([])`)
	if total != 0 || untiered != 0 {
		t.Errorf("total/untiered = %d/%d, want 0/0 for a script with no agent() calls", total, untiered)
	}
}

func TestScanWorkflowTieringIgnoresSubagentIdentifier(t *testing.T) {
	// A script defining its own "subagent(" helper must not be misread as
	// an untiered "agent(" call -- the word boundary is the whole point.
	total, _ := scanWorkflowTiering(`function subagent(p) { return callSomething(p) }`)
	if total != 0 {
		t.Errorf("total = %d, want 0 -- subagent( must not match the agent( word-boundary regex", total)
	}
}

func TestDecideWorkflowTieringAdvisesOnUntieredScript(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	script := `const a = agent(prompt, {label: 'x'})`
	toolInput, err := json.Marshal(map[string]any{"script": script})
	if err != nil {
		t.Fatal(err)
	}
	in := hookio.Input{SessionID: "s1", ToolName: "Workflow", Cwd: t.TempDir(), ToolInput: toolInput}

	out := decideWorkflowTiering(in, config.Default(), state)
	if out.HookSpecificOutput == nil {
		t.Fatal("expected an advisory output for an untiered script")
	}
	if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "1 of 1") {
		t.Errorf("advisory = %q, want it to name 1 of 1 untiered call sites", out.HookSpecificOutput.AdditionalContext)
	}
}

func TestDecideWorkflowTieringSilentWhenFullyTiered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	script := `const a = agent(prompt, {label: 'x', model: 'claude-sonnet-5'})`
	toolInput, _ := json.Marshal(map[string]any{"script": script})
	in := hookio.Input{SessionID: "s1", ToolName: "Workflow", Cwd: t.TempDir(), ToolInput: toolInput}

	out := decideWorkflowTiering(in, config.Default(), state)
	if out.HookSpecificOutput != nil {
		t.Errorf("expected no advisory when every agent() call is tiered, got %+v", out.HookSpecificOutput)
	}
}

func TestDecideWorkflowTieringRespectsRoutingOff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	script := `const a = agent(prompt, {label: 'x'})`
	toolInput, _ := json.Marshal(map[string]any{"script": script})
	in := hookio.Input{SessionID: "s1", ToolName: "Workflow", Cwd: t.TempDir(), ToolInput: toolInput}

	cfg := config.Default()
	cfg.Mode.Routing = "off"
	out := decideWorkflowTiering(in, cfg, state)
	if out.HookSpecificOutput != nil {
		t.Errorf("mode.routing=off must silence this advisory, got %+v", out.HookSpecificOutput)
	}
}

func TestDecideWorkflowTieringSkipsScriptPathOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	toolInput, _ := json.Marshal(map[string]any{"scriptPath": "/some/saved/workflow.js"})
	in := hookio.Input{SessionID: "s1", ToolName: "Workflow", Cwd: t.TempDir(), ToolInput: toolInput}

	out := decideWorkflowTiering(in, config.Default(), state)
	if out.HookSpecificOutput != nil {
		t.Errorf("no inline script text to scan (scriptPath only) must never advise, got %+v", out.HookSpecificOutput)
	}
}

func TestDecideWorkflowTieringDedupesIdenticalScript(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	script := `const a = agent(prompt, {label: 'x'})`
	toolInput, _ := json.Marshal(map[string]any{"script": script})

	first := decideWorkflowTiering(hookio.Input{SessionID: "s1", ToolName: "Workflow", Cwd: t.TempDir(), ToolInput: toolInput}, config.Default(), state)
	if first.HookSpecificOutput == nil {
		t.Fatal("expected an advisory on the first call")
	}
	second := decideWorkflowTiering(hookio.Input{SessionID: "s1", ToolName: "Workflow", Cwd: t.TempDir(), ToolInput: toolInput}, config.Default(), state)
	if second.HookSpecificOutput != nil {
		t.Error("the identical script re-submitted in the same session should not re-advise")
	}
}
