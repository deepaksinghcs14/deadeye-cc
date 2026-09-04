package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// codexHookScript is the adapter written to ~/.deadeye/hooks/. A canary
// test keeps it byte-identical to hooks/deadeye-codex-hook.sh.
const codexHookScript = `#!/usr/bin/env bash
# deadeye hook adapter for Codex CLI. Installed by ` + "`deadeye init codex`" + `
# to ~/.deadeye/hooks/ and referenced from ~/.codex/hooks.json. Same
# contract as the Claude Code adapter minus the plugin bootstrap: Codex
# installs have no marketplace, so the binary that ran ` + "`init codex`" + ` is
# the binary; updates are manual.
set -u
EVENT="${1:-}"

BIN="$(command -v deadeye 2>/dev/null || true)"
if [ -z "$BIN" ] || [ ! -x "$BIN" ]; then
  BIN="$HOME/.deadeye/bin/deadeye"
fi
if [ ! -x "$BIN" ]; then
  cat > /dev/null 2>&1 || true
  printf '{}'
  exit 0
fi

# Capture rather than exec: if the binary dies without output, Codex
# still gets valid JSON (fail open, INV-5).
out="$("$BIN" hook "$EVENT" --host codex 2>/dev/null)" || true
[ -n "$out" ] || out="{}"
printf '%s' "$out"
`

// codexEvents is what init codex registers. SessionEnd is included even
// though verified.md §12 saw it never fire under `codex exec` -- harmless
// if silent, useful if interactive sessions emit it. PermissionRequest and
// SubagentStop are deliberately not registered: deadeye has no useful
// Codex-specific response for them yet.
var codexEvents = []struct{ event, matcher string }{
	{"SessionStart", ""},
	{"SubagentStart", ""},
	{"UserPromptSubmit", ""},
	{"PreToolUse", "^(Bash|apply_patch|Edit|Write|Read|Grep|WebFetch)$"},
	{"PostToolUse", "^(Bash|apply_patch|Edit|Write|Read|Grep|Glob|WebFetch|WebSearch|mcp__.*)$"},
	{"PostCompact", ""},
	{"Stop", ""},
	{"SessionEnd", ""},
}

func codexHomeDir() (string, error) {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

func codexHooksPath() (string, error) {
	dir, err := codexHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "hooks.json"), nil
}

func codexScriptPath() string {
	return filepath.Join(meta.StateDir(), "hooks", "deadeye-codex-hook.sh")
}

func writeCodexAdapter(script string) error {
	if err := os.MkdirAll(filepath.Dir(script), 0o700); err != nil {
		return err
	}
	return os.WriteFile(script, []byte(codexHookScript), 0o755)
}

