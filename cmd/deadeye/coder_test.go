package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

func coderTestState(t *testing.T) *daemonState {
	t.Helper()
	t.Setenv("HOME", t.TempDir()) // isolate mode file + outcomes cache
	return newDaemonState(catalog.Catalog{}, nil)
}

func TestCoderSessionStartInjectsDefaultLevel(t *testing.T) {
	state := coderTestState(t)
	out := decideCoderSessionStart(hookio.Input{SessionID: "s1"}, config.Default(), "", state)
	if out.Raw == nil {
		t.Fatal("expected a raw SessionStart injection at the default level")
	}
	text := string(out.Raw)
	if !strings.HasPrefix(text, "DEADEYE CODER ACTIVE — level: marksman") {
		t.Errorf("banner wrong: %q", text[:60])
	}
	if !strings.Contains(text, "| **marksman** |") || strings.Contains(text, "| **sniper** |") {
		t.Error("injection not filtered to the active level")
	}
	// Mode file mirrors the level for the statusline.
	b, err := readFileTrim(meta.CoderModePath())
	if err != nil || b != "marksman" {
		t.Errorf("mode file = %q, %v; want marksman", b, err)
	}
}

func TestCoderSessionStartOffIsSilent(t *testing.T) {
	state := coderTestState(t)
	cfg := config.Default()
	cfg.Coder.DefaultLevel = "off"
	out := decideCoderSessionStart(hookio.Input{SessionID: "s1"}, cfg, "", state)
	if out.Raw != nil || out.HookSpecificOutput != nil {
		t.Errorf("expected silence with default off, got %+v", out)
	}
}

func TestCoderSessionStartKillSwitchBeatsSessionLevel(t *testing.T) {
	state := coderTestState(t)
	state.setCoderLevel("s1", "sniper") // an already-active session choice
	cfg := config.Default()
	cfg.Coder.Disabled = true // DEADEYE_CODER=off arrived on this request
	out := decideCoderSessionStart(hookio.Input{SessionID: "s1"}, cfg, "", state)
	if out.Raw != nil {
		t.Error("kill switch must silence an already-active session level")
	}
}

// TestCoderSessionStartReinjectsSwitchedLevel: a compact-source
// SessionStart mid-session must re-inject at the level the session
// SWITCHED to, not the config default -- this is the one behavior where
// daemon-side session state beats ponytail's global flag file.
func TestCoderSessionStartReinjectsSwitchedLevel(t *testing.T) {
	state := coderTestState(t)
	cfg := config.Default()
	decideCoderSessionStart(hookio.Input{SessionID: "s1"}, cfg, "", state)
	coderTracker(hookio.Input{SessionID: "s1", Prompt: "/deadeye-coder sniper"}, cfg, state)

	out := decideCoderSessionStart(hookio.Input{SessionID: "s1"}, cfg, "", state)
	if out.Raw == nil || !strings.Contains(string(out.Raw), "level: sniper") {
		t.Errorf("compaction re-inject used the wrong level: %.80s", out.Raw)
	}
}

func TestCoderTrackerVerbs(t *testing.T) {
	state := coderTestState(t)
	cfg := config.Default()
	track := func(prompt string) string {
		return coderTracker(hookio.Input{SessionID: "s1", Prompt: prompt}, cfg, state)
	}

	if got := track("/deadeye-coder sniper"); !strings.Contains(got, "level: sniper") {
		t.Errorf("switch: %q", got)
	}
	if got := track("/deadeye-coder"); !strings.Contains(got, "ACTIVE — level: sniper") {
		t.Errorf("report after switch: %q", got)
	}
	if got := track("/deadeye-coder ultra"); !strings.Contains(got, "level: sniper") {
		t.Errorf("legacy alias: %q", got)
	}
	if got := track("/deadeye-coder blastoff"); !strings.Contains(got, "level: marksman") || !strings.Contains(got, "blastoff") {
		t.Errorf("garbage falls back to default with a note: %q", got)
	}
	if got := track("normal mode"); got != "DEADEYE CODER OFF" {
		t.Errorf("deactivation: %q", got)
	}
	if got := track("/deadeye-coder"); !strings.Contains(got, "OFF") {
		t.Errorf("report after off: %q", got)
	}
	if got := track("add a normal mode toggle"); got != "" {
		t.Errorf("substring deactivation must not fire: %q", got)
	}
	if got := track("/deadeye-review please"); !strings.Contains(got, "level: review") {
		t.Errorf("review switch: %q", got)
	}
	if got := track("/deadeye-coder default review"); !strings.Contains(got, "session-only") {
		t.Errorf("default review must be rejected: %q", got)
	}
	if got := track("fix the bug"); got != "" {
		t.Errorf("ordinary prompt produced a confirmation: %q", got)
	}
}

func TestCoderTrackerDefaultPersists(t *testing.T) {
	state := coderTestState(t)
	got := coderTracker(hookio.Input{SessionID: "s1", Prompt: "/deadeye-coder default spotter"}, config.Default(), state)
	if !strings.Contains(got, "new sessions start at spotter") {
		t.Fatalf("default confirmation: %q", got)
	}
	cfg := config.LoadFor("", nil)
	if cfg.Coder.DefaultLevel != "spotter" {
		t.Errorf("persisted default = %q, want spotter", cfg.Coder.DefaultLevel)
	}
}

