package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitAndUninstallCodex: init --yes merges our entries while
// preserving foreign ones; a second init is a no-op; uninstall removes
// exactly ours and leaves the foreign entry untouched.
func TestInitAndUninstallCodex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o700)
	foreign := `{"hooks":{"PreToolUse":[{"matcher":"^Bash$","hooks":[{"type":"command","command":"/opt/other/tool.sh"}]}]}}`
	hooksPath := filepath.Join(home, ".codex", "hooks.json")
	os.WriteFile(hooksPath, []byte(foreign), 0o600)

	runInit([]string{"codex", "--yes"})

	b, _ := os.ReadFile(hooksPath)
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("hooks.json invalid after init: %v", err)
	}
	hooks := raw["hooks"].(map[string]any)
	if !strings.Contains(string(b), "/opt/other/tool.sh") {
		t.Error("foreign entry lost")
	}
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostCompact", "Stop", "SessionEnd"} {
		if !hasDeadeyeEntry(hooks[ev].([]any), codexScriptPath()) {
			t.Errorf("event %s not registered", ev)
		}
	}
	if _, err := os.Stat(codexScriptPath()); err != nil {
		t.Error("adapter script not written")
	}
	if !strings.Contains(string(b), "--host codex") || true {
		// host flag lives inside the adapter script, not hooks.json
		sb, _ := os.ReadFile(codexScriptPath())
		if !strings.Contains(string(sb), "--host codex") {
			t.Error("adapter script missing --host codex")
		}
	}

	// Idempotent: second init adds nothing.
	before, _ := os.ReadFile(hooksPath)
	runInit([]string{"codex", "--yes"})
	after, _ := os.ReadFile(hooksPath)
	if string(before) != string(after) {
		t.Error("second init modified hooks.json")
	}

	// Uninstall removes only ours.
	runUninstallCodex()
	b, _ = os.ReadFile(hooksPath)
	if strings.Contains(string(b), "deadeye-codex-hook.sh") {
		t.Error("our entries survived uninstall")
	}
	if !strings.Contains(string(b), "/opt/other/tool.sh") {
		t.Error("uninstall removed the foreign entry")
	}
}
