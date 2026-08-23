package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/coder"
)

// TestInitAndUninstallRules covers the static rules-file hosts (cursor,
// windsurf): init writes a marker-tagged persona file at the host's
// convention path, is idempotent, refuses to clobber a foreign file, and
// uninstall removes only ours.
func TestInitAndUninstallRules(t *testing.T) {
	for _, host := range []string{"cursor", "windsurf"} {
		t.Run(host, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir()) // isolate config.Load's global overlay
			proj := t.TempDir()
			t.Chdir(proj)

			runInitRules(host, []string{"--yes"})

			path := filepath.Join(proj, rulesFileRelPath[host])
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("rules file not written: %v", err)
			}
			body := string(b)
			if !strings.Contains(body, coder.RulesetMarkdownMarker) {
				t.Error("rules file missing deadeye's marker")
			}
			if !strings.Contains(body, "The ladder") {
				t.Error("rules file missing the persona body")
			}
			if strings.Contains(body, "DEADEYE CODER ACTIVE") {
				t.Error("static rules file must not carry the runtime banner")
			}

			// Idempotent: a second init on our own file rewrites cleanly.
			runInitRules(host, []string{"--yes"})
			if b2, _ := os.ReadFile(path); string(b2) != body {
				t.Error("second init changed the file")
			}

			// Uninstall removes ours.
			runUninstallRules(host)
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("uninstall left the rules file (err=%v)", err)
			}
		})
	}
}

// TestRulesFileGuard is the "never clobber a foreign rules file" guard,
// tested directly (init/uninstall call os.Exit around it, so the guard is
// factored out): absent → writable, ours (has the marker) → writable,
// foreign → not.
func TestRulesFileGuard(t *testing.T) {
	dir := t.TempDir()

	absent := filepath.Join(dir, "absent.md")
	if !rulesFileIsOursOrAbsent(absent) {
		t.Error("an absent path should be writable")
	}

	ours := filepath.Join(dir, "ours.md")
	os.WriteFile(ours, []byte(coder.RulesetMarkdown(coder.LevelMarksman)), 0o644)
	if !rulesFileIsOursOrAbsent(ours) {
		t.Error("a deadeye-written file should be writable (it carries the marker)")
	}

	foreign := filepath.Join(dir, "foreign.md")
	os.WriteFile(foreign, []byte("# my own rules\nalways use tabs\n"), 0o644)
	if rulesFileIsOursOrAbsent(foreign) {
		t.Error("a user's own rules file must NOT be writable/clobberable")
	}
}
