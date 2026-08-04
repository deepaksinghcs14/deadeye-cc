package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
)

func readToolInput(t *testing.T, path string, extra map[string]any) json.RawMessage {
	t.Helper()
	m := map[string]any{"file_path": path}
	for k, v := range extra {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestReadAdviceFlagsUnchangedRepeat: re-reading a file that hasn't
// changed since it was last read is the biggest silent token sink in long
// sessions -- flag it. A read after the file CHANGED is legitimate and
// must stay quiet.
func TestReadAdviceFlagsUnchangedRepeat(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	path := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(path, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := hookio.Input{SessionID: "s1", ToolName: "Read", ToolInput: readToolInput(t, path, nil)}

	if out := decideReadAdvice(in, config.Default(), state); out.HookSpecificOutput != nil {
		t.Errorf("first read advised: %+v", out.HookSpecificOutput)
	}
	out := decideReadAdvice(in, config.Default(), state)
	if out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "already read") {
		t.Fatalf("unchanged repeat read not flagged: %+v", out.HookSpecificOutput)
	}

	// Change the file (bump mtime into the future to defeat coarse mtime
	// granularity) -- the next read is legitimate.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if out := decideReadAdvice(in, config.Default(), state); out.HookSpecificOutput != nil {
		t.Errorf("read after a change was flagged: %+v", out.HookSpecificOutput)
	}
}

// TestReadAdviceFlagsFullReadOfLargeFile: a whole-file Read past 200KB
// gets a grep-first suggestion; a bounded (offset/limit) read of the same
// file is exactly the right move and stays quiet.
func TestReadAdviceFlagsFullReadOfLargeFile(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	path := filepath.Join(t.TempDir(), "big.txt")
	if err := os.WriteFile(path, make([]byte, 300*1024), 0o644); err != nil {
		t.Fatal(err)
	}

	out := decideReadAdvice(hookio.Input{SessionID: "s1", ToolName: "Read", ToolInput: readToolInput(t, path, nil)}, config.Default(), state)
	if out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "Grep") {
		t.Fatalf("full read of a 300KB file not flagged: %+v", out.HookSpecificOutput)
	}

	bounded := decideReadAdvice(hookio.Input{SessionID: "s2", ToolName: "Read", ToolInput: readToolInput(t, path, map[string]any{"offset": 100, "limit": 50})}, config.Default(), state)
	if bounded.HookSpecificOutput != nil {
		t.Errorf("bounded read of a large file was flagged: %+v", bounded.HookSpecificOutput)
	}
}

// TestRepeatCommandAdvice: the same Bash command run twice with no
// Edit/Write between is the retry-loop pathology. An edit in between
// makes the re-run legitimate verification.
func TestRepeatCommandAdvice(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	cfg := config.Default()
	bash := func() hookio.Output {
		b, _ := json.Marshal(map[string]any{"command": "ls -la"})
		return decideBashPreprocess(hookio.Input{SessionID: "s1", ToolName: "Bash", ToolInput: b}, cfg, state)
	}

	if out := bash(); out.HookSpecificOutput != nil {
		t.Errorf("first run advised: %+v", out.HookSpecificOutput)
	}
	out := bash()
	if out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "identical") {
		t.Fatalf("consecutive repeat not flagged: %+v", out.HookSpecificOutput)
	}

	// An Edit clears the marker -- the re-run after it must stay quiet.
	state.clearLastBash("s1")
	if out := bash(); out.HookSpecificOutput != nil {
		t.Errorf("re-run after an edit was flagged: %+v", out.HookSpecificOutput)
	}
}

