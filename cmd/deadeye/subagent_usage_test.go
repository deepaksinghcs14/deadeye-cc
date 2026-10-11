package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
)

func scanLog(t *testing.T, path string) []logstore.Record {
	t.Helper()
	recs, err := logstore.Scan(path)
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

// The recommendation and the model that ran are joined by tool_use_id: two
// Agent calls in flight at once must not swap recommendations, which a
// single "last routing" slot would do.
func TestResolvedModelPairsWithItsOwnRecommendation(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log.jsonl")
	state := newDaemonState(testCatalogForLessons(), logstore.Open(logPath))
	state.notePendingAgent("s", "call-a", "model-a")
	state.notePendingAgent("s", "call-b", "model-b")

	for _, c := range []struct{ id, ran string }{{"call-b", "model-b"}, {"call-a", "model-x"}} {
		resp, _ := json.Marshal(map[string]any{"status": "async_launched", "resolvedModel": c.ran})
		decidePostToolUse(hookio.Input{SessionID: "s", ToolName: "Agent", ToolUseID: c.id, ToolResponse: resp}, config.Default(), state)
	}

	var got []logstore.Record
	for _, r := range scanLog(t, logPath) {
		if r.Action == "resolved" {
			got = append(got, r)
		}
	}
	if len(got) != 2 {
		t.Fatalf("got %d resolved rows, want 2: %+v", len(got), got)
	}
	if got[0].Model != "model-b" || got[0].Reason != "recommended model-b" {
		t.Errorf("call-b row = %+v", got[0])
	}
	if got[1].Model != "model-x" || got[1].Reason != "recommended model-a" {
		t.Errorf("call-a row = %+v", got[1])
	}
	if left := state.consumePendingAgent("s", "call-a"); left != "" {
		t.Errorf("recommendation not consumed: %q", left)
	}
}

// A subagent transcript writes a turn's first content block with the output
// count as it stood mid-stream; the turn's last line has the real figure.
// Shape taken from a real Claude Code 2.1.296 subagent transcript.
func TestSubagentStopRecordsWholeRunFromTranscript(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "agent.jsonl")
	lines := `{"type":"user","message":{"role":"user"}}
{"type":"assistant","message":{"id":"m1","model":"claude-haiku-5-5","usage":{"input_tokens":2,"cache_creation_input_tokens":19382,"cache_read_input_tokens":0,"output_tokens":4}}}
{"type":"assistant","message":{"id":"m1","model":"claude-haiku-5-5","usage":{"input_tokens":2,"cache_creation_input_tokens":19382,"cache_read_input_tokens":0,"output_tokens":37}}}
{"type":"assistant","message":{"id":"m2","model":"claude-haiku-5-5","usage":{"input_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":19382,"output_tokens":100}}}
`
	if err := os.WriteFile(transcript, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "log.jsonl")
	state := newDaemonState(testCatalogForLessons(), logstore.Open(logPath))

	in := hookio.Input{SessionID: "s", AgentType: "general-purpose", AgentTranscriptPath: transcript, Effort: &hookio.Effort{Level: "low"}}
	decideSubagentStop(in, config.Default(), state)

	recs := scanLog(t, logPath)
	if len(recs) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(recs), recs)
	}
	r := recs[0]
	if r.Action != "subagent-usage" || r.Model != "claude-haiku-5-5" || r.Reason != "general-purpose effort=low" {
		t.Errorf("row = %+v", r)
	}
	if r.InputTokens != 7 || r.OutputTokens != 137 || r.CacheCreationTokens != 19382 || r.CacheReadTokens != 19382 {
		t.Errorf("tokens = in %d out %d create %d read %d; want 7/137/19382/19382 (output must use each turn's LAST line)",
			r.InputTokens, r.OutputTokens, r.CacheCreationTokens, r.CacheReadTokens)
	}

	// A missing transcript is a no-op, not a zero row that would dilute averages.
	decideSubagentStop(hookio.Input{SessionID: "s", AgentTranscriptPath: filepath.Join(dir, "absent.jsonl")}, config.Default(), state)
	if n := len(scanLog(t, logPath)); n != 1 {
		t.Errorf("missing transcript logged a row: %d rows", n)
	}
}
