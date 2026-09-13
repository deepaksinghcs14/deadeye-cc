package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// TestDoctorDispatchedToolsMatchTheSwitch: doctor checks the INSTALLED
// hooks.json at runtime against a hardcoded list, which is only useful if
// that list is the real one. Parse decidePreToolUse's switch and compare,
// so the list can't drift from the code it claims to describe -- the same
// drift that hid the Workflow advisory for three releases.
func TestDoctorDispatchedToolsMatchTheSwitch(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "decide.go", nil, 0)
	if err != nil {
		t.Fatalf("parse decide.go: %v", err)
	}
	var fromCode []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "decidePreToolUse" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range cc.List {
				lit, ok := e.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if s, err := strconv.Unquote(lit.Value); err == nil && s != "" {
					fromCode = append(fromCode, s)
				}
			}
			return true
		})
	}
	if len(fromCode) == 0 {
		t.Fatal("found no case labels in decidePreToolUse")
	}

	have := map[string]bool{}
	for _, tool := range preToolUseDispatchedTools {
		have[tool] = true
	}
	var missing []string
	for _, tool := range fromCode {
		// apply_patch is Codex's edit tool and never appears in Claude
		// Code's manifest; doctor only inspects that manifest.
		if tool == "apply_patch" || have[tool] {
			continue
		}
		missing = append(missing, tool)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("decidePreToolUse handles %v but doctor's preToolUseDispatchedTools omits them -- "+
			"doctor would report a broken manifest as healthy", missing)
	}
}

// TestDoctorFlagsAnUnmatchedTool: the manifest check has to actually catch
// a matcher that lost a tool, which is how the Grep and Workflow
// advisories each shipped dead.
func TestDoctorFlagsAnUnmatchedTool(t *testing.T) {
	full := `{"hooks":{"PreToolUse":[{"matcher":"Bash|Edit|Write|Agent|Read|Grep|WebFetch|Workflow"}]}}`
	if got := toolsMissingFromMatchers(full); len(got) != 0 {
		t.Errorf("complete manifest reported missing: %v", got)
	}
	partial := `{"hooks":{"PreToolUse":[{"matcher":"Bash|Edit"}]}}`
	got := toolsMissingFromMatchers(partial)
	if len(got) == 0 {
		t.Fatal("a manifest missing most tools reported nothing")
	}
	for _, want := range []string{"Agent", "Workflow"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing list %v does not include %q", got, want)
		}
	}
}

// TestDoctorDetectsAWorldReadableStateDir: the decision log carries prompt
// markers and credential paths, and one install path created this
// directory 0755.
func TestDoctorDetectsAWorldReadableStateDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(meta.StateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := checkStateDir(); got.status != "fail" {
		t.Errorf("state dir at 0755 = %q, want fail (%s)", got.status, got.detail)
	}
	if err := os.Chmod(meta.StateDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := checkStateDir(); got.status != "ok" {
		t.Errorf("state dir at 0700 = %q, want ok (%s)", got.status, got.detail)
	}
}

// TestDoctorDetectsAnUnparseableConfig: config.Load fails open to defaults
// by design, so nothing else in the product can tell the user their
// settings silently stopped applying.
func TestDoctorDetectsAnUnparseableConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(meta.StateDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := checkConfigParses(); got.status != "ok" {
		t.Errorf("no config file = %q, want ok", got.status)
	}
	if err := os.WriteFile(meta.ConfigPath(), []byte(`{"mode":{"routing":"enforce",}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := checkConfigParses()
	if got.status != "fail" {
		t.Errorf("unparseable config = %q, want fail", got.status)
	}
	if got.fix == "" || !filepath.IsAbs(meta.ConfigPath()) {
		t.Error("the fix line should name the file to repair")
	}
}
