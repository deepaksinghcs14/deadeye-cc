package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	out := decideCoderSessionStart(hookio.Input{SessionID: "s1"}, config.Default(), "", "", state)
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
	out := decideCoderSessionStart(hookio.Input{SessionID: "s1"}, cfg, "", "", state)
	if out.Raw != nil || out.HookSpecificOutput != nil {
		t.Errorf("expected silence with default off, got %+v", out)
	}
}

func TestCoderSessionStartKillSwitchBeatsSessionLevel(t *testing.T) {
	state := coderTestState(t)
	state.setCoderLevel("s1", "sniper") // an already-active session choice
	cfg := config.Default()
	cfg.Coder.Disabled = true // DEADEYE_CODER=off arrived on this request
	out := decideCoderSessionStart(hookio.Input{SessionID: "s1"}, cfg, "", "", state)
	if out.Raw != nil {
		t.Error("kill switch must silence an already-active session level")
	}
}

// TestCoderSessionStartReinjectsSwitchedLevel: a compact-source
// SessionStart mid-session must re-inject at the level the session
// SWITCHED to, not the config default -- this is the one behavior where
// daemon-side session state beats a global flag file.
func TestCoderSessionStartReinjectsSwitchedLevel(t *testing.T) {
	state := coderTestState(t)
	cfg := config.Default()
	decideCoderSessionStart(hookio.Input{SessionID: "s1"}, cfg, "", "", state)
	coderTracker(hookio.Input{SessionID: "s1", Prompt: "/deadeye-coder sniper"}, cfg, state)

	out := decideCoderSessionStart(hookio.Input{SessionID: "s1"}, cfg, "", "", state)
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
	out := decideUserPromptSubmit(hookio.Input{SessionID: "s1", Prompt: "/deadeye-coder sniper", Cwd: t.TempDir()}, config.Default(), "", state)
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
	out := decideUserPromptSubmit(hookio.Input{SessionID: "s9", Prompt: synthetic, Cwd: t.TempDir()}, config.Default(), "", state)
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

	decideCoderSessionStart(hookio.Input{SessionID: "s1"}, cfg, "", "", state)
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

func TestCoderModeFilePerSession(t *testing.T) {
	state := coderTestState(t)
	decideCoderSessionStart(hookio.Input{SessionID: "s1"}, config.Default(), "", "", state)
	if b, err := readFileTrim(meta.CoderModePathFor("s1")); err != nil || b != "marksman" {
		t.Errorf("per-session mode file = %q, %v; want marksman", b, err)
	}
	// Two concurrent sessions each keep their own badge.
	state.setCoderLevel("s2", "sniper")
	decideCoderSessionStart(hookio.Input{SessionID: "s2"}, config.Default(), "", "", state)
	if b, _ := readFileTrim(meta.CoderModePathFor("s1")); b != "marksman" {
		t.Errorf("s1 badge clobbered by s2: %q", b)
	}
	if b, _ := readFileTrim(meta.CoderModePathFor("s2")); b != "sniper" {
		t.Errorf("s2 badge = %q; want sniper", b)
	}
	// SessionEnd removes only that session's file.
	decideSessionEnd(hookio.Input{SessionID: "s1", Cwd: t.TempDir()}, state)
	if _, err := os.Stat(meta.CoderModePathFor("s1")); err == nil {
		t.Error("s1 per-session mode file should be gone after SessionEnd")
	}
	if _, err := os.Stat(meta.CoderModePathFor("s2")); err != nil {
		t.Error("s2 per-session mode file must survive s1's SessionEnd")
	}
}

func TestStatuslineNudgeHonorsConfigDir(t *testing.T) {
	state := coderTestState(t)
	confDir := t.TempDir()
	// A statusLine already configured in the CLAUDE_CONFIG_DIR settings must
	// suppress the nudge -- proving the override path is the one consulted.
	os.WriteFile(filepath.Join(confDir, "settings.json"), []byte(`{"statusLine":{"type":"command","command":"x"}}`), 0o600)
	if n := statuslineNudge("/plugin/root", confDir, state, "s1"); n != "" {
		t.Errorf("nudge fired despite existing statusLine in CLAUDE_CONFIG_DIR: %q", n)
	}
	// And with an empty config dir there, the nudge names that dir's path.
	confDir2 := t.TempDir()
	if n := statuslineNudge("/plugin/root", confDir2, state, "s1"); !strings.Contains(n, confDir2) {
		t.Errorf("nudge should name the override settings path, got: %q", n)
	}
}

func TestNativeRestoreSkipsSessionMemory(t *testing.T) {
	state := coderTestState(t)
	cwd := t.TempDir()
	// A summary that WOULD be injected on a cold start.
	dir := filepath.Join(os.Getenv("HOME"), ".deadeye", "sessions")
	os.MkdirAll(dir, 0o700)
	old := filepath.Join(dir, filepath.Base(cwd)+"_1.md")
	os.WriteFile(old, []byte("# Session summary: x\nbranch: main\n"), 0o600)
	oldTime := time.Now().Add(-time.Hour)
	os.Chtimes(old, oldTime, oldTime) // beat the freshness guard

	decideCoderSessionStart(hookio.Input{SessionID: "s1", Source: "resume"}, config.Default(), "", "", state)
	out := decideUserPromptSubmit(hookio.Input{SessionID: "s1", Prompt: "hello", Cwd: cwd}, config.Default(), "", state)
	if strings.Contains(out.HookSpecificOutput.AdditionalContext, "Session summary") {
		t.Error("resumed session must not re-inject the session-memory summary")
	}

	// Control: a startup session in the same cwd does get it.
	decideCoderSessionStart(hookio.Input{SessionID: "s2", Source: "startup"}, config.Default(), "", "", state)
	out2 := decideUserPromptSubmit(hookio.Input{SessionID: "s2", Prompt: "hello", Cwd: cwd}, config.Default(), "", state)
	if !strings.Contains(out2.HookSpecificOutput.AdditionalContext, "Session summary") {
		t.Error("startup session should still inject the session-memory summary")
	}
}

// TestNoopsAreNotLogged: quiet requests must not write decision-log rows --
// noops were 95% of the log and carried no reporting value.
func TestNoopsAreNotLogged(t *testing.T) {
	state := coderTestState(t)
	logPath := filepath.Join(t.TempDir(), "d.jsonl")
	state.logs = logstore.Open(logPath)

	cfg := config.Default()
	cfg.Coder.DefaultLevel = "off"
	decideCoderSessionStart(hookio.Input{SessionID: "s1"}, cfg, "", "", state)
	decideStop(hookio.Input{SessionID: "s1"}, state)
	decidePreToolUse(hookio.Input{SessionID: "s1", ToolName: "Glob"}, cfg, state)
	decideSessionEnd(hookio.Input{SessionID: "s1", Cwd: t.TempDir()}, state)
	if _, err := os.Stat(logPath); err == nil {
		b, _ := os.ReadFile(logPath)
		if len(b) > 0 {
			t.Errorf("noop-only traffic wrote log rows:\n%s", b)
		}
	}
}

// TestSubagentGetsCardNotFullRuleset guards the ~90% per-spawn cut.
func TestSubagentGetsCardNotFullRuleset(t *testing.T) {
	state := coderTestState(t)
	text := coderSubagentText(hookio.Input{SessionID: "s1", AgentType: "general-purpose"}, config.Default(), state)
	if text == "" || len(text) > 1100 {
		t.Fatalf("expected the condensed card, got %d bytes", len(text))
	}
	if strings.Contains(text, "## Intensity") {
		t.Error("subagent received full-ruleset sections")
	}
}

// TestGrepAdviceFiresOnceForUnboundedContentMode covers batch 3's Grep
// advisory: content mode with no head_limit gets one nudge per session;
// bounded shapes stay silent.
func TestGrepAdviceFiresOnceForUnboundedContentMode(t *testing.T) {
	state := coderTestState(t)
	cfg := config.Default()
	unbounded := hookio.Input{SessionID: "s1", ToolName: "Grep", ToolInput: []byte(`{"pattern":"x","output_mode":"content"}`)}
	out := decidePreToolUse(unbounded, cfg, state)
	if out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "head_limit") {
		t.Fatal("expected the grep-limit advisory on unbounded content mode")
	}
	if second := decidePreToolUse(unbounded, cfg, state); second.HookSpecificOutput != nil {
		t.Error("grep advisory must fire once per session, not nag")
	}
	bounded := hookio.Input{SessionID: "s2", ToolName: "Grep", ToolInput: []byte(`{"pattern":"x","output_mode":"content","head_limit":50}`)}
	if out := decidePreToolUse(bounded, cfg, state); out.HookSpecificOutput != nil {
		t.Error("bounded grep must stay silent")
	}
	files := hookio.Input{SessionID: "s3", ToolName: "Grep", ToolInput: []byte(`{"pattern":"x","output_mode":"files_with_matches"}`)}
	if out := decidePreToolUse(files, cfg, state); out.HookSpecificOutput != nil {
		t.Error("files_with_matches is bounded -- no advisory")
	}
}

