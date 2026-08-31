package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
)

// TestSkewNudge: warns once when the binary is behind the plugin, stays quiet
// when the binary is ahead (dev checkout) or there's no plugin root.
func TestSkewNudge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st := newDaemonState(catalog.Catalog{}, nil)
	root := t.TempDir()
	pj := filepath.Join(root, ".claude-plugin")
	if err := os.MkdirAll(pj, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(v string) {
		os.WriteFile(filepath.Join(pj, "plugin.json"), []byte(`{"version":"`+v+`"}`), 0o644)
	}

	// binary behind plugin -> warn once, then silent for that version
	write("99.0.0")
	if s := skewNudge(root, st, "s1"); s == "" || !strings.Contains(s, "99.0.0") {
		t.Errorf("expected skew warning for a behind binary, got %q", s)
	}
	if s := skewNudge(root, st, "s1"); s != "" {
		t.Error("skew must warn at most once per plugin version")
	}

	// binary ahead of plugin (dev checkout) -> no warning
	t.Setenv("HOME", t.TempDir())
	write("0.0.1")
	if s := skewNudge(root, st, "s1"); s != "" {
		t.Errorf("a binary ahead of the plugin should not warn, got %q", s)
	}

	// no plugin root -> no warning
	if s := skewNudge("", st, "s1"); s != "" {
		t.Error("empty plugin root should not warn")
	}
}
