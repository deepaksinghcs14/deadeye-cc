package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func cursorDoc(t *testing.T, s string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// ~/.cursor/hooks.json is the user's file. Registering twice must not
// duplicate deadeye's entries, and neither registering nor removing may
// touch an entry deadeye didn't write.
func TestMergeCursorHooksKeepsTheUsersEntries(t *testing.T) {
	const script = "/home/u/.deadeye/hooks/deadeye-cursor-hook.sh"
	theirs := `{"version":1,"custom":"kept","hooks":{
		"preToolUse":[{"command":"./mine.sh","matcher":"Shell"}],
		"afterFileEdit":[{"command":"./format.sh"}]}}`

	doc := mergeCursorHooks(cursorDoc(t, theirs), script, true)
	doc = mergeCursorHooks(doc, script, true) // idempotent
	hooks := doc["hooks"].(map[string]any)

	for _, e := range cursorHookEvents {
		ours := 0
		for _, entry := range hooks[e.event].([]any) {
			if isDeadeyeCursorEntry(entry, script) {
				ours++
				if got := entry.(map[string]any)["command"]; got != script+" "+e.event {
					t.Errorf("%s command = %v", e.event, got)
				}
			}
		}
		if ours != 1 {
			t.Errorf("%s has %d deadeye entries after two registrations, want 1", e.event, ours)
		}
	}
	if pre := hooks["preToolUse"].([]any); len(pre) != 2 || pre[0].(map[string]any)["command"] != "./mine.sh" {
		t.Errorf("the user's preToolUse hook was lost or reordered: %v", pre)
	}
	if doc["custom"] != "kept" {
		t.Error("an unrelated top-level key was dropped")
	}

	doc = mergeCursorHooks(doc, script, false)
	hooks = doc["hooks"].(map[string]any)
	if len(hooks) != 2 || len(hooks["preToolUse"].([]any)) != 1 || hooks["afterFileEdit"] == nil {
		t.Errorf("uninstall left %v, want exactly the user's two hooks", hooks)
	}

	// A fresh file gets the version Cursor requires.
	if fresh := mergeCursorHooks(nil, script, true); fresh["version"] != 1 {
		t.Errorf("version = %v, want 1", fresh["version"])
	}
}

// Cursor blocks a tool call whose permission-hook answer doesn't match the
// schema. With no deadeye binary at all, the adapter must still say allow.
func TestCursorAdapterAllowsWhenDeadeyeIsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash adapter")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "hook.sh")
	if err := os.WriteFile(script, []byte(cursorHookScript), 0o755); err != nil {
		t.Fatal(err)
	}
	for event, want := range map[string]string{"preToolUse": `{"permission":"allow"}`, "beforeReadFile": `{"permission":"allow"}`, "sessionStart": `{}`} {
		cmd := exec.Command("bash", script, event)
		cmd.Env = []string{"HOME=" + dir, "PATH=/usr/bin:/bin"}
		cmd.Stdin = strings.NewReader(`{}`)
		out, err := cmd.Output()
		if err != nil || string(out) != want {
			t.Errorf("%s: got %q (err %v), want %q", event, out, err, want)
		}
	}
}