// runInit dispatches `deadeye init <host>`. codex is a full hook adapter
// (below); cursor/windsurf are static rules files (initrules.go).
func runInit(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, initUsage)
		os.Exit(2)
	}
	switch args[0] {
	case "codex":
		// falls through to the codex body below
	case "cursor", "windsurf":
		runInitRules(args[0], args[1:])
		return
	case "gemini":
		runInitGemini(args[1:])
		return
	default:
		fmt.Fprintln(os.Stderr, initUsage)
		os.Exit(2)
	}
	assumeYes := false
	for _, a := range args[1:] {
		if a == "--yes" || a == "-y" {
			assumeYes = true
		}
	}

	hooksPath, err := codexHooksPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "deadeye init codex:", err)
		os.Exit(1)
	}
	if _, err := os.Stat(filepath.Dir(hooksPath)); err != nil {
		fmt.Fprintln(os.Stderr, "deadeye init codex:", filepath.Dir(hooksPath), "not found -- is Codex CLI installed?")
		os.Exit(1)
	}

	// Build the proposed hooks.json: existing content preserved, our
	// entries appended per event (skipped where already present).
	raw := map[string]any{}
	if b, err := os.ReadFile(hooksPath); err == nil {
		if json.Unmarshal(b, &raw) != nil {
			fmt.Fprintln(os.Stderr, "deadeye init codex: existing", hooksPath, "is not valid JSON -- fix or remove it first; refusing to rewrite it")
			os.Exit(1)
		}
	}
	hooks, _ := raw["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	script := codexScriptPath()
	changed := []string{}
	for _, e := range codexEvents {
		entries, _ := hooks[e.event].([]any)
		found := false
		updated := false
		for _, entry := range entries {
			entryFound, entryUpdated := normalizeCodexDeadeyeEntry(entry, script, e.event, e.matcher)
			found = found || entryFound
			updated = updated || entryUpdated
		}
		if found {
			if updated {
				changed = append(changed, e.event)
			}
			continue
		}
		entry := map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": script + " " + e.event}},
		}
		if e.matcher != "" {
			entry["matcher"] = e.matcher
		}
		hooks[e.event] = append(entries, any(entry))
		changed = append(changed, e.event)
	}
	raw["hooks"] = hooks

	if len(changed) == 0 {
		fmt.Println(cHead("deadeye init codex") + cDim("  (experimental -- Codex hooks are an experimental Codex feature)"))
		fmt.Println()
		fmt.Println("deadeye is already registered in", hooksPath)
		fmt.Println("Will refresh the hook adapter at " + cValue(script))
		fmt.Println("Will install/update the Codex PR-review skill.")
		fmt.Println()
		if flag := codexFeatureFlagHint(); flag != "" {
			fmt.Println(cWarn(flag))
			fmt.Println()
		}
		if !assumeYes {
			fmt.Print("Apply? [y/N] ")
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if l := strings.ToLower(strings.TrimSpace(line)); l != "y" && l != "yes" {
				fmt.Println("Nothing written.")
				return
			}
		}
		if err := writeCodexAdapter(script); err != nil {
			fmt.Fprintln(os.Stderr, "deadeye init codex:", err)
			os.Exit(1)
		}
		for _, cmd := range hostCmds {
			installCommand(cmd, "codex")
		}
		fmt.Println(cDim("Remove any time with: deadeye uninstall codex"))
		return
	}

	proposed, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "deadeye init codex:", err)
		os.Exit(1)
	}

	fmt.Println(cHead("deadeye init codex") + cDim("  (experimental -- Codex hooks are an experimental Codex feature)"))
	fmt.Println()
	fmt.Println("Will write the hook adapter to " + cValue(script))
	fmt.Println("Will update " + cValue(hooksPath) + " to:")
	fmt.Println()
	fmt.Println(string(proposed))
	fmt.Println()
	if flag := codexFeatureFlagHint(); flag != "" {
		fmt.Println(cWarn(flag))
		fmt.Println()
	}

	if !assumeYes {
		fmt.Print("Apply? [y/N] ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if l := strings.ToLower(strings.TrimSpace(line)); l != "y" && l != "yes" {
			fmt.Println("Nothing written.")
			return
		}
	}

	if err := writeCodexAdapter(script); err != nil {
		fmt.Fprintln(os.Stderr, "deadeye init codex:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(hooksPath, append(proposed, '\n'), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "deadeye init codex:", err)
		os.Exit(1)
	}
	fmt.Println(cGood("Registered/updated") + " deadeye for events: " + strings.Join(changed, ", "))
	fmt.Println(cDim("Codex will ask you to trust these hooks on first run -- that prompt is Codex's, not deadeye's."))
	for _, cmd := range hostCmds {
		installCommand(cmd, "codex")
	}
	fmt.Println(cDim("Remove any time with: deadeye uninstall codex"))
}

// hasDeadeyeEntry reports whether one of entries already invokes our
// script (matched by path prefix, so event-name args don't matter).
func hasDeadeyeEntry(entries []any, script string) bool {
	for _, e := range entries {
		em, _ := e.(map[string]any)
		inner, _ := em["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); strings.HasPrefix(cmd, script) {
				return true
			}
		}
	}
	return false
}

