package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
)

// loadSchema reads schema/config.schema.json, the file docs/site/settings.html
// is generated from.
func loadSchema(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("../../schema/config.schema.json")
	if err != nil {
		t.Fatalf("schema unreadable: %v", err)
	}
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	return d
}

func schemaProps(t *testing.T, d map[string]any, path ...string) map[string]any {
	t.Helper()
	cur := d
	for _, p := range path {
		next, ok := cur[p].(map[string]any)
		if !ok {
			t.Fatalf("schema path %v missing at %q", path, p)
		}
		cur = next
	}
	return cur
}

// jsonNames returns a struct's json tag names, skipping "-".
func jsonNames(t *testing.T, v any) []string {
	t.Helper()
	rt := reflect.TypeOf(v)
	var out []string
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		out = append(out, name)
	}
	return out
}

// Every mode axis must be settable from the CLI and described in the schema.
//
// This exists because tier_sample shipped in 0.65.0 with neither: the
// feature worked, but `deadeye config set mode.tier_sample on` -- the
// command its own CHANGELOG and skill told users to run -- was rejected by
// the tunables whitelist, and the settings page never mentioned it. A
// feature nobody can turn on is not shipped.
func TestEveryModeAxisIsSettableAndDocumented(t *testing.T) {
	schemaMode := schemaProps(t, loadSchema(t), "properties", "mode", "properties")
	for _, name := range jsonNames(t, config.Modes{}) {
		key := "mode." + name
		if _, ok := findTunable(key); !ok {
			t.Errorf("%s has no tunable entry -- `deadeye config set %s` would be rejected", key, key)
		}
		if _, ok := schemaMode[name]; !ok {
			t.Errorf("%s is missing from config.schema.json -- it won't appear on the settings page", key)
		}
	}
}

// Every top-level config block must be described in the schema, so the
// generated settings page can't silently omit a whole feature's tuning.
func TestEveryTopLevelConfigKeyIsInSchema(t *testing.T) {
	props := schemaProps(t, loadSchema(t), "properties")
	for _, name := range jsonNames(t, config.Config{}) {
		if _, ok := props[name]; !ok {
			t.Errorf("top-level config key %q is missing from config.schema.json", name)
		}
	}
}

// A tunable's enum must match the schema's enum exactly. A drift here means
// the CLI and the published settings page disagree about what's valid.
func TestTunableEnumsMatchSchema(t *testing.T) {
	schemaMode := schemaProps(t, loadSchema(t), "properties", "mode", "properties")
	for _, tn := range tunables {
		name, ok := strings.CutPrefix(tn.key, "mode.")
		if !ok || tn.kind != "enum" {
			continue
		}
		entry, ok := schemaMode[name].(map[string]any)
		if !ok {
			continue // covered by the test above
		}
		rawEnum, ok := entry["enum"].([]any)
		if !ok {
			t.Errorf("schema mode.%s has no enum but the CLI treats it as one", name)
			continue
		}
		var fromSchema []string
		for _, v := range rawEnum {
			fromSchema = append(fromSchema, v.(string))
		}
		if !reflect.DeepEqual(fromSchema, tn.allowed) {
			t.Errorf("mode.%s enum drift:\n  CLI:    %v\n  schema: %v", name, tn.allowed, fromSchema)
		}
	}
}
