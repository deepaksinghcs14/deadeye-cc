package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestPreToolUseMatcherCoversEveryDispatchedTool: every tool name
// decidePreToolUse switches on must appear as an alternative in at least
// one hooks.json PreToolUse matcher, or the daemon's handler for it is
// dead code -- the hook never fires. This has now shipped twice: the Grep
// advisory in 0.9.0 (fixed by widening the matcher, e5d7ac7) and the
// Workflow tiering advisory in 0.58.0, which was unreachable until 0.61.1
// because "Workflow" was never added to the matcher while its unit test
// called the function directly and passed. Read both artifacts from disk
// so the test can't drift from either.
func TestPreToolUseMatcherCoversEveryDispatchedTool(t *testing.T) {
	root := filepath.Join("..", "..")

	raw, err := os.ReadFile(filepath.Join(root, "hooks", "hooks.json"))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	var manifest struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse hooks.json: %v", err)
	}
	// The daemon serves every host through one switch, so a tool name only
	// needs to be reachable from SOME host's PreToolUse matcher: Claude
	// Code's hooks.json, or the Codex adapter's regex (codexEvents) --
	// apply_patch is Codex's edit tool and rightly absent from Claude's
	// list, just as Agent/Workflow are Claude-only. A label in neither is
	// dead code.
	matched := map[string]bool{}
	for _, entry := range manifest.Hooks.PreToolUse {
		for _, alt := range strings.Split(entry.Matcher, "|") {
			matched[strings.TrimSpace(alt)] = true
		}
	}
	if len(matched) == 0 {
		t.Fatal("hooks.json has no PreToolUse matcher alternatives")
	}
	for _, ev := range codexEvents {
		if ev.event != "PreToolUse" {
			continue
		}
		m := strings.TrimSuffix(strings.TrimPrefix(ev.matcher, "^("), ")$")
		for _, alt := range strings.Split(m, "|") {
			matched[strings.TrimSpace(alt)] = true
		}
	}

	dispatched := preToolUseCaseLabels(t, filepath.Join("decide.go"))
	if len(dispatched) == 0 {
		t.Fatal("found no case \"<Tool>\" labels in decidePreToolUse -- did the function or its switch get renamed?")
	}
	for _, tool := range dispatched {
		if !matched[tool] {
			t.Errorf("decidePreToolUse handles %q but neither hooks.json's PreToolUse matcher nor the Codex adapter's lists it -- that handler can never fire", tool)
		}
	}
}

// preToolUseCaseLabels parses decide.go and returns every string literal
// used as a case label inside decidePreToolUse's switch statements.
func preToolUseCaseLabels(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var labels []string
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
				s, err := strconv.Unquote(lit.Value)
				if err == nil && s != "" {
					labels = append(labels, s)
				}
			}
			return true
		})
	}
	return labels
}
