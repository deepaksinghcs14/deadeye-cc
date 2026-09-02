package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/prreview"
)

// TestWriteCommandPerHost: each host's rendering lands at the expected
// path, carries the shared rubric (marker) and the host's own argument
// token, and removeCommand takes it back out. Runs for both prCmd and
// reviewCmd -- same rendering machinery, different rubric.
func TestWriteCommandPerHost(t *testing.T) {
	cmds := []hostCmd{prCmd, reviewCmd}
	hostCases := []struct{ host, argToken string }{
		{"codex", "user's prompt"},
		{"gemini", "{{args}}"},
		{"cursor", "user's message"},
		{"windsurf", "user's message"},
	}
	for _, cmd := range cmds {
		for _, c := range hostCases {
			home := t.TempDir()
			t.Setenv("HOME", home)
			cwd := t.TempDir()
			t.Chdir(cwd)

			path, err := writeCommand(cmd, c.host)
			if err != nil {
				t.Fatalf("%s/%s: writeCommand: %v", cmd.name, c.host, err)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%s/%s: file not written: %v", cmd.name, c.host, err)
			}
			s := string(b)
			if !strings.Contains(s, cmd.marker) {
				t.Errorf("%s/%s: rendering missing rubric marker", cmd.name, c.host)
			}
			if !strings.Contains(s, c.argToken) {
				t.Errorf("%s/%s: rendering missing arg token %q", cmd.name, c.host, c.argToken)
			}
			if c.host == "codex" && strings.Contains(s, "$ARGUMENTS") {
				t.Errorf("%s: codex skill should not use deprecated custom-prompt arguments", cmd.name)
			}
			removeCommand(cmd, c.host)
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("%s/%s: removeCommand left the file (err=%v)", cmd.name, c.host, err)
			}
		}
	}

	home, cwd := t.TempDir(), t.TempDir()
	if p, _ := commandPath(prCmd, "codex", cwd, home); p != filepath.Join(home, ".agents", "skills", "deadeye-pr", "SKILL.md") {
		t.Errorf("codex prCmd path = %s", p)
	}
	if p, _ := commandPath(reviewCmd, "codex", cwd, home); p != filepath.Join(home, ".agents", "skills", "deadeye-review", "SKILL.md") {
		t.Errorf("codex reviewCmd path = %s", p)
	}
}

// TestWriteCommandRefusesForeign: a pre-existing file without deadeye's
// marker is never overwritten, and never deleted by uninstall.
func TestWriteCommandRefusesForeign(t *testing.T) {
	for _, cmd := range []hostCmd{prCmd, reviewCmd} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		cwd := t.TempDir()
		t.Chdir(cwd)

		path, _ := commandPath(cmd, "windsurf", cwd, home)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(path, []byte("my own workflow\n"), 0o644)

		if _, err := writeCommand(cmd, "windsurf"); err == nil {
			t.Errorf("%s: expected refusal to overwrite a foreign file", cmd.name)
		}
		if b, _ := os.ReadFile(path); string(b) != "my own workflow\n" {
			t.Errorf("%s: foreign file was modified", cmd.name)
		}
		removeCommand(cmd, "windsurf")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s: removeCommand deleted a foreign file", cmd.name)
		}
	}
}

func TestRemoveCodexPRCommandRemovesLegacyPrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	codexHome := filepath.Join(home, "codex-home")
	t.Setenv("CODEX_HOME", codexHome)
	cwd := t.TempDir()
	t.Chdir(cwd)

	skill, err := writeCommand(prCmd, "codex")
	if err != nil {
		t.Fatalf("writeCommand: %v", err)
	}
	legacy := []string{
		filepath.Join(home, ".codex", "prompts", "deadeye-pr.md"),
		filepath.Join(codexHome, "prompts", "deadeye-pr.md"),
	}
	for _, path := range legacy {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(prreview.Marker+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	removeCommand(prCmd, "codex")
	for _, path := range append([]string{skill}, legacy...) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("removeCommand left %s (err=%v)", path, err)
		}
	}
}

// TestReviewCommandHasNoLegacyCodexPaths: reviewCmd never shipped to
// codex's old prompts dir, so its uninstall must not touch it -- a
// deadeye-pr legacy file with reviewCmd's (wrong) marker check would only
// ever be true by coincidence, so assert the field is simply unset.
func TestReviewCommandHasNoLegacyCodexPaths(t *testing.T) {
	if reviewCmd.legacyPaths != nil {
		t.Error("reviewCmd.legacyPaths should be nil -- it never shipped to codex's legacy prompts dir")
	}
}
