package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGeminiScriptMatches is the canary keeping hooks/deadeye-gemini-hook.sh
// byte-identical to the embedded geminiHookScript const.
func TestGeminiScriptMatches(t *testing.T) {
	b, err := os.ReadFile("../../hooks/deadeye-gemini-hook.sh")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != geminiHookScript {
		t.Error("hooks/deadeye-gemini-hook.sh drifted from the embedded geminiHookScript const")
	}
}

// TestInitAndUninstallGemini: init writes a self-contained extension
// scaffold (manifest + hooks.json + adapter script), the hooks.json maps
// Gemini events to the canonical deadeye event args, the script carries
// --host gemini, and uninstall removes it all.
func TestInitAndUninstallGemini(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	runInitGemini([]string{"--yes"})

	// Adapter script written and host-tagged.
	scriptB, err := os.ReadFile(geminiScriptPath())
	if err != nil {
		t.Fatalf("adapter script not written: %v", err)
	}
	if !strings.Contains(string(scriptB), "--host gemini") {
		t.Error("adapter script missing --host gemini")
	}

	// Manifest present with the deadeye name.
	manB, err := os.ReadFile(geminiManifestPath())
	if err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
	if !strings.Contains(string(manB), `"name": "deadeye"`) {
		t.Error("manifest missing deadeye name")
	}

	// hooks.json maps Gemini event names to the canonical args.
	hooksB, err := os.ReadFile(geminiHooksPath())
	if err != nil {
		t.Fatalf("hooks.json not written: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(hooksB, &doc); err != nil {
		t.Fatalf("hooks.json invalid: %v", err)
	}
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks["SessionStart"] == nil || hooks["BeforeAgent"] == nil {
		t.Errorf("hooks.json missing expected events: %v", hooks)
	}
	// BeforeAgent must invoke the script with the canonical UserPromptSubmit arg.
	if !strings.Contains(string(hooksB), "UserPromptSubmit") {
		t.Error("BeforeAgent should map to the canonical UserPromptSubmit arg")
	}
	// Tool-level events are deliberately NOT registered this release.
	if hooks["BeforeTool"] != nil {
		t.Error("BeforeTool must not be registered until Gemini tool schemas are verified")
	}

	// Uninstall removes the whole scaffold.
	runUninstallGemini()
	if _, err := os.Stat(geminiExtensionDir()); !os.IsNotExist(err) {
		t.Errorf("uninstall left the extension dir (err=%v)", err)
	}
	if _, err := os.Stat(geminiScriptPath()); !os.IsNotExist(err) {
		t.Errorf("uninstall left the adapter script (err=%v)", err)
	}
}

// TestGeminiHooksCommandUsesAbsoluteScript: the command in hooks.json must
// reference the adapter script by its real path (Gemini runs it directly).
func TestGeminiHooksCommandUsesAbsoluteScript(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	runInitGemini([]string{"--yes"})
	b, _ := os.ReadFile(geminiHooksPath())
	if !strings.Contains(string(b), filepath.Join(geminiScriptPath())) {
		t.Errorf("hooks.json command should reference %s", geminiScriptPath())
	}
}
