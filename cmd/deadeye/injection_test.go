package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/coder"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/inject"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
)

func sessionStart(t *testing.T, state *daemonState, source string) string {
	t.Helper()
	out := decideCoderSessionStart(hookio.Input{SessionID: "s1", HookEventName: "SessionStart", Source: source}, config.Default(), "", "", "", state)
	return string(out.Raw)
}

// A compact must restore the persona as a digest, not the 10 KB ruleset
// again (36 compact injections sampled; one session carried 9 copies).
// Replays startup + 3 compacts and bounds the total.
func TestCompactInjectsDigestNotTheWholeRuleset(t *testing.T) {
	state := coderTestState(t)
	startup := sessionStart(t, state, "startup")
	if len(startup) < 9000 {
		t.Fatalf("startup injection is %d bytes; the full ruleset is expected here", len(startup))
	}
	total := len(startup)
	for i := 0; i < 3; i++ {
		got := sessionStart(t, state, "compact")
		if len(got) > 800 {
			t.Errorf("compact #%d injected %d bytes, want <= 800 (digest ~600 + pointer)", i+1, len(got))
		}
		for _, want := range []string{"level: marksman", "deadeye:", "ceiling:", "upgrade:", "deadeye-coder skill"} {
			if !strings.Contains(got, want) {
				t.Errorf("compact injection lost %q", want)
			}
		}
		total += len(got)
	}
	t.Logf("startup + 3 compacts = %d bytes (was %d)", total, 4*len(startup))
	if total > len(startup)+3*800 {
		t.Errorf("3-compact session injected %d bytes", total)
	}
}

// The digest names the ladder rungs, so a ruleset edit that drops one is
// noticed here rather than silently diverging from what compacts restore.
func TestCompactDigestCoversTheLadder(t *testing.T) {
	for _, rung := range []string{"needs to exist", "already in this codebase", "stdlib", "native", "installed library", "one line", "root cause"} {
		if !strings.Contains(coder.CompactInstructions(coder.LevelMarksman), rung) {
			t.Errorf("compact digest dropped %q", rung)
		}
	}
	if !strings.Contains(coder.Instructions(coder.LevelMarksman), "root cause") {
		t.Error("ruleset no longer mentions root cause; revisit the digest")
	}
}

// The shipped persona fits the configured budget at every level, so the
// "over budget, shipped anyway" row is a real signal, not noise on every
// start (490 rows sampled).
func TestCoderBudgetCoversEveryLevel(t *testing.T) {
	budget := config.Default().Coder.InjectionBudgetTokens
	for _, l := range []string{coder.LevelSpotter, coder.LevelMarksman, coder.LevelSniper} {
		if n := inject.EstimateTokens(coder.Instructions(l)); n > budget {
			t.Errorf("%s persona is ~%d tokens, budget %d", l, n, budget)
		}
	}
}

func TestNormalStartDoesNotLogShippedAnyway(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	logPath := filepath.Join(t.TempDir(), "decisions.jsonl")
	state := newDaemonState(catalog.Catalog{}, logstore.Open(logPath))
	for _, src := range []string{"startup", "compact"} {
		decideCoderSessionStart(hookio.Input{SessionID: "s-" + src, Source: src}, config.Default(), "", "", "", state)
	}
	records, err := logstore.Scan(logPath)
	if err != nil || len(records) == 0 {
		t.Fatalf("no records: %v", err)
	}
	for _, r := range records {
		if strings.Contains(r.Reason, "shipped anyway") {
			t.Errorf("normal %s start logged %q", r.Surface, r.Reason)
		}
	}
}

// Session guidance must not ride a synthetic prompt, and the notice must
// not burn the once-per-session slot: the first real prompt still gets it.
func TestSyntheticPromptGetsNoGuidanceAndKeepsTheSlot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(catalog.Catalog{}, nil)
	cfg := config.Default()
	for _, p := range []string{
		`<teammate-message teammate_id="eng">done</teammate-message>`,
		"note: <task-notification><summary>done</summary></task-notification>",
		"This session is being continued from a previous conversation that ran out of context.",
	} {
		out := decideUserPromptSubmit(hookio.Input{SessionID: "s1", Prompt: p, Cwd: t.TempDir()}, cfg, "", "", state)
		if out.HookSpecificOutput != nil {
			t.Errorf("synthetic prompt %.40q got context: %.80q", p, out.HookSpecificOutput.AdditionalContext)
		}
	}
	out := decideUserPromptSubmit(hookio.Input{SessionID: "s1", Prompt: "what does this repo do", Cwd: t.TempDir()}, cfg, "", "", state)
	if out.HookSpecificOutput == nil || out.HookSpecificOutput.AdditionalContext == "" {
		t.Error("first real prompt did not get the session guidance")
	}
}

// The digest points at the deadeye-coder skill, which only the Claude Code
// plugin ships; other hosts must still get the full ruleset on a compact.
func TestCompactKeepsFullRulesetOnHostsWithoutTheSkill(t *testing.T) {
	for _, host := range []string{"codex", "gemini"} {
		state := coderTestState(t)
		out := decideCoderSessionStart(hookio.Input{SessionID: "s1", HookEventName: "SessionStart", Source: "compact"}, config.Default(), "", "", host, state)
		text := out.Raw
		if out.HookSpecificOutput != nil {
			text = []byte(out.HookSpecificOutput.AdditionalContext)
		}
		if len(text) < 9000 || strings.Contains(string(text), "deadeye-coder skill") {
			t.Errorf("%s compact injected %d bytes / pointer present; want the full ruleset", host, len(text))
		}
	}
}
