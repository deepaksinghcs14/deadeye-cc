package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// tunable is one config key the picker and validator know about. key is the
// dotted path, which is also its JSON location (mode.routing -> {mode:{routing}}).
// allowed lists enum values ("" kind=string/int/bool/number means free-form,
// coerced+range-checked instead). The registry is the allow-list: `config set`
// refuses any key not here, so a typo can't write a dead setting into the file.
type tunable struct {
	key, label, kind string
	allowed          []string
}

// tunables covers every scalar setting worth tuning. Arrays
// (preprocess.disabled_rules, security.sensitive_paths) are left to a direct
// config.json edit -- a comma-splitting set is more surprising than helpful.
var tunables = []tunable{
	{"mode.routing", "routing", "enum", []string{"off", "advise", "enforce"}},
	{"mode.effort", "effort", "enum", []string{"off", "advise"}},
	{"mode.preprocess", "context hygiene", "enum", []string{"off", "on"}},
	{"mode.plan_gate", "plan gate", "enum", []string{"off", "soft", "hard"}},
	{"mode.workflow_hint", "workflow hints", "enum", []string{"off", "on"}},
	{"mode.codemap", "codebase map", "enum", []string{"off", "on"}},
	{"mode.update_check", "update check", "enum", []string{"off", "on"}},
	{"coder.default_level", "coder persona", "enum", []string{"off", "spotter", "marksman", "sniper"}},
	{"coder.security", "coder security check", "enum", []string{"off", "advise", "ask"}},
	{"coder.security_osv", "OSV dependency lookup", "bool", []string{"true", "false"}},
	{"coder.subagent_matcher", "subagent matcher (regex)", "string", nil},
	{"security.exfil", "exfil guard", "enum", []string{"off", "advise", "ask"}},
	{"downshift_threshold", "downshift threshold (0-1)", "number", nil},
	{"injection_budget_tokens", "advisory budget (tokens)", "int", nil},
	{"coder.injection_budget_tokens", "coder injection budget (tokens)", "int", nil},
	{"plan_gate.min_files", "plan gate min files", "int", nil},
}

func findTunable(key string) (tunable, bool) {
	for _, t := range tunables {
		if t.key == key {
			return t, true
		}
	}
	return tunable{}, false
}

// runConfig backs `deadeye config [get <key> | set <key> <value> | list]`.
// With no args it opens the interactive picker.
func runConfig(args []string) {
	if len(args) == 0 {
		runConfigPicker()
		return
	}
	switch args[0] {
	case "get":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: deadeye config get <key>")
			os.Exit(2)
		}
		if _, ok := findTunable(args[1]); !ok {
			fmt.Fprintln(os.Stderr, "unknown key:", args[1], "\nrun `deadeye config list` for the keys.")
			os.Exit(2)
		}
		fmt.Println(currentValue(args[1]))
	case "set":
		if len(args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: deadeye config set <key> <value>")
			os.Exit(2)
		}
		if err := configSet(args[1], args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "deadeye config set:", err)
			os.Exit(1)
		}
		fmt.Println(cGood("✓") + " " + args[1] + " → " + cValue(args[2]) + cDim("   (~/.deadeye/config.json)"))
	case "list":
		for _, t := range tunables {
			allowed := "free text"
			if t.allowed != nil {
				allowed = strings.Join(t.allowed, " · ")
			}
			fmt.Printf("  %-30s %-12s %s\n", t.key, cValue(currentValue(t.key)), cDim(allowed))
		}
	default:
		fmt.Fprintln(os.Stderr, "usage: deadeye config [get <key> | set <key> <value> | list]  (no args: interactive)")
		os.Exit(2)
	}
}

// currentValue returns the effective value of key from ~/.deadeye/config.json,
// falling back to the built-in default when the key is absent.
func currentValue(key string) string {
	path := strings.Split(key, ".")
	if v, ok := rawGet(loadConfigMap(), path); ok {
		return scalarString(v)
	}
	if v, ok := rawGet(defaultMap(), path); ok {
		return scalarString(v)
	}
	return ""
}

