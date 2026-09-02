package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitAndUninstallCodex: init --yes merges our entries while
// preserving foreign ones; a second init refreshes sidecars without touching
// hooks.json; uninstall removes exactly ours and leaves the foreign entry
// untouched.
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
	for _, ev := range []string{"SessionStart", "SubagentStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostCompact", "Stop", "SessionEnd"} {
		if !hasDeadeyeEntry(hooks[ev].([]any), codexScriptPath()) {
			t.Errorf("event %s not registered", ev)
		}
	}
	if !strings.Contains(string(b), "Edit|Write|Read|Grep|WebFetch") {
		t.Error("PreToolUse matcher missing Codex-supported tool aliases")
	}
	if _, err := os.Stat(codexScriptPath()); err != nil {
		t.Error("adapter script not written")
	}
	sb, _ := os.ReadFile(codexScriptPath())
	if !strings.Contains(string(sb), "--host codex") {
		t.Error("adapter script missing --host codex")
	}
	skillPath, _ := prCommandPath("codex", "", home)
	skill, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("codex PR-review skill not written: %v", err)
	}
	if !strings.Contains(string(skill), "name: deadeye-pr") || strings.Contains(string(skill), "$ARGUMENTS") {
		t.Error("codex PR-review install is not a Codex skill")
	}

	// Idempotent: second init adds no hook entries, but refreshes sidecars.
	before, _ := os.ReadFile(hooksPath)
	os.Remove(codexScriptPath())
	os.Remove(skillPath)
	runInit([]string{"codex", "--yes"})
	after, _ := os.ReadFile(hooksPath)
	if string(before) != string(after) {
		t.Error("second init modified hooks.json")
	}
	if _, err := os.Stat(codexScriptPath()); err != nil {
		t.Error("second init did not refresh adapter script")
	}
	if _, err := os.Stat(skillPath); err != nil {
		t.Error("second init did not refresh PR-review skill")
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
	if _, err := os.Stat(codexScriptPath()); !os.IsNotExist(err) {
		t.Error("uninstall left adapter script")
	}
	if _, err := os.Stat(skillPath); !os.IsNotExist(err) {
		t.Error("uninstall left PR-review skill")
	}
}

func TestInitCodexHonorsCODEXHOME(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	codexHome := filepath.Join(t.TempDir(), "codex")
	t.Setenv("CODEX_HOME", codexHome)
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}

	runInit([]string{"codex", "--yes"})

	if _, err := os.Stat(filepath.Join(codexHome, "hooks.json")); err != nil {
		t.Errorf("hooks.json not written under CODEX_HOME: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "hooks.json")); !os.IsNotExist(err) {
		t.Errorf("hooks.json written under HOME despite CODEX_HOME (err=%v)", err)
	}
}

func TestInitCodexUpdatesStaleDeadeyeEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatal(err)
	}
	hooksPath := filepath.Join(codexDir, "hooks.json")
	stale := `{"hooks":{"PreToolUse":[{"matcher":"^(Bash|apply_patch)$","hooks":[{"type":"command","command":"` + codexScriptPath() + ` PreToolUse"}]}]}}`
	if err := os.WriteFile(hooksPath, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	runInit([]string{"codex", "--yes"})

	b, _ := os.ReadFile(hooksPath)
	if !strings.Contains(string(b), "Edit|Write|Read|Grep|WebFetch") {
		t.Error("stale PreToolUse matcher was not upgraded")
	}
}

func TestUninstallCodexRemovesSidecarsWithoutHooksFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := writeCodexAdapter(codexScriptPath()); err != nil {
		t.Fatal(err)
	}
	skillPath, err := writePRCommand("codex")
	if err != nil {
		t.Fatal(err)
	}

	runUninstallCodex()

	if _, err := os.Stat(codexScriptPath()); !os.IsNotExist(err) {
		t.Errorf("adapter script survived uninstall without hooks.json (err=%v)", err)
	}
	if _, err := os.Stat(skillPath); !os.IsNotExist(err) {
		t.Errorf("PR-review skill survived uninstall without hooks.json (err=%v)", err)
	}
}
