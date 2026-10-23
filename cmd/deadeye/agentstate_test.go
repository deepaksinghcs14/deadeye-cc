package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
)

// A subagent shares its parent's session_id; only agent_id tells them
// apart. These tests feed hook inputs shaped like the real ones (session_id
// on every call, agent_id/agent_type only inside a subagent) and pin that
// the per-agent advisories no longer cross that boundary.

func readAs(t *testing.T, state *daemonState, agentID, path string) string {
	t.Helper()
	in := hookio.Input{SessionID: "s1", AgentID: agentID, ToolName: "Read", ToolInput: readToolInput(t, path, nil)}
	out := decideReadAdvice(in, config.Default(), state)
	if out.HookSpecificOutput == nil {
		return ""
	}
	return out.HookSpecificOutput.AdditionalContext
}

func TestAlreadyReadIsPerAgent(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	path := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(path, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const flag = "already read"

	if got := readAs(t, state, "agent-1", path); got != "" {
		t.Fatalf("first read by a subagent advised: %q", got)
	}
	if got := readAs(t, state, "", path); strings.Contains(got, flag) {
		t.Errorf("parent told %q for a file only its subagent read", got)
	}
	if got := readAs(t, state, "agent-2", path); strings.Contains(got, flag) {
		t.Errorf("sibling told %q for a file only another subagent read", got)
	}
	// The same agent reading twice, unchanged, is still the real case.
	if got := readAs(t, state, "agent-1", path); !strings.Contains(got, flag) {
		t.Errorf("same subagent re-read not flagged: %q", got)
	}
	if got := readAs(t, state, "", path); !strings.Contains(got, flag) {
		t.Errorf("parent re-read not flagged: %q", got)
	}
}

func TestRepeatCommandIsPerAgent(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	cfg := config.Default()
	run := func(agentID string) string {
		b, _ := json.Marshal(map[string]any{"command": "ls -la"})
		out := decideBashPreprocess(hookio.Input{SessionID: "s1", AgentID: agentID, ToolName: "Bash", ToolInput: b}, cfg, state)
		if out.HookSpecificOutput == nil {
			return ""
		}
		return out.HookSpecificOutput.AdditionalContext
	}
	if run("agent-1") != "" {
		t.Fatal("first run advised")
	}
	if got := run(""); got != "" {
		t.Errorf("parent's first run flagged as repeat of its subagent's: %q", got)
	}
	if got := run("agent-1"); !strings.Contains(got, "identical") {
		t.Errorf("same subagent's immediate repeat not flagged: %q", got)
	}
}

func TestContextArrivalCountsOnlyTheCurrentAgent(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	cfg := config.Default()
	big, _ := json.Marshal(strings.Repeat("x", compactAdviseBytes))
	// A subagent's tool output never entered the parent's context.
	decidePostToolUse(hookio.Input{SessionID: "s1", AgentID: "agent-1", ToolName: "Grep", ToolResponse: big}, cfg, state)
	if n, fire := state.shouldAdviseCompact("s1", compactAdviseBytes); fire {
		t.Errorf("parent advised to compact after %d bytes that only a subagent saw", n)
	}
	decidePostToolUse(hookio.Input{SessionID: "s1", ToolName: "Grep", ToolResponse: big}, cfg, state)
	if _, fire := state.shouldAdviseCompact("s1", compactAdviseBytes); !fire {
		t.Error("parent's own output did not count")
	}
}

func TestAgentStateIsFreedAndMergedForCodemap(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	readAs(t, state, "", a)
	readAs(t, state, "agent-1", b)
	if got := state.readFilesSnapshot("s1"); len(got) != 2 {
		t.Errorf("session snapshot = %v, want both agents' files", got)
	}
	state.endAgent(hookio.Input{SessionID: "s1", AgentID: "agent-1"})
	if got := state.readFilesSnapshot("s1"); len(got) != 1 {
		t.Errorf("after SubagentStop snapshot = %v, want the parent's file only", got)
	}
	readAs(t, state, "agent-2", b)
	state.endSession("s1")
	if len(state.sessions) != 0 {
		t.Errorf("SessionEnd left %d state entries", len(state.sessions))
	}
}

// If a host never sends agent_id (a real PreToolUse payload from inside a
// subagent has not been observed; only SubagentStart is captured), state
// falls back to the session id: the pre-existing behaviour, never an error
// and never lost advice.
func TestMissingAgentIDFallsBackToSessionState(t *testing.T) {
	if got := agentKey(hookio.Input{SessionID: "s1"}); got != "s1" {
		t.Fatalf("agentKey without agent_id = %q, want the session id", got)
	}
	state := newDaemonState(catalog.Catalog{}, nil)
	path := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(path, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readAs(t, state, "", path); got != "" {
		t.Fatalf("first read advised: %q", got)
	}
	if got := readAs(t, state, "", path); !strings.Contains(got, "already read") {
		t.Errorf("without agent_id the session-level repeat was lost: %q", got)
	}
	state.endAgent(hookio.Input{SessionID: "s1"}) // no agent_id: must be a no-op
	if len(state.sessions) != 1 {
		t.Errorf("endAgent without agent_id changed state: %d entries", len(state.sessions))
	}
}