// TestPostToolUseObservesOnlyLargeResponses: Read/Grep/etc responses build
// the evidence base, but only past the threshold -- small ones would just
// re-create the noop bloat 0.7.0 removed.
func TestPostToolUseObservesOnlyLargeResponses(t *testing.T) {
	state := coderTestState(t)
	logPath := filepath.Join(t.TempDir(), "d.jsonl")
	state.logs = logstore.Open(logPath)

	small := hookio.Input{SessionID: "s1", ToolName: "Read", ToolResponse: []byte(strings.Repeat("a", 100))}
	decidePostToolUse(small, config.Default(), state)
	big := hookio.Input{SessionID: "s1", ToolName: "Grep", ToolResponse: []byte(strings.Repeat("a", observeThresholdBytes+1))}
	decidePostToolUse(big, config.Default(), state)

	b, _ := os.ReadFile(logPath)
	if got := strings.Count(string(b), `"observed"`); got != 1 {
		t.Errorf("want exactly 1 observed row (the large Grep), got %d:\n%s", got, b)
	}
	if !strings.Contains(string(b), `"Grep"`) && !strings.Contains(string(b), "Grep") {
		t.Errorf("observed row should name the tool:\n%s", b)
	}
}

// TestMuteSilencesAdvisoriesNotRewrites: /deadeye-mute stands down every
// nag surface for the session while silent rewrites keep working.
func TestMuteSilencesAdvisoriesNotRewrites(t *testing.T) {
	state := coderTestState(t)
	cfg := config.Default()

	out := decideUserPromptSubmit(hookio.Input{SessionID: "s1", Prompt: "/deadeye-mute", Cwd: t.TempDir()}, cfg, "", state)
	if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "DEADEYE MUTED") {
		t.Fatal("mute confirmation missing")
	}

	// Grep advisory: silent while muted.
	grep := hookio.Input{SessionID: "s1", ToolName: "Grep", ToolInput: []byte(`{"pattern":"x","output_mode":"content"}`)}
	if out := decidePreToolUse(grep, cfg, state); out.HookSpecificOutput != nil {
		t.Error("grep advisory fired while muted")
	}
	// Bash advisory rule (bare git diff): silent while muted.
	diff := hookio.Input{SessionID: "s1", ToolName: "Bash", ToolInput: []byte(`{"command":"git diff"}`)}
	if out := decidePreToolUse(diff, cfg, state); out.HookSpecificOutput != nil {
		t.Error("bash advisory fired while muted")
	}
	// Rewrites still fire: they save tokens silently, muting must not stop them.
	test := hookio.Input{SessionID: "s1", ToolName: "Bash", ToolInput: []byte(`{"command":"go test ./..."}`)}
	if out := decidePreToolUse(test, cfg, state); out.HookSpecificOutput == nil || out.HookSpecificOutput.UpdatedInput == nil {
		t.Error("rewrite must keep working while muted")
	}

	// Unmute restores.
	out = decideUserPromptSubmit(hookio.Input{SessionID: "s1", Prompt: "/deadeye-mute off", Cwd: t.TempDir()}, cfg, "", state)
	if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "DEADEYE UNMUTED") {
		t.Fatal("unmute confirmation missing")
	}
	if out := decidePreToolUse(grep, cfg, state); out.HookSpecificOutput == nil {
		t.Error("grep advisory should fire again after unmute")
	}
}

