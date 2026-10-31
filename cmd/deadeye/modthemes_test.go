package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The mod reads mod.theme and the CLI validates it: the slug list in
// hooks/themes.ts and modThemes must agree, or `deadeye config set
// mod.theme <x>` rejects a theme the picker offers (or accepts one the mod
// can't draw).
func TestModThemesMatchTable(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "hooks", "themes.ts"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^\s*T\('([a-z0-9]+)',`)
	var fromTable []string
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		fromTable = append(fromTable, m[1])
	}
	want := append([]string{"auto"}, fromTable...)
	if len(want) != len(modThemes) {
		t.Fatalf("modThemes has %d entries, hooks/themes.ts has %d (+auto):\n  CLI:   %v\n  table: %v", len(modThemes), len(fromTable), modThemes, want)
	}
	for i := range want {
		if want[i] != modThemes[i] {
			t.Errorf("modThemes[%d] = %q, hooks/themes.ts has %q", i, modThemes[i], want[i])
		}
	}
	// The schema enum is checked against the tunable by the config schema
	// tests only for mode.*; cover mod.theme here.
	schema := loadSchema(t)
	enum := schemaProps(t, schema, "properties", "mod", "properties", "theme")["enum"].([]any)
	if len(enum) != len(modThemes) {
		t.Fatalf("config.schema.json mod.theme enum has %d entries, modThemes %d", len(enum), len(modThemes))
	}
	for i, v := range enum {
		if v.(string) != modThemes[i] {
			t.Errorf("schema mod.theme enum[%d] = %q, CLI has %q", i, v, modThemes[i])
		}
	}
}
