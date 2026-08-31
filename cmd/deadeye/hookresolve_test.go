package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestHookBinaryResolution guards deadeye-hook.sh's version-gated choice: a
// PATH binary behind the plugin defers to the managed self-updating binary
// (so auto-update is never shadowed), while a current/newer PATH binary wins.
func TestHookBinaryResolution(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	hook, err := filepath.Abs("../../hooks/deadeye-hook.sh")
	if err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, ".claude-plugin", "plugin.json"), []byte(`{"version":"0.27.0"}`), 0o644)

	stub := func(dir, ver, src string) {
		os.MkdirAll(dir, 0o755)
		s := "#!/usr/bin/env bash\n" +
			`[ "$1" = version ] && { echo "deadeye ` + ver + `"; exit 0; }` + "\n" +
			`[ "$1" = hook ] && { echo '{"src":"` + src + `"}'; exit 0; }` + "\n"
		os.WriteFile(filepath.Join(dir, "deadeye"), []byte(s), 0o755)
	}
	stub(filepath.Join(home, ".deadeye", "bin"), "0.27.0", "managed") // managed at plugin version
	pdir := t.TempDir()

	run := func(pathVer string) string {
		stub(pdir, pathVer, "path")
		cmd := exec.Command("bash", hook, "PreToolUse")
		cmd.Env = []string{"HOME=" + home, "CLAUDE_PLUGIN_ROOT=" + root, "PATH=" + pdir + ":/usr/bin:/bin"}
		out, _ := cmd.Output()
		return strings.TrimSpace(string(out))
	}

	if got := run("0.13.0"); !strings.Contains(got, `"managed"`) {
		t.Errorf("a stale PATH binary must defer to the managed one, got %q", got)
	}
	if got := run("0.27.0"); !strings.Contains(got, `"path"`) {
		t.Errorf("a current PATH binary must win, got %q", got)
	}
	if got := run("0.99.0"); !strings.Contains(got, `"path"`) {
		t.Errorf("a newer PATH binary (dev build) must win, got %q", got)
	}
}