// configSet validates value for key and writes it into ~/.deadeye/config.json,
// preserving every other key already in the file.
func configSet(key, value string) error {
	t, ok := findTunable(key)
	if !ok {
		return fmt.Errorf("unknown key %q -- run `deadeye config list`", key)
	}
	var coerced any
	switch t.kind {
	case "enum":
		if !contains(t.allowed, value) {
			return fmt.Errorf("%s must be one of: %s", key, strings.Join(t.allowed, ", "))
		}
		coerced = value
	case "bool":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%s must be true or false", key)
		}
		coerced = b
	case "int":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s must be a whole number", key)
		}
		coerced = n
	case "number":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("%s must be a number", key)
		}
		coerced = f
	default:
		coerced = value
	}

	m := loadConfigMap()
	rawSet(m, strings.Split(key, "."), coerced)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(meta.StateDir(), 0o700); err != nil {
		return err
	}
	// temp + rename so a crash mid-write never truncates the live config.
	tmp := meta.ConfigPath() + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, meta.ConfigPath())
}

// runConfigPicker is the terminal selector: a numbered list, pick a setting,
// pick a value. Stdlib only -- no raw-mode TUI, no dependency. Meaningful with
// a TTY; inside Claude Code the agent uses `config set` instead.
func runConfigPicker() {
	in := bufio.NewReader(os.Stdin)
	fmt.Println(cHead(meta.Name) + " " + cDim("· settings") + cDim("   type a number · q to quit"))
	fmt.Println()
	for {
		for i, t := range tunables {
			opts := "free text"
			if t.allowed != nil {
				opts = strings.Join(t.allowed, " · ")
			}
			fmt.Printf("  %2d  %-24s %-12s %s\n", i+1, t.label, cValue(currentValue(t.key)), cDim(opts))
		}
		fmt.Print("\n  change which? [1-", len(tunables), ", q] ")
		line, err := in.ReadString('\n')
		if err != nil {
			return
		}
		s := strings.TrimSpace(line)
		if s == "q" || s == "" {
			return
		}
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > len(tunables) {
			fmt.Println(cWarn("  not a valid choice."))
			continue
		}
		t := tunables[n-1]
		var val string
		if t.allowed != nil {
			fmt.Printf("\n  %s (currently: %s)\n", t.label, cValue(currentValue(t.key)))
			for i, a := range t.allowed {
				fmt.Printf("    %d) %s\n", i+1, a)
			}
			fmt.Print("  new value? [1-", len(t.allowed), "] ")
			vl, _ := in.ReadString('\n')
			vi, err := strconv.Atoi(strings.TrimSpace(vl))
			if err != nil || vi < 1 || vi > len(t.allowed) {
				fmt.Println(cWarn("  not a valid choice."))
				continue
			}
			val = t.allowed[vi-1]
		} else {
			fmt.Printf("\n  %s (currently: %s)\n  new value: ", t.label, currentValue(t.key))
			vl, _ := in.ReadString('\n')
			val = strings.TrimSpace(vl)
		}
		if err := configSet(t.key, val); err != nil {
			fmt.Println(cWarn("  " + err.Error()))
			continue
		}
		fmt.Println(cGood("  ✓ ") + t.key + " → " + cValue(val))
		fmt.Println()
	}
}

// --- helpers: raw nested map get/set + config.json / default loading ---

func loadConfigMap() map[string]any {
	m := map[string]any{}
	if b, err := os.ReadFile(meta.ConfigPath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func defaultMap() map[string]any {
	b, _ := json.Marshal(config.Default())
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	return m
}

func rawGet(m map[string]any, path []string) (any, bool) {
	cur := any(m)
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = mm[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func rawSet(m map[string]any, path []string, val any) {
	for i := 0; i < len(path)-1; i++ {
		next, ok := m[path[i]].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[path[i]] = next
		}
		m = next
	}
	m[path[len(path)-1]] = val
}

func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		if x == "" {
			return "(unset)"
		}
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
