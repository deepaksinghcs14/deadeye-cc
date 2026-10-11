package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// cursorHookScript is the adapter Cursor's hooks invoke. Two things set it
// apart from the Codex and Gemini adapters:
//
//   - It passes Cursor's own event name through; the daemon translates.
//   - On the two permission hooks it never prints {}. Cursor blocks an
//     action whose permission-hook response doesn't match the schema, so
//     every failure path here (no binary, daemon down, empty output) answers
//     with an explicit allow instead -- deadeye failing must never stop the
//     user's tool call.
const cursorHookScript = `#!/usr/bin/env bash
# deadeye hook adapter for Cursor. Written by "deadeye init cursor".
set -u
EVENT="${1:-}"
case "$EVENT" in
  preToolUse|beforeReadFile) FALLBACK='{"permission":"allow"}' ;;
  *) FALLBACK='{}' ;;
esac

BIN="$(command -v deadeye 2>/dev/null || true)"
if [ -z "$BIN" ] || [ ! -x "$BIN" ]; then
  BIN="$HOME/.deadeye/bin/deadeye"
fi
if [ ! -x "$BIN" ]; then
  cat > /dev/null 2>&1 || true
  printf '%s' "$FALLBACK"
  exit 0
fi

out="$("$BIN" hook "$EVENT" --host cursor 2>/dev/null)" || true
case "$out" in ''|'{}') out="$FALLBACK" ;; esac
printf '%s' "$out"
`

// cursorHookEvents is what init cursor registers -- only events whose
// response Cursor delivers (see hookio.cursorEvents for the ones left out).
// Reads go through beforeReadFile, not preToolUse: that is the hook whose
// file path Cursor documents.
var cursorHookEvents = []struct {
	event, matcher string
	timeout        int
}{
	{"sessionStart", "", 10},
	{"sessionEnd", "", 5},
	{"preToolUse", "Shell", 5},
	{"beforeReadFile", "", 5},
	{"postToolUse", "Shell|MCP:.*", 5},
}

func cursorScriptPath() string {
	return filepath.Join(meta.StateDir(), "hooks", "deadeye-cursor-hook.sh")
}

// cursorHooksPath is Cursor's user-level hooks file. User-level, not
// project: the rules file init writes is per project, but the engine's
// hooks carry no project state and one registration serves every workspace.
func cursorHooksPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cursor", "hooks.json"), nil
}

// isDeadeyeCursorEntry reports whether one hooks.json entry is deadeye's,
// by its command -- the only thing that identifies it among the user's own.
func isDeadeyeCursorEntry(entry any, script string) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	cmd, _ := m["command"].(string)
	return strings.HasPrefix(cmd, script+" ")
}

// mergeCursorHooks returns doc with deadeye's entries present exactly once
// (register) or absent (!register), leaving every other entry and key as
// the user wrote it.
func mergeCursorHooks(doc map[string]any, script string, register bool) map[string]any {
	if doc == nil {
		doc = map[string]any{}
	}
	if _, ok := doc["version"]; !ok {
		doc["version"] = 1
	}
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for event, v := range hooks {
		entries, _ := v.([]any)
		kept := entries[:0:0]
		for _, e := range entries {
			if !isDeadeyeCursorEntry(e, script) {
				kept = append(kept, e)
			}
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	if register {
		for _, e := range cursorHookEvents {
			entry := map[string]any{"command": script + " " + e.event, "timeout": e.timeout}
			if e.matcher != "" {
				entry["matcher"] = e.matcher
			}
			existing, _ := hooks[e.event].([]any)
			hooks[e.event] = append(existing, entry)
		}
	}
	doc["hooks"] = hooks
	return doc
}

// rewriteCursorHooks applies mergeCursorHooks to the user's hooks file. A
// file that exists but isn't a JSON object is left alone and reported:
// overwriting it would silently drop hooks the user wrote.
func rewriteCursorHooks(register bool) (path string, err error) {
	path, err = cursorHooksPath()
	if err != nil {
		return path, err
	}
	var doc map[string]any
	b, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		if err := json.Unmarshal(b, &doc); err != nil {
			return path, fmt.Errorf("%s is not valid JSON (%v) -- not touching it", path, err)
		}
	case !os.IsNotExist(readErr):
		return path, readErr
	case !register:
		return path, nil // nothing to remove
	}
	out, err := json.MarshalIndent(mergeCursorHooks(doc, cursorScriptPath(), register), "", "  ")
	if err != nil {
		return path, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return path, err
	}
	tmp := path + ".deadeye-tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o644); err != nil {
		return path, err
	}
	return path, os.Rename(tmp, path)
}

// installCursorHooks writes the adapter and registers it. Called after the
// rules file is in place; a failure here leaves that file, which still
// works on its own.
func installCursorHooks() {
	script := cursorScriptPath()
	// 0700 for the state dir, same as every other writer -- the decision
	// log there carries prompt markers and credential paths.
	if err := os.MkdirAll(meta.StateDir(), 0o700); err == nil {
		err = os.MkdirAll(filepath.Dir(script), 0o700)
		if err == nil {
			err = os.WriteFile(script, []byte(cursorHookScript), 0o755)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "deadeye init cursor: hook adapter:", err)
			return
		}
	}
	path, err := rewriteCursorHooks(true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "deadeye init cursor: hooks:", err)
		return
	}
	fmt.Println(cGood("Registered") + " deadeye's hooks in " + path)
}

func uninstallCursorHooks() {
	path, err := rewriteCursorHooks(false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "deadeye uninstall cursor: hooks:", err)
		return
	}
	_ = os.Remove(cursorScriptPath())
	fmt.Println(cGood("Removed") + " deadeye's hooks from " + path)
}
