package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/prreview"
)

// TestWritePRCommandPerHost: each host's rendering lands at the expected
// path, carries the shared rubric (marker) and the host's own PR-argument
// token, and removePRCommand takes it back out.
func TestWritePRCommandPerHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	t.Chdir(cwd)

	cases := []struct{ host, argToken string }{
		{"codex", "user's prompt"},
		{"gemini", "{{args}}"},
		{"cursor", "user's message"},
		{"windsurf", "user's message"},
	}
	for _, c := range cases {
		path, err := writePRCommand(c.host)
		if err != nil {
			t.Fatalf("%s: writePRCommand: %v", c.host, err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: file not written: %v", c.host, err)
		}
		s := string(b)
		if !strings.Contains(s, prreview.Marker) {
			t.Errorf("%s: rendering missing rubric marker", c.host)
		}
		if !strings.Contains(s, c.argToken) {
			t.Errorf("%s: rendering missing arg token %q", c.host, c.argToken)
		}
		if c.host == "codex" && strings.Contains(s, "$ARGUMENTS") {
			t.Error("codex skill should not use deprecated custom-prompt arguments")
		}
		removePRCommand(c.host)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s: removePRCommand left the file (err=%v)", c.host, err)
		}
	}

	if p, _ := prCommandPath("codex", cwd, home); p != filepath.Join(home, ".agents", "skills", "deadeye-pr", "SKILL.md") {
		t.Errorf("codex path = %s", p)
	}
}

// TestWritePRCommandRefusesForeign: a pre-existing file without deadeye's
// marker is never overwritten, and never deleted by uninstall.
func TestWritePRCommandRefusesForeign(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	t.Chdir(cwd)

	path, _ := prCommandPath("windsurf", cwd, home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("my own workflow\n"), 0o644)

	if _, err := writePRCommand("windsurf"); err == nil {
		t.Error("expected refusal to overwrite a foreign file")
	}
	if b, _ := os.ReadFile(path); string(b) != "my own workflow\n" {
		t.Error("foreign file was modified")
	}
	removePRCommand("windsurf")
	if _, err := os.Stat(path); err != nil {
		t.Error("removePRCommand deleted a foreign file")
	}
}

func TestRemoveCodexPRCommandRemovesLegacyPrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	codexHome := filepath.Join(home, "codex-home")
	t.Setenv("CODEX_HOME", codexHome)
	cwd := t.TempDir()
	t.Chdir(cwd)

	skill, err := writePRCommand("codex")
	if err != nil {
		t.Fatalf("writePRCommand: %v", err)
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

	removePRCommand("codex")
	for _, path := range append([]string{skill}, legacy...) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("removePRCommand left %s (err=%v)", path, err)
		}
	}
}
