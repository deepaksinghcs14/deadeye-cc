package main

import (
	"os"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// TestConfigSetGet: set validates + writes, get reads back, invalid values and
// unknown keys are rejected, coercion works, and an unset key falls back to the
// built-in default.
func TestConfigSetGet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// unset key -> built-in default, not empty
	if v := currentValue("mode.routing"); v == "" {
		t.Error("unset key should fall back to a default, got empty")
	}

	// enum set + read back
	if err := configSet("mode.plan_gate", "off"); err != nil {
		t.Fatal(err)
	}
	if err := configSet("coder.default_level", "sniper"); err != nil {
		t.Fatal(err)
	}
	if v := currentValue("mode.plan_gate"); v != "off" {
		t.Errorf("plan_gate = %q, want off", v)
	}
	// second write must preserve the first key
	if v := currentValue("coder.default_level"); v != "sniper" {
		t.Errorf("default_level = %q, want sniper (writes must preserve other keys)", v)
	}

	// enum rejects a bad value
	if err := configSet("mode.plan_gate", "banana"); err == nil {
		t.Error("expected enum rejection for banana")
	}
	// unknown key rejected (no typo can write a dead setting)
	if err := configSet("mode.bogus", "on"); err == nil {
		t.Error("expected unknown-key rejection")
	}
	// bool + int coercion
	if err := configSet("coder.security_osv", "false"); err != nil {
		t.Fatal(err)
	}
	if v := currentValue("coder.security_osv"); v != "false" {
		t.Errorf("security_osv = %q, want false", v)
	}
	if err := configSet("plan_gate.min_files", "5"); err != nil {
		t.Fatal(err)
	}
	if err := configSet("plan_gate.min_files", "notanint"); err == nil {
		t.Error("expected int rejection for notanint")
	}
}

// TestWelcomeNudgeOnce: the first-run welcome fires exactly once, ever, and
// records its flag file.
func TestWelcomeNudgeOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st := newDaemonState(catalog.Catalog{}, nil)

	if w := welcomeNudge(st, "s1"); w == "" {
		t.Fatal("first welcome should return onboarding text")
	}
	if w := welcomeNudge(st, "s1"); w != "" {
		t.Error("welcome must fire at most once ever")
	}
	if _, err := os.Stat(meta.WelcomedPath()); err != nil {
		t.Errorf("welcomed flag not written: %v", err)
	}
}
