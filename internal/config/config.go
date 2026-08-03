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
	}
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
	}
	if isOff(off, "DEADEYE_PREPROCESS") {
		cfg.Mode.Preprocess = "off"
	}
	if isOff(off, "DEADEYE_GATE") {
		cfg.Mode.PlanGate = "off"
	}
	return cfg
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
var killSwitchVars = []string{"DEADEYE", "DEADEYE_PREPROCESS", "DEADEYE_GATE"}

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
