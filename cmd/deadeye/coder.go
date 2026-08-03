package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"

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

// writeCoderModeFile mirrors the active level to ~/.deadeye/coder-mode for
// the statusline script -- display only, never authoritative.
// deadeye: one global mode file across concurrent sessions, last writer
// wins -- per-session badges need Claude Code to pass session_id to
// statusline commands.
func writeCoderModeFile(level string) {
	if level == coder.LevelOff || level == "" {
		os.Remove(meta.CoderModePath())
		return
	}
	_ = os.MkdirAll(meta.StateDir(), 0o700)
	_ = os.WriteFile(meta.CoderModePath(), []byte(level+"\n"), 0o600)
}

// decideCoderSessionStart delivers the persona at every session start --
// including source: compact, so the ruleset survives compaction (raw
// stdout delivery, docs/verified.md §11).
func decideCoderSessionStart(in hookio.Input, cfg config.Config, pluginRoot string, state *daemonState) hookio.Output {
	level := effectiveCoderLevel(in.SessionID, cfg, state)
	if !coder.Active(level) {
		writeCoderModeFile(coder.LevelOff)
		state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "SessionStart", Action: "noop"})
		return hookio.Empty()
	}

	state.setCoderLevel(in.SessionID, level)
	writeCoderModeFile(level)

	text := coder.Instructions(level)
	reason := "coder ruleset injection"
	if inject.EstimateTokens(text) > cfg.Coder.InjectionBudgetTokens {
		reason = "coder ruleset injection (over budget, shipped anyway -- trim before adding more)"
	}
	if nudge := statuslineNudge(pluginRoot, state, in.SessionID); nudge != "" {
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
func statuslineNudge(pluginRoot string, state *daemonState, sessionID string) string {
	if pluginRoot == "" {
		return ""
	}
	if _, err := os.Stat(meta.StatuslineNudgedPath()); err == nil {
		return "" // already nudged, ever
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	// deadeye: assumes ~/.claude -- CLAUDE_CONFIG_DIR overrides aren't
	// visible to the daemon (env is client-side only); support it by
	// carrying the value over the wire if anyone actually hits this.
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	b, err := os.ReadFile(settingsPath)
	if err == nil {
		var settings map[string]any
		if json.Unmarshal(b, &settings) == nil {
			if _, has := settings["statusLine"]; has {
				return "" // user already has a statusline; not ours to suggest over
			}
		}
	}

	_ = os.WriteFile(meta.StatuslineNudgedPath(), []byte("1\n"), 0o600)
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: sessionID, Surface: "SessionStart", Action: "statusline-nudge"})

	script := filepath.Join(pluginRoot, "hooks", "deadeye-statusline.sh")
	if !coder.IsShellSafe(script) {
		return "deadeye: a statusline badge showing the coder level is available at hooks/deadeye-statusline.sh inside the deadeye plugin directory. OFFER the user (once) to add it as their statusLine command in ~/.claude/settings.json -- never edit their settings without asking."
	}
	return "deadeye: a statusline badge showing the coder level is available. OFFER the user (once) to add this to ~/.claude/settings.json -- never edit their settings without asking:\n  \"statusLine\": { \"type\": \"command\", \"command\": \"bash \\\"" + script + "\\\"\" }"
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
		writeCoderModeFile(cmd.Level)
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
		writeCoderModeFile(level)
		log("coder-switch", level+" (unrecognized: "+cmd.Raw+")")
		return "DEADEYE CODER CHANGED — level: " + level + " (didn't recognize \"" + cmd.Raw + "\")"
	case coder.KindOff:
		state.setCoderLevel(in.SessionID, coder.LevelOff)
		writeCoderModeFile(coder.LevelOff)
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

// coderSubagentText returns the persona for a spawning subagent, or "".
// The optional matcher scopes injection by agent type; a regex that fails
// to compile fails OPEN (injects) -- a broken config must never silently
// strip the persona (ported behavior).
func coderSubagentText(in hookio.Input, cfg config.Config, state *daemonState) string {
	level := effectiveCoderLevel(in.SessionID, cfg, state)
	if !coder.Active(level) {
		return ""
	}
	if m := cfg.Coder.SubagentMatcher; m != "" && in.AgentType != "" {
		re, err := regexp.Compile("(?i)" + m)
		if err == nil && !re.MatchString(in.AgentType) {
			return ""
		}
	}
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "SubagentStart", Action: "coder-subagent", Reason: level})
	return coder.Instructions(level)
}
