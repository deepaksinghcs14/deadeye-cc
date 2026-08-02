// Package config loads ~/.deadeye/config.json overlaid by project-level
// .deadeye.json, per PLAN.md §7. A missing or malformed config file is not
// an error -- INV-5 (fail open) applies to configuration too: the zero
// value is always a working, conservative default.
package config

import (
	"encoding/json"
	"os"

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

// PlanGate is Phase 4's trigger tuning.
type PlanGate struct {
	MinFiles      int  `json:"min_files"`
	RadiusTrigger bool `json:"radius_trigger"`
}

// Config mirrors schema/config.schema.json. `tiers.override` is
// deliberately not modeled here: internal/catalog.Load() already reads
// ~/.deadeye/catalog.json directly as a wholesale override, so a second
// pointer to the same file in Config would just be a second source of
// truth for one fact.
type Config struct {
	Mode                  Modes      `json:"mode"`
	DownshiftThreshold    float64    `json:"downshift_threshold"`
	Posture               string     `json:"posture"`
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
		Posture:               "balanced",
		InjectionBudgetTokens: 400,
		PlanGate:              PlanGate{MinFiles: 2, RadiusTrigger: true},
	}
}

// Load returns Default() overlaid by ~/.deadeye/config.json, overlaid by
// ./.deadeye.json relative to cwd. Either file may be absent.
func Load() Config {
	cfg := Default()
	overlay(&cfg, meta.ConfigPath())
	overlay(&cfg, ".deadeye.json")
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

// KillSwitchOff reports whether the given env var is set to exactly "off".
// Used for DEADEYE, DEADEYE_PREPROCESS, DEADEYE_GATE.
func KillSwitchOff(envVar string) bool {
	return os.Getenv(envVar) == "off"
}