// TestCoderTrackerConfirmationLeadsCombinedContext: the switch
// confirmation must be the FIRST part of the one UserPromptSubmit
// additionalContext slot, ahead of the once-per-session advisory.
func TestCoderTrackerConfirmationLeadsCombinedContext(t *testing.T) {
	state := coderTestState(t)
	out := decideUserPromptSubmit(hookio.Input{SessionID: "s1", Prompt: "/deadeye-coder sniper", Cwd: t.TempDir()}, config.Default(), state)
	if out.HookSpecificOutput == nil {
		t.Fatal("expected combined context")
	}
	ctx := out.HookSpecificOutput.AdditionalContext
	if !strings.HasPrefix(ctx, "DEADEYE CODER CHANGED — level: sniper") {
		t.Errorf("confirmation must lead the combined context, got: %.80s", ctx)
	}
	if !strings.Contains(ctx, "deadeye guidance for this session") {
		t.Error("once-per-session advisory missing from the same combined slot")
	}
}

func TestCoderTrackerIgnoresSyntheticPrompts(t *testing.T) {
	state := coderTestState(t)
	synthetic := "<task-notification>/deadeye-coder sniper</task-notification>"
	out := decideUserPromptSubmit(hookio.Input{SessionID: "s9", Prompt: synthetic, Cwd: t.TempDir()}, config.Default(), state)
	if out.HookSpecificOutput != nil && strings.Contains(out.HookSpecificOutput.AdditionalContext, "CODER CHANGED") {
		t.Error("tracker fired on a synthetic prompt")
	}
	if state.coderLevelFor("s9") == "sniper" {
		t.Error("synthetic prompt switched the session level")
	}
}

func TestCoderSubagentInjection(t *testing.T) {
	state := coderTestState(t)
	cfg := config.Default()
	in := hookio.Input{SessionID: "s1", AgentType: "Explore"}

	if got := coderSubagentText(in, cfg, state); !strings.Contains(got, "level: marksman") {
		t.Errorf("default-level subagent injection missing: %.60q", got)
	}

	cfg.Coder.SubagentMatcher = "^general" // doesn't match Explore
	if got := coderSubagentText(in, cfg, state); got != "" {
		t.Error("matcher miss still injected")
	}

	cfg.Coder.SubagentMatcher = "explore" // case-insensitive hit
	if got := coderSubagentText(in, cfg, state); got == "" {
		t.Error("matcher hit did not inject")
	}

	cfg.Coder.SubagentMatcher = "(broken[" // fails to compile -> fail open
	if got := coderSubagentText(in, cfg, state); got == "" {
		t.Error("broken matcher must fail open and inject")
	}

	cfg.Coder.SubagentMatcher = "^general"
	if got := coderSubagentText(hookio.Input{SessionID: "s1"}, cfg, state); got == "" {
		t.Error("missing agent_type must fail open and inject")
	}

	cfg = config.Default()
	cfg.Coder.Disabled = true
	if got := coderSubagentText(in, cfg, state); got != "" {
		t.Error("kill switch did not silence subagent injection")
	}
}

// TestCoderSubagentMergesWithBrevityNote: one SubagentStart emit carrying
// both parts -- never two responses, never a dropped part.
func TestCoderSubagentMergesWithBrevityNote(t *testing.T) {
	state := coderTestState(t)
	out := decideSubagentStart(hookio.Input{SessionID: "s1", AgentType: "general-purpose"}, config.Default(), state)
	if out.HookSpecificOutput == nil {
		t.Fatal("expected a merged SubagentStart emit")
	}
	ctx := out.HookSpecificOutput.AdditionalContext
	if !strings.Contains(ctx, "terse, structured results") {
		t.Error("brevity note missing from merged emit")
	}
	if !strings.Contains(ctx, "DEADEYE CODER ACTIVE") {
		t.Error("coder persona missing from merged emit")
	}
}

func TestCoderActionsAreLogged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	logPath := filepath.Join(t.TempDir(), "decisions.jsonl")
	state := newDaemonState(catalog.Catalog{}, logstore.Open(logPath))
	cfg := config.Default()

	decideCoderSessionStart(hookio.Input{SessionID: "s1"}, cfg, "", state)
	coderTracker(hookio.Input{SessionID: "s1", Prompt: "/deadeye-coder sniper"}, cfg, state)
	coderTracker(hookio.Input{SessionID: "s1", Prompt: "normal mode"}, cfg, state)
	coderSubagentText(hookio.Input{SessionID: "s2"}, cfg, state)

	records, err := logstore.Scan(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"coder-inject": false, "coder-switch": false, "coder-off": false, "coder-subagent": false}
	for _, r := range records {
		if _, ok := want[r.Action]; ok {
			want[r.Action] = true
		}
	}
	for action, seen := range want {
		if !seen {
			t.Errorf("action %q never logged", action)
		}
	}
}

func readFileTrim(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
