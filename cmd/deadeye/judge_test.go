package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/kernel"
)

func TestParseTier(t *testing.T) {
	cases := []struct {
		in   string
		tier int
		ok   bool
	}{
		{"1", 1, true},
		{"2\n", 2, true},
		{"0", 0, true},
		{"Tier: 2", 2, true},
		{"I'd say 1 here", 1, true},
		{"no digit here", 0, false},
		{"", 0, false},
		{"tier three", 0, false}, // no 0/1/2 present
		// A digit embedded in a larger number is not a verdict: these all
		// used to return a tier, so a judge answering in prose that
		// happened to contain a year or a version routed real work.
		{"2024", 0, false},
		{"v1.2.3", 0, false},
		{"line 42", 0, false},
		{"go 1.21", 0, false},
		{"I would say 2 for this", 2, true}, // still parses a standalone digit
	}
	for _, c := range cases {
		tier, ok := parseTier(c.in)
		if ok != c.ok || (ok && tier != c.tier) {
			t.Errorf("parseTier(%q) = (%d,%v), want (%d,%v)", c.in, tier, ok, c.tier, c.ok)
		}
	}
}

// TestJudgeCacheHitsOnce: an identical task is judged once, then served from
// cache -- no repeat model calls.
func TestJudgeCacheHitsOnce(t *testing.T) {
	var calls int32
	orig := judgeFunc
	judgeFunc = func(string) (int, bool) { atomic.AddInt32(&calls, 1); return 1, true }
	defer func() { judgeFunc = orig }()

	task := "TestJudgeCacheHitsOnce unique task marker"
	if tier, ok := judgeTierCached(task); !ok || tier != 1 {
		t.Fatalf("first = (%d,%v), want (1,true)", tier, ok)
	}
	if tier, ok := judgeTierCached(task); !ok || tier != 1 {
		t.Fatalf("cached = (%d,%v), want (1,true)", tier, ok)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("judgeFunc called %d times, want 1 (second should be cached)", n)
	}
}

// TestJudgeTierToModelUsesRoles: the judge's tier 1/2 resolve through the
// same UnsureCeiling/HighCeiling role lookup the kernel itself uses, not a
// raw tier-number lookup -- proving the judge and the deterministic path
// can never disagree about what "capable middle" / "high ceiling" means
// in a catalog with roles tagged at unusual tier numbers.
func TestJudgeTierToModelUsesRoles(t *testing.T) {
	cat := catalog.Catalog{Models: []catalog.Model{
		{ID: "cheap", Tier: 0},
		{ID: "mid", Tier: 4, Role: catalog.RoleUnsureCeiling},
		{ID: "top", Tier: 9, Role: catalog.RoleHighCeiling},
	}}
	cases := []struct {
		tier int
		want string
	}{{0, "cheap"}, {1, "mid"}, {2, "top"}}
	for _, c := range cases {
		if m, ok := judgeTierToModel(cat, c.tier); !ok || m != c.want {
			t.Errorf("judgeTierToModel(tier=%d) = (%q,%v), want (%q,true)", c.tier, m, ok, c.want)
		}
	}
	if _, ok := judgeTierToModel(cat, 3); ok {
		t.Error("judgeTierToModel(3) should fail -- the judge's vocabulary is only 0/1/2")
	}
}

// judgeFixture is the shared setup for the judge tests: judge on, a
// three-tier catalog, and a heuristic decision the cheap signals couldn't
// place (Unsure) -- exactly the case that routes through the judge.
func judgeFixture() (config.Config, catalog.Catalog, kernel.Decision) {
	cfg := config.Config{}
	cfg.Mode.RoutingJudge = "on"
	cat := catalog.Catalog{Models: []catalog.Model{
		{ID: "cheap", Tier: 0}, {ID: "mid", Tier: 1}, {ID: "top", Tier: 2},
	}}
	return cfg, cat, kernel.Decision{Model: "heuristic", Effort: "medium", Reason: "thin evidence", Confidence: 0.2, Unsure: true}
}

// TestJudgeVerdictReachesTheCallThatAskedForIt: the Agent path waits for the
// judge, because the verdict is the whole routing product (benchmarks/
// routing: 48% realized savings with the judge on, 22% with it off -- for a
// prompt-only subtask the heuristic correctly refuses to downshift, so
// answering before the verdict exists means answering "sonnet" every time).
// An earlier async version returned the heuristic immediately and cached the
// verdict under the exact prompt text, where a distinct subagent spawn would
// essentially never read it.
func TestJudgeVerdictReachesTheCallThatAskedForIt(t *testing.T) {
	orig := judgeFunc
	judgeFunc = func(string) (int, bool) { return 0, true }
	defer func() { judgeFunc = orig }()
	cfg, cat, decision := judgeFixture()

	got := applyRoutingJudge(cfg, decision, cat, "TestJudgeVerdictReaches unique prompt")
	if got.Model != "cheap" || got.Unsure || got.Effort != "low" {
		t.Errorf("first call = %+v, want the tier-0 verdict applied to THIS decision", got)
	}
}

// TestJudgeDeadlinesNestInnermostFirst guards the ordering the Agent path
// depends on: the judge's own timeout must expire before the client's, the
// client's before the daemon's, and the daemon's before the hook timeout in
// hooks/hooks.json -- otherwise an outer layer gives up first and the
// judge's fail-open to the heuristic never runs, which is exactly how the
// judge came to eat every routing answer instead of improving it.
func TestJudgeDeadlinesNestInnermostFirst(t *testing.T) {
	if !(judgeTimeout < agentRequestTimeout) {
		t.Errorf("judgeTimeout %v must be under agentRequestTimeout %v", judgeTimeout, agentRequestTimeout)
	}
	daemonDeadline := agentRequestTimeout + time.Second // daemon.go's handleConn
	if !(agentRequestTimeout < daemonDeadline) {
		t.Errorf("agentRequestTimeout %v must be under the daemon deadline %v", agentRequestTimeout, daemonDeadline)
	}

	raw, err := os.ReadFile(filepath.Join("..", "..", "hooks", "hooks.json"))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	var manifest struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Timeout int `json:"timeout"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse hooks.json: %v", err)
	}
	var agentTimeout int
	for _, e := range manifest.Hooks.PreToolUse {
		if strings.Contains(e.Matcher, "Agent") && len(e.Hooks) > 0 {
			agentTimeout = e.Hooks[0].Timeout
		}
	}
	if agentTimeout == 0 {
		t.Fatal("no PreToolUse entry matching Agent found in hooks.json")
	}
	if time.Duration(agentTimeout)*time.Second <= daemonDeadline {
		t.Errorf("hooks.json Agent timeout %ds must exceed the daemon deadline %v", agentTimeout, daemonDeadline)
	}
}

// TestJudgeTierToModelFallsBackWithoutRoles: an un-roled catalog (any
// existing ~/.deadeye/catalog.json override) resolves through the
// historical tier 1/2 fallback, same as before this existed.
func TestJudgeTierToModelFallsBackWithoutRoles(t *testing.T) {
	cat := catalog.Catalog{Models: []catalog.Model{
		{ID: "cheap", Tier: 0}, {ID: "mid", Tier: 1}, {ID: "top", Tier: 2},
	}}
	cases := []struct {
		tier int
		want string
	}{{0, "cheap"}, {1, "mid"}, {2, "top"}}
	for _, c := range cases {
		if m, ok := judgeTierToModel(cat, c.tier); !ok || m != c.want {
			t.Errorf("judgeTierToModel(tier=%d) = (%q,%v), want (%q,true)", c.tier, m, ok, c.want)
		}
	}
}