// TestAdvisoryDisableSymmetry: the non-Bash advisories honor
// preprocess.disabled_rules like every Bash rule does.
func TestAdvisoryDisableSymmetry(t *testing.T) {
	state := coderTestState(t)
	cfg := config.Default()
	cfg.Preprocess.DisabledRules = []string{"grep-limit", "repeat-command"}

	grep := hookio.Input{SessionID: "s1", ToolName: "Grep", ToolInput: []byte(`{"pattern":"x","output_mode":"content"}`)}
	if out := decidePreToolUse(grep, cfg, state); out.HookSpecificOutput != nil {
		t.Error("disabled grep-limit still fired")
	}
	echoTwice := hookio.Input{SessionID: "s1", ToolName: "Bash", ToolInput: []byte(`{"command":"echo hi"}`)}
	decidePreToolUse(echoTwice, cfg, state)
	if out := decidePreToolUse(echoTwice, cfg, state); out.HookSpecificOutput != nil {
		t.Error("disabled repeat-command still fired")
	}
}

// TestObservationGatedByPreprocessSwitch: DEADEYE_PREPROCESS=off must
// silence the response-size observation too -- "independently switchable"
// includes the logging.
func TestObservationGatedByPreprocessSwitch(t *testing.T) {
	state := coderTestState(t)
	logPath := filepath.Join(t.TempDir(), "d.jsonl")
	state.logs = logstore.Open(logPath)
	cfg := config.Default()
	cfg.Mode.Preprocess = "off"
	big := hookio.Input{SessionID: "s1", ToolName: "Read", ToolResponse: []byte(strings.Repeat("a", observeThresholdBytes+1))}
	decidePostToolUse(big, cfg, state)
	if b, _ := os.ReadFile(logPath); len(b) > 0 {
		t.Errorf("observation logged despite preprocess off:\n%s", b)
	}
}

// TestStatuslineNudgeConcurrentClaim: N racing SessionStarts produce at
// most one nudge -- the O_EXCL create is the atomic claim.
func TestStatuslineNudgeConcurrentClaim(t *testing.T) {
	state := coderTestState(t)
	var wg sync.WaitGroup
	nudges := make(chan string, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if n := statuslineNudge("/plugin/root", "", state, "s"); n != "" {
				nudges <- n
			}
		}()
	}
	wg.Wait()
	close(nudges)
	count := 0
	for range nudges {
		count++
	}
	if count != 1 {
		t.Errorf("nudge fired %d times under concurrency; contract is once ever", count)
	}
}
