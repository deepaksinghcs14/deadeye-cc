// Package config loads ~/.deadeye/config.json overlaid by project-level
// .deadeye.json, per PLAN.md §7. A missing or malformed config file is not
// an error -- INV-5 (fail open) applies to configuration too: the zero
// value is always a working, conservative default.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// Modes holds the per-axis enforcement posture. Values are "off", "advise",
// or "enforce" for routing/effort; preprocess/plan_gate/workflow_hint keep
// their own small vocabularies per PLAN.md §7's example config.
type Modes struct {
	Routing      string `json:"routing"`
	Effort       string `json:"effort"`
	Preprocess   string `json:"preprocess"`
	PlanGate     string `json:"plan_gate"`
	WorkflowHint string `json:"workflow_hint"`
}

// Preprocess is per-rule config for internal/preprocess.
type Preprocess struct {
	DisabledRules []string `json:"disabled_rules"`
}

// PlanGate is Phase 4's trigger tuning. (A radius_trigger knob used to sit
// alongside MinFiles -- deleted: nothing ever read it, so it was a
// documented setting that silently did nothing. The blast-radius signal it
// promised needs the optional greybeard provider this plugin deliberately
// doesn't depend on; re-add the knob when that signal actually exists.)
type PlanGate struct {
	MinFiles int `json:"min_files"`
}

// Coder configures the coder-mode persona (see internal/coder;
// third-party notice in THIRD-PARTY.md). DefaultLevel is what new sessions start at
// (off|spotter|marksman|sniper -- never review, which is session-only).
// SubagentMatcher optionally scopes subagent injection to agent types
// matching the regex ("" = all). InjectionBudgetTokens is the persona's
// own logged ceiling, separate from the 400-token advisory budget: the
// ruleset is an explicit opt-in and byte-stable per level, so INV-4's
// no-silent-creep intent holds via its own ceiling rather than by
// squeezing under the advisory one.
type Coder struct {
	DefaultLevel          string `json:"default_level"`
	SubagentMatcher       string `json:"subagent_matcher"`
	InjectionBudgetTokens int    `json:"injection_budget_tokens"`

	// Disabled is set by LoadFor when a kill switch (DEADEYE or
	// DEADEYE_CODER) is engaged for the current request -- distinct from
	// DefaultLevel because the switch must silence an already-active
	// SESSION level too, not just stop new sessions from starting one.
	Disabled bool `json:"-"`
}

// Config mirrors schema/config.schema.json. `tiers.override` is
// deliberately not modeled here: internal/catalog.Load() already reads
// ~/.deadeye/catalog.json directly as a wholesale override, so a second
// pointer to the same file in Config would just be a second source of
// truth for one fact. (A `posture` preset field used to live here too --
// deleted: no code path ever read it, so setting posture: "frugal"
// changed nothing but the status printout. A preset that only exists to
// be echoed back is worse than no preset.)
type Config struct {
	Mode                  Modes      `json:"mode"`
	DownshiftThreshold    float64    `json:"downshift_threshold"`
	InjectionBudgetTokens int        `json:"injection_budget_tokens"`
	Preprocess            Preprocess `json:"preprocess"`
	PlanGate              PlanGate   `json:"plan_gate"`
	Coder                 Coder      `json:"coder"`
}

// DisabledRuleSet returns Preprocess.DisabledRules as a lookup set for
// internal/preprocess.Apply.
func (c Config) DisabledRuleSet() map[string]bool {
	if len(c.Preprocess.DisabledRules) == 0 {
		return nil
	}
	set := make(map[string]bool, len(c.Preprocess.DisabledRules))
	for _, r := range c.Preprocess.DisabledRules {
		set[r] = true
	}
	return set
}

// Default returns the out-of-the-box posture: advisory everywhere, nothing
// enforced, nothing silently disabled.
func Default() Config {
	return Config{
		Mode: Modes{
			Routing:      "advise",
			Effort:       "advise",
			Preprocess:   "on",
			PlanGate:     "soft",
			WorkflowHint: "on",
		},
		DownshiftThreshold:    0.8,
		InjectionBudgetTokens: 400,
		PlanGate:              PlanGate{MinFiles: 2},
		Coder: Coder{
			DefaultLevel:          "marksman",
			SubagentMatcher:       "",
			InjectionBudgetTokens: 1600,
		},
	}
}

