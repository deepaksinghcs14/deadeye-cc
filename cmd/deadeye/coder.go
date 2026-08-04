package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/coder"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/inject"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// effectiveCoderLevel resolves the level governing a request: kill switch
// wins, then an explicit session choice, then the config default. An
// invalid config default falls back to marksman (ported parity: garbage
// defaults resolve to the standard level, not to off).
func effectiveCoderLevel(sessionID string, cfg config.Config, state *daemonState) string {
	if cfg.Coder.Disabled {
		return coder.LevelOff
	}
	if s := state.coderLevelFor(sessionID); s != "" {
		return s
	}
	level, ok := coder.NormalizePersisted(cfg.Coder.DefaultLevel)
	if !ok {
		return coder.LevelMarksman
	}
	return level
}

// writeCoderModeFile mirrors the active level for the statusline script --
// display only, never authoritative. Per-session file first (the script
// resolves it via the session_id in its stdin JSON, so concurrent sessions
// each see their own badge); the global file stays as the fallback for
// statusline invocations whose stdin carries no session_id, where
// last-writer-wins is the best available answer.
func writeCoderModeFile(sessionID, level string) {
	paths := []string{meta.CoderModePath()}
	if sessionID != "" {
		paths = append(paths, meta.CoderModePathFor(sessionID))
	}
	if level == coder.LevelOff || level == "" {
		for _, p := range paths {
			os.Remove(p)
		}
		return
	}
	_ = os.MkdirAll(meta.StateDir(), 0o700)
	for _, p := range paths {
		_ = os.WriteFile(p, []byte(level+"\n"), 0o600)
	}
	sweepStaleModeFiles()
}

// sweepStaleModeFiles removes per-session mode files older than 48h --
// sessions killed without a SessionEnd (crash, SIGKILL) leak theirs, and
// this opportunistic sweep at write time is the only cleanup they get.
func sweepStaleModeFiles() {
	entries, err := os.ReadDir(meta.StateDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || len(name) <= len("coder-mode.") || name[:len("coder-mode.")] != "coder-mode." {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 48*time.Hour {
			os.Remove(filepath.Join(meta.StateDir(), name))
		}
	}
}

// decideCoderSessionStart delivers the persona at every session start --
// including source: compact, so the ruleset survives compaction (raw
// stdout delivery, docs/verified.md §11).
func decideCoderSessionStart(in hookio.Input, cfg config.Config, pluginRoot, configDir string, state *daemonState) hookio.Output {
	// A resume replays the full transcript and a compact carries Claude
	// Code's own summary -- either way the re-orientation tax deadeye's
	// session memory exists to cut is already paid, so the next-prompt
	// injection stands down on its memory paragraph (PLAN.md §5.7/§10.10).
	if in.Source == "resume" || in.Source == "compact" {
		state.markNativeRestore(in.SessionID)
	}

	level := effectiveCoderLevel(in.SessionID, cfg, state)
	if !coder.Active(level) {
		writeCoderModeFile(in.SessionID, coder.LevelOff)
		return hookio.Empty()
	}

	state.setCoderLevel(in.SessionID, level)
	writeCoderModeFile(in.SessionID, level)

	text := coder.Instructions(level)
	reason := "coder ruleset injection"
	if inject.EstimateTokens(text) > cfg.Coder.InjectionBudgetTokens {
		reason = "coder ruleset injection (over budget, shipped anyway -- trim before adding more)"
	}
	if nudge := statuslineNudge(pluginRoot, configDir, state, in.SessionID); nudge != "" {
		text += "\n\n" + nudge
	}

	state.log(logstore.Record{
		TS: nowRFC3339(), SessionID: in.SessionID, Surface: "SessionStart",
		Action: "coder-inject", Reason: reason, BytesAfter: len(text),
	})
	out := hookio.Empty()
	out.Raw = []byte(text)
	return out
}

// statuslineNudge returns, at most once ever, prose asking the AGENT to
// offer adding deadeye's statusline to the user's settings.json. deadeye
// itself never writes Claude's settings -- the flag file records that the
// offer was made, and the shell-safe guard keeps an exotic plugin path
// out of a suggested command.
func statuslineNudge(pluginRoot, configDir string, state *daemonState, sessionID string) string {
	if pluginRoot == "" {
		return ""
	}
	if _, err := os.Stat(meta.StatuslineNudgedPath()); err == nil {
		return "" // already nudged, ever
	}
	// configDir is the client's CLAUDE_CONFIG_DIR, carried over the wire
	// (the daemon never reads env); empty means the ~/.claude default.
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		configDir = filepath.Join(home, ".claude")
	}
	settingsPath := filepath.Join(configDir, "settings.json")
	b, err := os.ReadFile(settingsPath)
	if err == nil {
		var settings map[string]any
		if json.Unmarshal(b, &settings) == nil {
			if _, has := settings["statusLine"]; has {
				return "" // user already has a statusline; not ours to suggest over
			}
		}
	}

	// O_EXCL create is the atomic once-ever claim: two sessions starting
	// concurrently both pass the Stat above, but only one wins this create
	// -- a plain stat-then-write let both nudge.
	_ = os.MkdirAll(meta.StateDir(), 0o700)
	f, err := os.OpenFile(meta.StatuslineNudgedPath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "" // another session claimed the nudge first
	}
	_, _ = f.WriteString("1\n")
	f.Close()
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: sessionID, Surface: "SessionStart", Action: "statusline-nudge"})

	script := filepath.Join(pluginRoot, "hooks", "deadeye-statusline.sh")
	if !coder.IsShellSafe(script) {
		return "deadeye: a statusline badge showing the coder level is available at hooks/deadeye-statusline.sh inside the deadeye plugin directory. OFFER the user (once) to add it as their statusLine command in " + settingsPath + " -- never edit their settings without asking."
	}
	return "deadeye: a statusline badge showing the coder level is available. OFFER the user (once) to add this to " + settingsPath + " -- never edit their settings without asking:\n  \"statusLine\": { \"type\": \"command\", \"command\": \"bash \\\"" + script + "\\\"\" }"
}