// TestPostToolUseMeasuresRewrittenCommand: the bytes logged at rewrite
// time are per-rule estimates (the command hasn't run yet); PostToolUse
// sees the real response and must log its actual size against the rule.
func TestPostToolUseMeasuresRewrittenCommand(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "decisions.jsonl")
	state := newDaemonState(catalog.Catalog{}, logstore.Open(logPath))
	cfg := config.Default()

	b, _ := json.Marshal(map[string]any{"command": "go test ./..."})
	out := decideBashPreprocess(hookio.Input{SessionID: "s1", ToolName: "Bash", ToolInput: b}, cfg, state)
	if out.HookSpecificOutput == nil || out.HookSpecificOutput.UpdatedInput == nil {
		t.Fatal("expected go test to be rewritten")
	}
	var rewritten struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(out.HookSpecificOutput.UpdatedInput, &rewritten); err != nil {
		t.Fatal(err)
	}

	response := json.RawMessage(`{"stdout":"--- FAIL: TestX\nFAIL\n","stderr":""}`)
	postInput, _ := json.Marshal(map[string]any{"command": rewritten.Command})
	decidePostToolUse(hookio.Input{SessionID: "s1", ToolName: "Bash", ToolInput: postInput, ToolResponse: response}, config.Default(), state)

	records, err := logstore.Scan(logPath)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range records {
		if r.Action == "measured" {
			found = true
			if r.Reason != "test-filter" {
				t.Errorf("measured record attributed to %q, want test-filter", r.Reason)
			}
			if r.BytesAfter != len(response) {
				t.Errorf("measured BytesAfter = %d, want the real response size %d", r.BytesAfter, len(response))
			}
		}
	}
	if !found {
		t.Fatalf("no 'measured' record logged; got %+v", records)
	}

	// A second PostToolUse for the same command must NOT measure again --
	// the pending rewrite is consumed once.
	decidePostToolUse(hookio.Input{SessionID: "s1", ToolName: "Bash", ToolInput: postInput, ToolResponse: response}, config.Default(), state)
	records, _ = logstore.Scan(logPath)
	n := 0
	for _, r := range records {
		if r.Action == "measured" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("measured %d times, want exactly 1", n)
	}
}

// TestPostToolUseObservesMCPTools: MCP tool inputs can't be rewritten
// safely, so deadeye builds the evidence base first -- every mcp__* call
// logs its response size.
func TestPostToolUseObservesMCPTools(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "decisions.jsonl")
	state := newDaemonState(catalog.Catalog{}, logstore.Open(logPath))

	response := json.RawMessage(`{"big":"` + strings.Repeat("x", 500) + `"}`)
	decidePostToolUse(hookio.Input{SessionID: "s1", ToolName: "mcp__github__search", ToolResponse: response}, config.Default(), state)

	records, err := logstore.Scan(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Action != "observed" || records[0].Reason != "mcp__github__search" || records[0].BytesAfter != len(response) {
		t.Errorf("got %+v, want one 'observed' record for mcp__github__search with the real size", records)
	}
}

// TestAdvisorySurfacesRespectPreprocessOff: every surface must have an
// off switch -- the Read advisories and subagent brevity note sit under
// mode.preprocess (same family as the Bash-output rules), so turning that
// off must silence them too.
func TestAdvisorySurfacesRespectPreprocessOff(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	cfg := config.Default()
	cfg.Mode.Preprocess = "off"

	path := filepath.Join(t.TempDir(), "big.txt")
	if err := os.WriteFile(path, make([]byte, 300*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	in := hookio.Input{SessionID: "s1", ToolName: "Read", ToolInput: readToolInput(t, path, nil)}
	decideReadAdvice(in, cfg, state) // first read records it
	if out := decideReadAdvice(in, cfg, state); out.HookSpecificOutput != nil {
		t.Errorf("read advice fired with mode.preprocess=off: %+v", out.HookSpecificOutput)
	}
	// The brevity note obeys mode.preprocess; the coder persona has its
	// OWN switch (coder.default_level / DEADEYE_CODER) and legitimately
	// still fires here -- assert the specific absence, not total silence.
	out := decideSubagentStart(hookio.Input{SessionID: "s1", AgentID: "a1"}, cfg, state)
	if out.HookSpecificOutput != nil && strings.Contains(out.HookSpecificOutput.AdditionalContext, "terse, structured results") {
		t.Errorf("subagent brevity note fired with mode.preprocess=off: %+v", out.HookSpecificOutput)
	}

	// With coder ALSO off, the surface must be fully silent.
	cfg.Coder.Disabled = true
	if out := decideSubagentStart(hookio.Input{SessionID: "s1", AgentID: "a1"}, cfg, state); out.HookSpecificOutput != nil {
		t.Errorf("SubagentStart emitted with preprocess off AND coder disabled: %+v", out.HookSpecificOutput)
	}
}

// TestSubagentStartInjectsBrevityGuidance: subagent output lands in the
// parent's context whole, so ask for terse results. (Whether the surface
// actually delivers additionalContext is unverified -- see the handler's
// comment -- but the response must at least be well-formed.)
func TestSubagentStartInjectsBrevityGuidance(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	out := decideSubagentStart(hookio.Input{SessionID: "s1", AgentID: "a1"}, config.Default(), state)
	if out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "terse") {
		t.Errorf("expected brevity guidance, got %+v", out.HookSpecificOutput)
	}
}
