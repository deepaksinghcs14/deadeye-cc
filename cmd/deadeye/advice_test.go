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

func bashIn(sessionID, cmd string) hookio.Input {
	b, _ := json.Marshal(map[string]any{"command": cmd})
	return hookio.Input{SessionID: sessionID, ToolName: "Bash", ToolInput: b}
}

// TestBashRetryAdvises: the flag-escalation retry loop -- the same target
// re-run with only option changes and no edits in between -- fires on the
// third same-key run, riding the rewrite response when a filter rule
// matched (go test is exactly such a command).
func TestBashRetryAdvises(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	cfg := config.Default()

	hasRetry := func(out hookio.Output) bool {
		return out.HookSpecificOutput != nil &&
			strings.Contains(out.HookSpecificOutput.AdditionalContext, "only flag changes")
	}

	if out := decideBashPreprocess(bashIn("s1", "go test ./internal/auth"), cfg, state); hasRetry(out) {
		t.Error("first run advised")
	}
	if out := decideBashPreprocess(bashIn("s1", "go test -v ./internal/auth"), cfg, state); hasRetry(out) {
		t.Error("second run advised")
	}
	out := decideBashPreprocess(bashIn("s1", "go test -race -v ./internal/auth"), cfg, state)
	if !hasRetry(out) {
		t.Fatalf("third same-key run not advised: %+v", out.HookSpecificOutput)
	}
	if out.HookSpecificOutput.UpdatedInput == nil {
		t.Error("retry advisory must not displace the test-filter rewrite")
	}
	// Fourth run: once per key, not re-nagged.
	if out := decideBashPreprocess(bashIn("s1", "go test -count=5 -race -v ./internal/auth"), cfg, state); hasRetry(out) {
		t.Error("fourth run re-nagged")
	}
}

// TestBashRetryStaysQuiet: the false-positive side -- legitimately
// different runs, raw-identical repeats (repeat-command's line), and an
// edit between runs must all stay silent.
func TestBashRetryStaysQuiet(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	cfg := config.Default()

	hasRetry := func(out hookio.Output) bool {
		return out.HookSpecificOutput != nil &&
			strings.Contains(out.HookSpecificOutput.AdditionalContext, "only flag changes")
	}

	// Different -run= values: different keys, three in a row stay quiet.
	for _, cmd := range []string{"go run . -run=TestA", "go run . -run=TestB", "go run . -run=TestC"} {
		if out := decideBashPreprocess(bashIn("s1", cmd), cfg, state); hasRetry(out) {
			t.Errorf("different-target run advised: %s", cmd)
		}
	}

	// Raw-identical third run: repeat-command owns it, never both.
	for i := 0; i < 3; i++ {
		out := decideBashPreprocess(bashIn("s2", "ls -la src"), cfg, state)
		if hasRetry(out) {
			t.Error("raw-identical repeat drew the bash-retry advisory")
		}
	}

	// An Edit between runs resets the streak.
	decideBashPreprocess(bashIn("s3", "pytest tests/a.py"), cfg, state)
	decideBashPreprocess(bashIn("s3", "pytest tests/a.py -x"), cfg, state)
	state.clearLastBash("s3") // what the Edit/Write arm does
	if out := decideBashPreprocess(bashIn("s3", "pytest tests/a.py -vv"), cfg, state); hasRetry(out) {
		t.Error("run after an edit counted toward the retry streak")
	}

	// Piped commands never participate.
	for i := 0; i < 4; i++ {
		out := decideBashPreprocess(bashIn("s4", "grep -r auth src | head -5"), cfg, state)
		if hasRetry(out) {
			t.Error("piped command drew the retry advisory")
		}
	}
}

func webFetchIn(sessionID, url string) hookio.Input {
	b, _ := json.Marshal(map[string]any{"url": url, "prompt": "summarize"})
	return hookio.Input{SessionID: sessionID, ToolName: "WebFetch", ToolInput: b}
}

// TestWebFetchAdviceFlagsRepeat: re-fetching a URL already fetched this
// session buys nothing while the first response is still in context.
// Fragment and trailing-slash variants name the same resource; a different
// query string is a genuinely different fetch.
func TestWebFetchAdviceFlagsRepeat(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	cfg := config.Default()

	if out := decidePreToolUse(webFetchIn("s1", "https://docs.example.com/api"), cfg, state); out.HookSpecificOutput != nil {
		t.Errorf("first fetch advised: %+v", out.HookSpecificOutput)
	}
	// Trailing slash + fragment variant of the SAME resource: repeat.
	out := decidePreToolUse(webFetchIn("s1", "https://docs.example.com/api/#section-2"), cfg, state)
	if out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "already fetched") {
		t.Fatalf("repeat fetch not flagged: %+v", out.HookSpecificOutput)
	}
	// Third fetch of the same URL: not re-nagged.
	if out := decidePreToolUse(webFetchIn("s1", "https://docs.example.com/api"), cfg, state); out.HookSpecificOutput != nil {
		t.Errorf("third fetch re-nagged: %+v", out.HookSpecificOutput)
	}
	// Different query string: a different fetch, silent.
	if out := decidePreToolUse(webFetchIn("s1", "https://docs.example.com/api?page=2"), cfg, state); out.HookSpecificOutput != nil {
		t.Errorf("different query string advised: %+v", out.HookSpecificOutput)
	}
	// A DIFFERENT repeated URL still gets its own advisory.
	decidePreToolUse(webFetchIn("s1", "https://other.example.com"), cfg, state)
	if out := decidePreToolUse(webFetchIn("s1", "https://other.example.com"), cfg, state); out.HookSpecificOutput == nil {
		t.Error("a second repeated URL was not advised")
	}
}

