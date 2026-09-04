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

// geminiHookScript is the adapter installed to ~/.deadeye/hooks/. Kept
// byte-identical to hooks/deadeye-gemini-hook.sh by TestGeminiScriptMatches.
const geminiHookScript = `#!/usr/bin/env bash
# deadeye hook adapter for Gemini CLI. Installed by ` + "`deadeye init gemini`" + `
# to ~/.deadeye/hooks/ and referenced from the deadeye Gemini extension's
# hooks/hooks.json. Gemini passes the hook payload as JSON on stdin and
# reads the response as JSON on stdout -- the binary handles both; the
# --host gemini flag selects Gemini's output dialect (hookSpecificOutput.
# tool_input, decision:deny, etc.). The event name is passed as $1 (the
# canonical Claude event the daemon switches on), mapped from Gemini's own
# event name in hooks.json.
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

# Capture rather than exec: if the binary dies without output, Gemini
# still gets valid JSON (fail open, INV-5).
out="$("$BIN" hook "$EVENT" --host gemini 2>/dev/null)" || true
[ -n "$out" ] || out="{}"
printf '%s' "$out"
`

// geminiEvents is what init gemini registers. Each maps a Gemini lifecycle
// event (the hooks.json key) to the CANONICAL Claude event name passed as
// the script's arg, which the daemon switches on. THIS RELEASE registers
// only the context-injection surfaces (SessionStart persona + BeforeAgent
// = UserPromptSubmit guidance/codemap/dep-flag/large-paste/plan-gate) --
// these read the prompt and session state, not tool schemas, so they work
// by shape. Tool-level surfaces (BeforeTool/AfterTool -> the exfil guard,
// preprocessing, routing) are deliberately NOT registered yet: Gemini's
// tool_input field names differ from Claude's and are unverified, so a
// blind wiring could silently no-op the exfil guard (false security). They
// join once verified on a live install (verified.md §13).
var geminiEvents = []struct{ geminiEvent, canonicalArg string }{
	{"SessionStart", "SessionStart"},
	{"BeforeAgent", "UserPromptSubmit"},
}

func geminiExtensionDir() string { return filepath.Join(meta.StateDir(), "gemini-extension") }
func geminiHooksPath() string    { return filepath.Join(geminiExtensionDir(), "hooks", "hooks.json") }
func geminiManifestPath() string { return filepath.Join(geminiExtensionDir(), "gemini-extension.json") }
func geminiScriptPath() string {
	return filepath.Join(meta.StateDir(), "hooks", "deadeye-gemini-hook.sh")
}

// runInitGemini backs `deadeye init gemini [--yes]`. Unlike Codex (which
// merges into the user's ~/.codex/hooks.json), this writes a self-contained
// deadeye extension under ~/.deadeye/gemini-extension/ and prints the
// `gemini extensions install` command for the user to run -- deadeye never
// edits Gemini's own config, and the user's install step is their consent.
func runInitGemini(args []string) {
	assumeYes := false
	for _, a := range args {
		if a == "--yes" || a == "-y" {
			assumeYes = true
		}
	}

	script := geminiScriptPath()
	hooks := map[string]any{}
	for _, e := range geminiEvents {
		hooks[e.geminiEvent] = []any{map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": script + " " + e.canonicalArg}},
		}}
	}
	hooksDoc, _ := json.MarshalIndent(map[string]any{"hooks": hooks}, "", "  ")
	manifest, _ := json.MarshalIndent(map[string]any{
		"name":        "deadeye",
		"version":     meta.Version,
		"description": "deadeye: lean-first coder persona and session guidance for Gemini CLI (experimental -- context-injection tier).",
	}, "", "  ")

	fmt.Println(cHead("deadeye init gemini") + cDim("  (experimental -- context-injection tier; tool-level engine pending live verification)"))
	fmt.Println()
	fmt.Println("Will write a self-contained deadeye extension:")
	fmt.Println("  " + cValue(geminiManifestPath()))
	fmt.Println("  " + cValue(geminiHooksPath()))
	fmt.Println("  " + cValue(script) + cDim("  (hook adapter)"))
	fmt.Println()
	fmt.Println(cDim("Registers: SessionStart (coder persona) and BeforeAgent (session guidance)."))
	fmt.Println(cDim("Not yet: tool-level features (exfil guard, output trimming) -- Gemini's tool schemas need live confirmation first."))
	fmt.Println()

	if !assumeYes {
		fmt.Print("Apply? [y/N] ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if l := strings.ToLower(strings.TrimSpace(line)); l != "y" && l != "yes" {
			fmt.Println("Nothing written.")
			return
		}
	}

	for _, p := range []string{filepath.Dir(script), filepath.Dir(geminiHooksPath())} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "deadeye init gemini:", err)
			os.Exit(1)
		}
	}
	if err := os.WriteFile(script, []byte(geminiHookScript), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "deadeye init gemini:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(geminiManifestPath(), append(manifest, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "deadeye init gemini:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(geminiHooksPath(), append(hooksDoc, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "deadeye init gemini:", err)
		os.Exit(1)
	}
	fmt.Println(cGood("Wrote") + " the extension. Install it into Gemini with:")
	fmt.Println("  " + cValue("gemini extensions install --path "+geminiExtensionDir()))
	for _, cmd := range hostCmds {
		installCommand(cmd, "gemini")
	}
	fmt.Println(cDim("Remove with: deadeye uninstall gemini  (then: gemini extensions uninstall deadeye)"))
}

// runUninstallGemini removes the deadeye Gemini extension scaffold. It does
// not run Gemini's own uninstall -- it tells the user to.
func runUninstallGemini() {
	for _, cmd := range hostCmds {
		removeCommand(cmd, "gemini")
	}
	removed := false
	for _, p := range []string{geminiExtensionDir(), geminiScriptPath()} {
		if err := os.RemoveAll(p); err == nil {
			removed = true
		}
	}
	if removed {
		fmt.Println(cGood("Removed") + " the deadeye Gemini extension scaffold.")
	}
	fmt.Println(cDim("If you installed it into Gemini, also run: gemini extensions uninstall deadeye"))
}
