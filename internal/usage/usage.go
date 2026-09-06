// Package usage cross-checks deadeye's own estimated savings against real
// token usage Claude Code already measured -- the same numbers /usage
// renders -- by reading the session transcripts Claude Code writes to
// ~/.claude/projects/<encoded-cwd>/*.jsonl. That directory layout and the
// transcript's JSON shape are Claude Code's own internal format, not a
// documented or versioned API, so every read here fails open: a missing
// directory, an unreadable file, or a line that doesn't parse is skipped,
// never surfaced as an error. This is a display cross-check, not
// load-bearing state.
package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Totals is real, measured token usage summed across a project's
// transcripts.
type Totals struct {
	Sessions            int
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

func (t Totals) Empty() bool { return t.Sessions == 0 }

// Total is every token dimension combined -- the number to weigh an
// estimated savings figure against.
func (t Totals) Total() int64 {
	return t.InputTokens + t.OutputTokens + t.CacheReadTokens + t.CacheCreationTokens
}

// ConfigDir is $CLAUDE_CONFIG_DIR, or ~/.claude if unset -- same resolution
// Claude Code itself uses.
func ConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// ProjectDir is the transcript directory Claude Code uses for cwd, mirroring
// its own (undocumented) encoding: every "/" in the absolute path becomes
// "-". Verified against this repo's own ~/.claude/projects entries; if
// Claude Code ever changes the scheme, ScanProject just finds nothing.
func ProjectDir(configDir, cwd string) string {
	return filepath.Join(configDir, "projects", strings.ReplaceAll(cwd, "/", "-"))
}

// transcriptLine is the handful of fields ScanProject needs from one
// Claude Code transcript JSONL line -- everything else is ignored.
type transcriptLine struct {
	Type    string `json:"type"`
	Message struct {
		ID    string `json:"id"`
		Usage struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// ScanProject sums real usage across every transcript in cwd's project
// directory. Claude Code writes one JSONL line per content block (text,
// thinking, tool use), and sibling lines from the same API response repeat
// the same usage figures -- so lines are deduped by the shared message id,
// counting each assistant turn once.
func ScanProject(configDir, cwd string) Totals {
	entries, err := os.ReadDir(ProjectDir(configDir, cwd))
	if err != nil {
		return Totals{}
	}

	var t Totals
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if scanFile(filepath.Join(ProjectDir(configDir, cwd), e.Name()), seen, &t) {
			t.Sessions++
		}
	}
	return t
}

// scanFile folds one transcript's usage into t, returning whether it
// contributed at least one not-yet-seen assistant turn.
func scanFile(path string, seen map[string]bool, t *Totals) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	contributed := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024) // a tool-result line can be large
	for sc.Scan() {
		var l transcriptLine
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Type != "assistant" || l.Message.ID == "" {
			continue
		}
		if seen[l.Message.ID] {
			continue
		}
		seen[l.Message.ID] = true
		contributed = true
		t.InputTokens += l.Message.Usage.InputTokens
		t.OutputTokens += l.Message.Usage.OutputTokens
		t.CacheReadTokens += l.Message.Usage.CacheReadInputTokens
		t.CacheCreationTokens += l.Message.Usage.CacheCreationInputTokens
	}
	return contributed
}
