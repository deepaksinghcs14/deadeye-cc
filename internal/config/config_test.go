package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestLoadForReadsProjectConfigFromGivenCwd is the regression test for the
// cross-project config bleed: the daemon serves every project, so config
// must come from the SESSION's cwd (passed in), never the daemon process's
// own working directory.
func TestLoadForReadsProjectConfigFromGivenCwd(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate from any real ~/.deadeye/config.json
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".deadeye.json"), []byte(`{"mode":{"preprocess":"off"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got := LoadFor(dir, nil)
	if got.Mode.Preprocess != "off" {
		t.Errorf("Mode.Preprocess = %q, want %q from %s/.deadeye.json", got.Mode.Preprocess, "off", dir)
	}

	// A different cwd with no project config must get the defaults, not
	// the other project's overlay.
	other := LoadFor(t.TempDir(), nil)
	if other.Mode.Preprocess != "on" {
		t.Errorf("unrelated cwd inherited another project's config: Mode.Preprocess = %q, want %q", other.Mode.Preprocess, "on")
	}
}

// TestLoadForAppliesKillSwitches: the off list (derived from the CLIENT's
// env, carried over the wire) must fold into the effective config.
func TestLoadForAppliesKillSwitches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := LoadFor("", []string{"DEADEYE_PREPROCESS"})
	if cfg.Mode.Preprocess != "off" {
		t.Errorf("DEADEYE_PREPROCESS off-switch did not disable preprocess: %q", cfg.Mode.Preprocess)
	}
	if cfg.Mode.PlanGate == "off" {
		t.Error("DEADEYE_PREPROCESS off-switch must not touch the plan gate")
	}

	cfg = LoadFor("", []string{"DEADEYE_GATE"})
	if cfg.Mode.PlanGate != "off" {
		t.Errorf("DEADEYE_GATE off-switch did not disable the plan gate: %q", cfg.Mode.PlanGate)
	}

	cfg = LoadFor("", []string{"DEADEYE"})
	if cfg.Mode.Routing != "off" || cfg.Mode.Preprocess != "off" || cfg.Mode.PlanGate != "off" || cfg.Mode.WorkflowHint != "off" || cfg.Mode.Effort != "off" {
		t.Errorf("DEADEYE off-switch must disable every axis, got %+v", cfg.Mode)
	}
}

// TestWriteDefaultIfMissing: installing the plugin never created the
// config file users are told they can tweak -- the first daemon start now
// seeds it with every default spelled out. An existing file must never be
// touched, whatever it contains.
func TestWriteDefaultIfMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".deadeye"), 0o700); err != nil {
		t.Fatal(err)
	}

	WriteDefaultIfMissing()

	path := filepath.Join(home, ".deadeye", "config.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config.json was not created: %v", err)
	}

	// It must round-trip to exactly the defaults, and carry the $schema
	// pointer for editor tooling.
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("seeded config.json is not valid JSON: %v\n%s", err, b)
	}
	if _, ok := raw["$schema"]; !ok {
		t.Error("seeded config.json missing the $schema pointer")
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	// The seed writes disabled_rules as [] rather than null for
	// hand-editability; nil vs empty is behaviorally identical
	// (DisabledRuleSet only checks length), so normalize before comparing.
	if len(cfg.Preprocess.DisabledRules) == 0 {
		cfg.Preprocess.DisabledRules = nil
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Errorf("seeded config = %+v, want exactly Default() %+v", cfg, Default())
	}

	// Second call with a user-modified file: must not overwrite.
	if err := os.WriteFile(path, []byte(`{"mode":{"routing":"enforce"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	WriteDefaultIfMissing()
	b2, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b2) != `{"mode":{"routing":"enforce"}}` {
		t.Errorf("an existing config.json was overwritten: %s", b2)
	}
}