// WriteDefaultIfMissing creates ~/.deadeye/config.json with every default
// spelled out, so users tweak an existing file instead of writing one from
// scratch (nothing else ever creates it -- installing the plugin left users
// to discover the file format on their own). O_EXCL: an existing file is
// never touched, whatever its content. Best-effort -- a failure just means
// the user creates the file themselves, same as before this existed.
func WriteDefaultIfMissing() {
	f, err := os.OpenFile(meta.ConfigPath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	// The $schema pointer gives schema-aware editors validation and
	// autocomplete on the knobs. json.Unmarshal ignores it on load.
	seed := Default()
	// nil marshals as `null`; an empty list reads better in a file that
	// exists specifically to be hand-edited.
	seed.Preprocess.DisabledRules = []string{}
	out := struct {
		Schema string `json:"$schema"`
		Config
	}{
		Schema: "https://raw.githubusercontent.com/deepaksinghcs14/deadeye-cc/main/schema/config.schema.json",
		Config: seed,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return
	}
	f.Write(append(b, '\n'))
}

// EnsureCoderBlock adds the coder section, with its defaults spelled out,
// to a config.json seeded before coder mode existed (pre-0.5.0) -- the
// setting always applied via Default(), but the knob wasn't visible in
// the file users open to tweak. Read-modify-write on the raw JSON so
// unknown fields and the $schema pointer survive; a file that already has
// any coder key is left untouched (it's the user's, whatever it says).
func EnsureCoderBlock() {
	path := meta.ConfigPath()
	b, err := os.ReadFile(path)
	if err != nil {
		return // no file: WriteDefaultIfMissing owns seeding
	}
	raw := map[string]any{}
	if json.Unmarshal(b, &raw) != nil {
		return // malformed: not ours to rewrite
	}
	if _, has := raw["coder"]; has {
		return
	}
	seed := Default().Coder
	raw["coder"] = map[string]any{
		"default_level":           seed.DefaultLevel,
		"subagent_matcher":        seed.SubagentMatcher,
		"injection_budget_tokens": seed.InjectionBudgetTokens,
	}
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, append(out, '\n'), 0o600)
}

// Load returns Default() overlaid by ~/.deadeye/config.json, overlaid by
// ./.deadeye.json relative to this PROCESS's own cwd. Correct for the CLI
// commands (status/route) -- each is a fresh, short-lived process already
// running in the directory the user cares about. Never call this from
// inside the daemon: see LoadFor.
func Load() Config {
	cfg := Default()
	overlay(&cfg, meta.ConfigPath())
	overlay(&cfg, ".deadeye.json")
	return cfg
}

// LoadFor is Load, but for the daemon: cwd is the SESSION's working
// directory (from the hook payload), not this process's own, and off is
// the client's env-derived kill switches (see OffSwitches) folded in as
// config rather than checked separately. Both matter because the daemon
// is one long-lived process serving every project and session it's asked
// about, spawned from whichever directory and environment happened to
// start it -- verified live: a project's own .deadeye.json previously
// governed every OTHER project's sessions too for as long as that daemon
// stayed up (its idle timeout resets on every connection, so in practice
// indefinitely), and DEADEYE_PREPROCESS=off set in a later shell had no
// effect at all, while a fresh `deadeye status` process (which reads its
// own real env) reported the switch as engaged. Call this once per
// request, not once at daemon startup.
func LoadFor(cwd string, off []string) Config {
	cfg := Default()
	overlay(&cfg, meta.ConfigPath())
	if cwd != "" {
		overlay(&cfg, filepath.Join(cwd, ".deadeye.json"))
	}
	if isOff(off, "DEADEYE") {
		cfg.Mode.Routing = "off"
		cfg.Mode.Effort = "off"
		cfg.Mode.Preprocess = "off"
		cfg.Mode.PlanGate = "off"
		cfg.Mode.WorkflowHint = "off"
		cfg.Coder.Disabled = true
	}
	if isOff(off, "DEADEYE_PREPROCESS") {
		cfg.Mode.Preprocess = "off"
	}
	if isOff(off, "DEADEYE_GATE") {
		cfg.Mode.PlanGate = "off"
	}
	if isOff(off, "DEADEYE_CODER") {
		cfg.Coder.Disabled = true
	}
	return cfg
}

// WriteCoderDefault persists level as coder.default_level in deadeye's OWN
// ~/.deadeye/config.json ("/deadeye-coder default <level>"). Read-modify-
// write on the raw JSON so unknown fields and the $schema pointer survive.
// This never touches Claude's settings.json -- deadeye only ever writes
// its own state dir.
func WriteCoderDefault(level string) error {
	path := meta.ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &raw) // malformed existing file: start fresh below
	}
	coderRaw, _ := raw["coder"].(map[string]any)
	if coderRaw == nil {
		coderRaw = map[string]any{}
	}
	coderRaw["default_level"] = level
	raw["coder"] = coderRaw
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func overlay(cfg *Config, path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	// Best-effort: a malformed override leaves whatever fields it managed
	// to parse before failing; encoding/json fills in what it can up to
	// the error, so this can partially apply. That's acceptable here --
	// the invariant is "never worse than Default()", not "atomic apply".
	_ = json.Unmarshal(b, cfg)
}

// killSwitchVars is the fixed set of env-var kill switches checked by
// OffSwitches.
var killSwitchVars = []string{"DEADEYE", "DEADEYE_PREPROCESS", "DEADEYE_GATE", "DEADEYE_CODER"}

// OffSwitches reports which of the three env-var kill switches are set to
// exactly "off" in THIS process's environment. Meant to be called
// client-side (the CLI process invoked per hook call, which has the
// user's real, current environment) and carried across the wire in
// proto.Request.Off -- the daemon must never call os.Getenv for these
// itself; see LoadFor's comment for why.
func OffSwitches() []string {
	var off []string
	for _, v := range killSwitchVars {
		if os.Getenv(v) == "off" {
			off = append(off, v)
		}
	}
	return off
}

func isOff(off []string, name string) bool {
	for _, o := range off {
		if o == name {
			return true
		}
	}
	return false
}
