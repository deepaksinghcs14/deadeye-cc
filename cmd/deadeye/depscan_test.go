package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
)

func writeManifest(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDependencyFlagFiresOnVulnerableManifest: an existing manifest whose
// declared dep has an OSV advisory produces the session-start summary
// naming it.
func TestDependencyFlagFiresOnVulnerableManifest(t *testing.T) {
	noNetworkOSV(t)
	state := coderTestState(t) // isolates HOME (and thus the OSV cache path)
	primeOSVCache(t, "npm", "lodash", "4.17.20", "GHSA-p6mc-m468-83gg")
	proj := t.TempDir()
	writeManifest(t, proj, "package.json", "{\n  \"dependencies\": {\n    \"lodash\": \"4.17.20\"\n  }\n}\n")

	line, ok := decideDependencyFlag(proj, config.Default(), state)
	if !ok || !strings.Contains(line, "lodash") || !strings.Contains(line, "/deadeye-guard") {
		t.Fatalf("expected a dep-flag summary naming lodash, got %q (ok=%v)", line, ok)
	}
}

// TestDependencyFlagFiresOnSuperseded: even with no OSV cache, the bundled
// superseded table flags a legacy dep.
func TestDependencyFlagFiresOnSuperseded(t *testing.T) {
	noNetworkOSV(t)
	state := coderTestState(t)
	proj := t.TempDir()
	writeManifest(t, proj, "package.json", "{\n  \"dependencies\": {\n    \"moment\": \"2.29.0\"\n  }\n}\n")

	line, ok := decideDependencyFlag(proj, config.Default(), state)
	if !ok || !strings.Contains(line, "moment") {
		t.Fatalf("expected a dep-flag summary naming moment, got %q (ok=%v)", line, ok)
	}
}

// TestDependencyFlagSilentWhenClean: a manifest with no flagged deps says
// nothing.
func TestDependencyFlagSilentWhenClean(t *testing.T) {
	noNetworkOSV(t)
	state := coderTestState(t)
	proj := t.TempDir()
	writeManifest(t, proj, "package.json", "{\n  \"dependencies\": {\n    \"react\": \"18.2.0\"\n  }\n}\n")

	if line, ok := decideDependencyFlag(proj, config.Default(), state); ok {
		t.Errorf("clean manifest should be silent, got %q", line)
	}
}

// TestDependencyFlagRespectsGates: no cwd, security off, coder disabled,
// and the dep-flag disable key each silence it.
func TestDependencyFlagRespectsGates(t *testing.T) {
	noNetworkOSV(t)
	state := coderTestState(t)
	proj := t.TempDir()
	writeManifest(t, proj, "package.json", "{\n  \"dependencies\": {\n    \"moment\": \"2.29.0\"\n  }\n}\n")

	if _, ok := decideDependencyFlag("", config.Default(), state); ok {
		t.Error("empty cwd should be silent")
	}

	off := config.Default()
	off.Coder.Security = "off"
	if _, ok := decideDependencyFlag(proj, off, state); ok {
		t.Error("security off should silence the dep flag")
	}

	dis := config.Default()
	dis.Coder.Disabled = true
	if _, ok := decideDependencyFlag(proj, dis, state); ok {
		t.Error("coder disabled should silence the dep flag")
	}

	drule := config.Default()
	drule.Preprocess.DisabledRules = []string{"dep-flag"}
	if _, ok := decideDependencyFlag(proj, drule, state); ok {
		t.Error("dep-flag disable key should silence it")
	}
}
