package main

import (
	"encoding/json"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
)

// A schema-invalid updatedInput fails the whole Agent call, so the cases that
// matter most here are the ones where effort must NOT be written: a client
// too old to know the parameter, a version deadeye couldn't read, and a
// caller who already chose.
func TestEffortEnforceRewritesOnlyWhenSafe(t *testing.T) {
	cases := []struct {
		name          string
		clientVersion string
		callerEffort  string
		effortMode    string
		routingMode   string
		wantEffort    bool
		wantModel     bool
	}{
		{"enforce on a new client", "2.1.292", "", "enforce", "advise", true, false},
		{"both axes enforce", "2.1.296", "", "enforce", "enforce", true, true},
		{"client predates the parameter", "2.1.291", "", "enforce", "advise", false, false},
		{"unknown client version", "", "", "enforce", "advise", false, false},
		{"unparseable client version", "2.1", "", "enforce", "advise", false, false},
		{"caller already chose effort", "2.1.296", "max", "enforce", "advise", false, false},
		{"advise never rewrites", "2.1.296", "", "advise", "advise", false, false},
		{"routing enforce alone leaves effort out", "2.1.296", "", "advise", "enforce", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]any{"description": "d", "prompt": "rename a variable in one file"}
			if tc.callerEffort != "" {
				raw["effort"] = tc.callerEffort
			}
			toolInput, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			in := hookio.Input{SessionID: "eff-" + tc.name, ToolName: "Agent", Cwd: t.TempDir(), ToolInput: toolInput, ClientVersion: tc.clientVersion}
			cfg := config.Default()
			cfg.Mode.RoutingJudge = "off"
			cfg.Mode.Effort = tc.effortMode
			cfg.Mode.Routing = tc.routingMode

			out := decideAgentRouting(in, cfg, newDaemonState(testCatalogForLessons(), nil))

			var got map[string]any
			if u := out.HookSpecificOutput.UpdatedInput; len(u) > 0 {
				if err := json.Unmarshal(u, &got); err != nil {
					t.Fatal(err)
				}
				// updatedInput replaces the input wholesale -- losing a
				// required field is the failure this guards against.
				if got["description"] != "d" || got["prompt"] == nil {
					t.Errorf("rewrite dropped original fields: %v", got)
				}
			}
			effort, hasEffort := got["effort"]
			if tc.callerEffort != "" {
				hasEffort = hasEffort && effort != tc.callerEffort
			}
			if hasEffort != tc.wantEffort {
				t.Errorf("effort rewritten = %v (%v), want %v", hasEffort, effort, tc.wantEffort)
			}
			if tc.wantEffort && !agentEfforts[effort.(string)] {
				t.Errorf("effort %q is outside the Agent tool's enum", effort)
			}
			if _, hasModel := got["model"]; hasModel != tc.wantModel {
				t.Errorf("model rewritten = %v, want %v", hasModel, tc.wantModel)
			}
			if !tc.wantEffort && !tc.wantModel && out.HookSpecificOutput.AdditionalContext == "" {
				t.Error("no rewrite and no advice: the recommendation was lost")
			}
		})
	}
}
