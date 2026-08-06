package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
)

func contextFixtureLog(t *testing.T) string {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "decisions.jsonl")
	s := logstore.Open(logPath)
	for _, r := range []logstore.Record{
		// Older session -- must not bleed into the newest session's report.
		{TS: "2026-08-01T10:00Z", SessionID: "old", Surface: "UserPromptSubmit", Action: "inject", Reason: "session guidance injection", BytesAfter: 999},

		// Newest session.
		{TS: "2026-08-06T10:00Z", SessionID: "new", Surface: "UserPromptSubmit", Action: "inject", Reason: "session guidance injection", BytesAfter: 1289},
		{TS: "2026-08-06T10:00Z", SessionID: "new", Surface: "UserPromptSubmit", Action: "inject-codemap", Reason: "codebase map", BytesAfter: 3412},
		{TS: "2026-08-06T10:01Z", SessionID: "new", Surface: "SubagentStart", Action: "inject-subagent", BytesAfter: 180},
		{TS: "2026-08-06T10:01Z", SessionID: "new", Surface: "SubagentStart", Action: "inject-subagent"}, // pre-0.16 row, no size
		{TS: "2026-08-06T10:02Z", SessionID: "new", Surface: "PostToolUse", Action: "observed", Reason: "mcp__github__search", BytesAfter: 212004},
		{TS: "2026-08-06T10:03Z", SessionID: "new", Surface: "PostToolUse", Action: "observed", Reason: "Read", BytesAfter: 61220},
		{TS: "2026-08-06T10:04Z", SessionID: "new", Surface: "UserPromptSubmit", Action: "advise", Reason: "large-paste", BytesAfter: 34102},
		{TS: "2026-08-06T10:05Z", SessionID: "new", Surface: "PreToolUse/Bash", Action: "rewrite", Reason: "test-filter", BytesBeforeEst: 30000, BytesAfter: 9600},
		{TS: "2026-08-06T10:05Z", SessionID: "new", Surface: "PostToolUse", Action: "measured", Reason: "test-filter", BytesAfter: 313},
	} {
		if err := s.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	return logPath
}

// TestContextRanksNewestSession: default view is the newest session by
// TS, sections present with measured/estimated labels intact, arrivals
// labeled a floor, and the unsized pre-0.16 inject-subagent row shown as
// unrecorded rather than folded into a total as zero.
func TestContextRanksNewestSession(t *testing.T) {
	out := captureStdout(t, func() { renderContext(contextFixtureLog(t), "") })

	for _, want := range []string{
		"session new",
		"Injected by deadeye",
		"inject-codemap",
		"3,412",
		"size not recorded",
		"Observed arrivals",
		"a floor, not a session total",
		"mcp__github__search",
		"212,004",
		"large-paste (prompt)",
		"Kept out of context",
		"estimated",
		"measured",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("context output missing %q\n%s", want, out)
		}
	}
	// The older session's injection must not appear in the totals.
	if strings.Contains(out, "999") {
		t.Errorf("older session's bytes bled into the newest session's report:\n%s", out)
	}
	// Injected total = 1289 + 3412 + 180 (the unsized row excluded).
	if !strings.Contains(out, "4,881") {
		t.Errorf("injected total wrong (want 4,881 excluding the unsized row):\n%s", out)
	}
}

// TestContextExplicitAndUnknownSession: an exact session arg selects that
// session; an unknown one lists the newest candidates and exits cleanly.
func TestContextExplicitAndUnknownSession(t *testing.T) {
	logPath := contextFixtureLog(t)

	out := captureStdout(t, func() { renderContext(logPath, "old") })
	if !strings.Contains(out, "session old") || !strings.Contains(out, "999") {
		t.Errorf("explicit session arg not honored:\n%s", out)
	}

	out = captureStdout(t, func() { renderContext(logPath, "nope") })
	if !strings.Contains(out, "No decisions logged for session") || !strings.Contains(out, "new") {
		t.Errorf("unknown session should list candidates:\n%s", out)
	}
}

func TestContextEmptyLog(t *testing.T) {
	out := captureStdout(t, func() { renderContext(filepath.Join(t.TempDir(), "none.jsonl"), "") })
	if !strings.Contains(out, "No decisions logged yet") {
		t.Errorf("empty-log message missing: %s", out)
	}
}