// coderTracker handles /deadeye-coder invocations and deactivation
// phrases inside UserPromptSubmit. Returns the confirmation text ("" if
// the prompt wasn't a coder command).
func coderTracker(in hookio.Input, cfg config.Config, state *daemonState) string {
	if cfg.Coder.Disabled {
		return ""
	}
	cmd := coder.ParseCommand(in.Prompt)
	log := func(action, reason string) {
		state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "UserPromptSubmit", Action: action, Reason: reason})
	}

	switch cmd.Kind {
	case coder.KindSwitch, coder.KindReviewSwitch:
		state.setCoderLevel(in.SessionID, cmd.Level)
		writeCoderModeFile(in.SessionID, cmd.Level)
		log("coder-switch", cmd.Level)
		return "DEADEYE CODER CHANGED — level: " + cmd.Level
	case coder.KindSwitchBad:
		// Ported parity: an unrecognized level switches to the configured
		// DEFAULT rather than erroring out.
		level, ok := coder.NormalizePersisted(cfg.Coder.DefaultLevel)
		if !ok {
			level = coder.LevelMarksman
		}
		state.setCoderLevel(in.SessionID, level)
		writeCoderModeFile(in.SessionID, level)
		log("coder-switch", level+" (unrecognized: "+cmd.Raw+")")
		return "DEADEYE CODER CHANGED — level: " + level + " (didn't recognize \"" + cmd.Raw + "\")"
	case coder.KindOff:
		state.setCoderLevel(in.SessionID, coder.LevelOff)
		writeCoderModeFile(in.SessionID, coder.LevelOff)
		log("coder-off", "")
		return "DEADEYE CODER OFF"
	case coder.KindReport:
		level := effectiveCoderLevel(in.SessionID, cfg, state)
		log("coder-report", level)
		if !coder.Active(level) {
			return "DEADEYE CODER OFF — start with /deadeye-coder spotter|marksman|sniper"
		}
		return "DEADEYE CODER ACTIVE — level: " + level
	case coder.KindDefault:
		if err := config.WriteCoderDefault(cmd.Level); err != nil {
			log("coder-default", "write failed: "+err.Error())
			return "DEADEYE CODER — couldn't persist the default (" + err.Error() + ")"
		}
		log("coder-default", cmd.Level)
		return "DEADEYE CODER DEFAULT SET — new sessions start at " + cmd.Level + "."
	case coder.KindDefaultBad:
		log("coder-default", "rejected: "+cmd.Raw)
		return "DEADEYE CODER — valid defaults are off|spotter|marksman|sniper (review is session-only)."
	}
	return ""
}

// matcherCache holds the last compiled subagent matcher -- one pattern in
// practice (it comes from config), so a single entry beats a map.
var matcherCache struct {
	mu      sync.Mutex
	pattern string
	re      *regexp.Regexp
}

func compileMatcher(pattern string) (*regexp.Regexp, error) {
	matcherCache.mu.Lock()
	defer matcherCache.mu.Unlock()
	if matcherCache.pattern == pattern && matcherCache.re != nil {
		return matcherCache.re, nil
	}
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, err
	}
	matcherCache.pattern, matcherCache.re = pattern, re
	return re, nil
}

// coderSubagentText returns the persona card for a spawning subagent, or
// "". Subagents get coder.SubagentCard, not the full ruleset -- ~90%
// smaller per spawn with the behavior-bearing rules intact. The optional
// matcher scopes injection by agent type; a regex that fails to compile
// fails OPEN (injects) -- a broken config must never silently strip the
// persona (ported behavior).
func coderSubagentText(in hookio.Input, cfg config.Config, state *daemonState) string {
	level := effectiveCoderLevel(in.SessionID, cfg, state)
	if !coder.Active(level) {
		return ""
	}
	if m := cfg.Coder.SubagentMatcher; m != "" && in.AgentType != "" {
		re, err := compileMatcher(m)
		if err == nil && !re.MatchString(in.AgentType) {
			return ""
		}
	}
	card := coder.SubagentCard(level)
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "SubagentStart", Action: "coder-subagent", Reason: level, BytesAfter: len(card)})
	return card
}
