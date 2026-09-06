package usage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectDirEncodesSlashesAsDashes(t *testing.T) {
	got := ProjectDir("/home/u/.claude", "/Users/deepaksingh/GolandProjects/deadeye-cc")
	want := "/home/u/.claude/projects/-Users-deepaksingh-GolandProjects-deadeye-cc"
	if got != want {
		t.Errorf("ProjectDir = %q, want %q", got, want)
	}
}

// TestScanProjectDedupesSiblingContentBlocks: Claude Code writes one JSONL
// line per content block (text, thinking, tool_use) from the same API
// response, repeating that response's usage on every sibling line. Without
// deduping by message id, a two-block turn would be double-counted.
func TestScanProjectDedupesSiblingContentBlocks(t *testing.T) {
	configDir := t.TempDir()
	cwd := "/Users/x/proj"
	projDir := ProjectDir(configDir, cwd)
	if err := os.MkdirAll(projDir, 0o700); err != nil {
		t.Fatal(err)
	}

	transcript := `{"type":"assistant","message":{"id":"msg_1","usage":{"input_tokens":2,"output_tokens":10,"cache_read_input_tokens":100,"cache_creation_input_tokens":5}}}
{"type":"assistant","message":{"id":"msg_1","usage":{"input_tokens":2,"output_tokens":10,"cache_read_input_tokens":100,"cache_creation_input_tokens":5}}}
{"type":"user","message":{"id":"","usage":{}}}
{"type":"assistant","message":{"id":"msg_2","usage":{"input_tokens":3,"output_tokens":20,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
not json, must be skipped
`
	if err := os.WriteFile(filepath.Join(projDir, "session-a.jsonl"), []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}

	got := ScanProject(configDir, cwd)
	if got.Sessions != 1 {
		t.Errorf("Sessions = %d, want 1", got.Sessions)
	}
	if got.InputTokens != 5 || got.OutputTokens != 30 {
		t.Errorf("InputTokens/OutputTokens = %d/%d, want 5/30 (msg_1 deduped, msg_2 added once)", got.InputTokens, got.OutputTokens)
	}
	if got.CacheReadTokens != 100 || got.CacheCreationTokens != 5 {
		t.Errorf("cache tokens = %d/%d, want 100/5", got.CacheReadTokens, got.CacheCreationTokens)
	}
	if got.Total() != 140 {
		t.Errorf("Total() = %d, want 140", got.Total())
	}
}

func TestScanProjectMissingDirReturnsEmpty(t *testing.T) {
	got := ScanProject(t.TempDir(), "/nonexistent/cwd")
	if !got.Empty() {
		t.Errorf("expected Empty() for a project dir that was never created, got %+v", got)
	}
}
