package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
)

// TestGainSeparatesMeasuredFromEstimated: the scoreboard must label
// per-rule estimate constants and real measured bytes differently --
// "measured, not estimated" forbids blending them into one figure.
func TestGainSeparatesMeasuredFromEstimated(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "decisions.jsonl")
	s := logstore.Open(logPath)
	for _, r := range []logstore.Record{
		{TS: "t", SessionID: "s1", Surface: "PreToolUse/Bash", Action: "rewrite", Reason: "test-filter", BytesBeforeEst: 30000, BytesAfter: 9600},
		{TS: "t", SessionID: "s1", Surface: "PostToolUse", Action: "measured", Reason: "test-filter", BytesAfter: 313},
		{TS: "t", SessionID: "s1", Surface: "PreToolUse/Read", Action: "advise", Reason: "read-advice"},
		{TS: "t", SessionID: "s1", Surface: "PostToolUse", Action: "observed", Reason: "mcp__github__search", BytesAfter: 2048},
		{TS: "t", SessionID: "s1", Surface: "SessionStart", Action: "coder-inject", Reason: "coder ruleset injection", BytesAfter: 4200},
	} {
		if err := s.Append(r); err != nil {
			t.Fatal(err)
		}
	}

	out := captureStdout(t, func() { renderGain(logPath) })

	for _, want := range []string{
		"1 verbose commands trimmed",
		"estimated",
		"per-rule typical-case constants",
		"measured",
		"1 real runs",
		"test-filter",
		"1 nudges",
		"MCP observed",
		"1 session injections",
		"never written, so there is no baseline",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("gain output missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "%") && strings.Contains(out, "saved") {
		t.Error("gain output must not print a savings percentage")
	}
}

func TestGainEmptyLog(t *testing.T) {
	out := captureStdout(t, func() { renderGain(filepath.Join(t.TempDir(), "none.jsonl")) })
	if !strings.Contains(out, "No decisions logged yet") {
		t.Errorf("empty-log message missing: %s", out)
	}
}

func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}