func normalizeCodexDeadeyeEntry(entry any, script, event, matcher string) (found bool, changed bool) {
	em, _ := entry.(map[string]any)
	if em == nil {
		return false, false
	}
	inner, _ := em["hooks"].([]any)
	for _, h := range inner {
		hm, _ := h.(map[string]any)
		cmd, _ := hm["command"].(string)
		if !strings.HasPrefix(cmd, script) {
			continue
		}
		found = true
		want := script + " " + event
		if cmd != want {
			hm["command"] = want
			changed = true
		}
		if hm["type"] != "command" {
			hm["type"] = "command"
			changed = true
		}
	}
	if !found {
		return false, false
	}
	if matcher == "" {
		if _, ok := em["matcher"]; ok {
			delete(em, "matcher")
			changed = true
		}
	} else if em["matcher"] != matcher {
		em["matcher"] = matcher
		changed = true
	}
	return true, changed
}

// codexFeatureFlagHint returns a reminder when Codex's config.toml does
// not visibly enable the hooks feature. Advisory only -- deadeye never
// edits Codex's config.toml (TOML surgery on someone else's config is a
// good way to break their tool; the one-line instruction is safer).
func codexFeatureFlagHint() string {
	dir, err := codexHomeDir()
	if err != nil {
		return ""
	}
	configPath := filepath.Join(dir, "config.toml")
	b, err := os.ReadFile(configPath)
	if err == nil && strings.Contains(string(b), "hooks = true") {
		return ""
	}
	return "Codex hooks may be gated behind a feature flag on older builds. If hooks do not run, add to " + configPath + ":\n  [features]\n  hooks = true"
}

// runUninstallCodex backs `deadeye uninstall codex`: removes exactly the
// entries init codex added (matched by our script path), preserving
// everything else in the file.
func runUninstallCodex() {
	hooksPath, err := codexHooksPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "deadeye uninstall codex:", err)
		os.Exit(1)
	}
	b, err := os.ReadFile(hooksPath)
	if err != nil {
		removeCodexSidecars()
		fmt.Println("No", hooksPath, "-- nothing registered.")
		return
	}
	raw := map[string]any{}
	if json.Unmarshal(b, &raw) != nil {
		fmt.Fprintln(os.Stderr, "deadeye uninstall codex:", hooksPath, "is not valid JSON -- refusing to touch it")
		os.Exit(1)
	}
	hooks, _ := raw["hooks"].(map[string]any)
	script := codexScriptPath()
	removed := 0
	for event, v := range hooks {
		entries, _ := v.([]any)
		kept := entries[:0]
		for _, e := range entries {
			em, _ := e.(map[string]any)
			inner, _ := em["hooks"].([]any)
			ours := false
			for _, h := range inner {
				hm, _ := h.(map[string]any)
				if cmd, _ := hm["command"].(string); strings.HasPrefix(cmd, script) {
					ours = true
				}
			}
			if ours {
				removed++
			} else {
				kept = append(kept, e)
			}
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	if removed == 0 {
		removeCodexSidecars()
		fmt.Println("No deadeye entries in", hooksPath, "-- nothing to do.")
		return
	}
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "deadeye uninstall codex:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(hooksPath, append(out, '\n'), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "deadeye uninstall codex:", err)
		os.Exit(1)
	}
	removeCodexSidecars()
	fmt.Printf("Removed %d deadeye hook entr%s from %s; adapter script deleted.\n", removed, map[bool]string{true: "y", false: "ies"}[removed == 1], hooksPath)
}

func removeCodexSidecars() {
	os.Remove(codexScriptPath())
	for _, cmd := range hostCmds {
		removeCommand(cmd, "codex")
	}
}

// codexRegistered reports whether any of our entries exist in codex's
// hooks.json -- status display only.
func codexRegistered() bool {
	hooksPath, err := codexHooksPath()
	if err != nil {
		return false
	}
	b, err := os.ReadFile(hooksPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(b), "deadeye-codex-hook.sh")
}
