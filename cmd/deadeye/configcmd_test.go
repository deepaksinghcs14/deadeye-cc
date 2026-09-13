package main

import (
	"os"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// TestConfigSetRefusesUnparseableConfig: a config.json that exists but
// doesn't parse must make `config set` fail loudly and leave the file
// byte-for-byte alone. Before this, loadConfigMap swallowed the parse
// error and configSet re-serialized an empty map -- a trailing comma
// silently wiped every setting the user had (reproduced live). The read
// path (currentValue) keeps failing open to defaults, like config.Load().
func TestConfigSetRefusesUnparseableConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(meta.StateDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	broken := []byte(`{"mode":{"plan_gate":"hard","routing":"enforce",},"coder":{"default_level":"sniper"}}` + "\n")
	if err := os.WriteFile(meta.ConfigPath(), broken, 0o600); err != nil {
		t.Fatal(err)
	}

	err := configSet("mode.codemap", "off")
	if err == nil {
		t.Fatal("configSet over an unparseable config.json returned nil; it must refuse to write")
	}
	if !strings.Contains(err.Error(), meta.ConfigPath()) || !strings.Contains(err.Error(), "not writing") {
		t.Errorf("error should name the file and say it won't write; got %q", err)
	}
	after, err := os.ReadFile(meta.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(broken) {
		t.Errorf("config.json was modified despite the parse failure:\n%s", after)
	}
	if v := currentValue("mode.routing"); v == "" {
		t.Error("read path should still fall back to a default over a broken file, got empty")
	}
}

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