func TestWebFetchAdviceStaysQuiet(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)

	// Malformed tool_input fails open.
	in := hookio.Input{SessionID: "s1", ToolName: "WebFetch", ToolInput: json.RawMessage(`{broken`)}
	if out := decidePreToolUse(in, config.Default(), state); out.HookSpecificOutput != nil {
		t.Errorf("malformed input advised: %+v", out.HookSpecificOutput)
	}
	// Muted session: tracked but silent.
	state.setMuted("s2", true)
	decidePreToolUse(webFetchIn("s2", "https://a.example.com"), config.Default(), state)
	if out := decidePreToolUse(webFetchIn("s2", "https://a.example.com"), config.Default(), state); out.HookSpecificOutput != nil {
		t.Errorf("muted session advised: %+v", out.HookSpecificOutput)
	}
	// Rule disabled: silent.
	cfg := config.Default()
	cfg.Preprocess.DisabledRules = []string{"repeat-webfetch"}
	decidePreToolUse(webFetchIn("s3", "https://b.example.com"), cfg, state)
	if out := decidePreToolUse(webFetchIn("s3", "https://b.example.com"), cfg, state); out.HookSpecificOutput != nil {
		t.Errorf("disabled rule advised: %+v", out.HookSpecificOutput)
	}
}

// TestMCPOversizeAdvises: a single MCP response past mcpOversizeBytes gets
// a post-hoc advisory targeting the NEXT call, once per tool per session --
// smaller responses and repeat offenders stay observation-only.
func TestMCPOversizeAdvises(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "decisions.jsonl")
	state := newDaemonState(catalog.Catalog{}, logstore.Open(logPath))

	big := json.RawMessage(`{"big":"` + strings.Repeat("x", mcpOversizeBytes+1024) + `"}`)
	in := hookio.Input{SessionID: "s1", ToolName: "mcp__github__search", ToolResponse: big}

	out := decidePostToolUse(in, config.Default(), state)
	if out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "narrow the next call") {
		t.Fatalf("oversized MCP response not advised: %+v", out.HookSpecificOutput)
	}

	// Second oversized response from the SAME tool: observed, not re-advised.
	if out := decidePostToolUse(in, config.Default(), state); out.HookSpecificOutput != nil {
		t.Errorf("same tool re-advised: %+v", out.HookSpecificOutput)
	}

	// A DIFFERENT oversized tool still gets its own advisory.
	in2 := hookio.Input{SessionID: "s1", ToolName: "mcp__jira__query", ToolResponse: big}
	if out := decidePostToolUse(in2, config.Default(), state); out.HookSpecificOutput == nil {
		t.Error("a different oversized tool was not advised")
	}

	// The advise row carries the real measured size.
	records, err := logstore.Scan(logPath)
	if err != nil {
		t.Fatal(err)
	}
	advised := 0
	for _, r := range records {
		if r.Action == "advise" && r.Reason == "mcp-oversize" {
			advised++
			if r.BytesAfter != len(big) {
				t.Errorf("advise row BytesAfter = %d, want the real size %d", r.BytesAfter, len(big))
			}
		}
	}
	if advised != 2 {
		t.Errorf("advise rows = %d, want 2 (one per tool)", advised)
	}
}

func TestMCPOversizeStaysQuiet(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	big := json.RawMessage(`{"big":"` + strings.Repeat("x", mcpOversizeBytes+1024) + `"}`)
	small := json.RawMessage(`{"small":"` + strings.Repeat("x", 500) + `"}`)

	// Under threshold: silent.
	if out := decidePostToolUse(hookio.Input{SessionID: "s1", ToolName: "mcp__a__b", ToolResponse: small}, config.Default(), state); out.HookSpecificOutput != nil {
		t.Errorf("under-threshold response advised: %+v", out.HookSpecificOutput)
	}
	// Muted: silent.
	state.setMuted("s1", true)
	if out := decidePostToolUse(hookio.Input{SessionID: "s1", ToolName: "mcp__a__b", ToolResponse: big}, config.Default(), state); out.HookSpecificOutput != nil {
		t.Errorf("muted session advised: %+v", out.HookSpecificOutput)
	}
	// Rule disabled: silent.
	cfg := config.Default()
	cfg.Preprocess.DisabledRules = []string{"mcp-oversize"}
	if out := decidePostToolUse(hookio.Input{SessionID: "s2", ToolName: "mcp__a__b", ToolResponse: big}, cfg, state); out.HookSpecificOutput != nil {
		t.Errorf("disabled rule advised: %+v", out.HookSpecificOutput)
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
