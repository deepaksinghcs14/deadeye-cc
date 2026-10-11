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
	skillPath, _ := commandPath(prCmd, "codex", "", home)
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
	skillPath, err := writeCommand(prCmd, "codex")
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

// The plugin's hooks file and its manifest are checked-in copies of things
// the code already knows. Regenerate with:
//
//	DEADEYE_REGEN=1 go test ./cmd/deadeye -run TestCodexPlugin
func TestCodexPluginHooksCanary(t *testing.T) {
	const path = "../../hooks/codex-hooks.json"
	want := codexPluginHooksJSON()
	if os.Getenv("DEADEYE_REGEN") == "1" {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Error("hooks/codex-hooks.json drifted from codexEvents -- a plugin install and `init codex` would register different hooks")
	}
}

// Codex reads .codex-plugin/plugin.json ahead of .claude-plugin/plugin.json.
// Its `hooks` key is what keeps Codex off hooks/hooks.json, which is
// written in Claude Code's dialect -- so the key must be there, must point
// at a real file, and the two manifests must not disagree on the version
// (the release workflow only checks Claude's).
func TestCodexPluginManifest(t *testing.T) {
	read := func(path string) map[string]any {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return m
	}
	codex, claude := read("../../.codex-plugin/plugin.json"), read("../../.claude-plugin/plugin.json")
	if codex["version"] != claude["version"] || codex["name"] != claude["name"] {
		t.Errorf("manifests disagree: codex %v@%v, claude %v@%v", codex["name"], codex["version"], claude["name"], claude["version"])
	}
	hooks, _ := codex["hooks"].(string)
	// Codex ignores a manifest path that doesn't start with "./".
	if !strings.HasPrefix(hooks, "./") {
		t.Fatalf("hooks = %q, want a ./-relative path", hooks)
	}
	if _, err := os.Stat(filepath.Join("../..", hooks)); err != nil {
		t.Errorf("manifest points at a hooks file that isn't there: %v", err)
	}
}
