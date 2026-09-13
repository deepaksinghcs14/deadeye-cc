package main

import (
	"strings"
	"sync"
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

// judgeFixture is the shared setup for the async-judge tests: judge on, a
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

// TestJudgeAsyncReturnsHeuristicThenCachedVerdict is the regression guard
// for the bug where routing advice never reached Claude when the judge
// ran: the hook path (wait=false) must return the heuristic decision
// immediately -- well inside the ~200ms client deadline -- labelled as
// pending, and an identical later call must get the judge's verdict from
// the cache once the background call lands.
func TestJudgeAsyncReturnsHeuristicThenCachedVerdict(t *testing.T) {
	release := make(chan struct{})
	orig := judgeFunc
	judgeFunc = func(string) (int, bool) { <-release; return 1, true }
	defer func() { judgeFunc = orig }()
	cfg, cat, decision := judgeFixture()
	prompt := "TestJudgeAsyncReturnsHeuristicThenCachedVerdict unique prompt"

	start := time.Now()
	first := applyRoutingJudge(cfg, decision, cat, prompt, false)
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("hook-path judge blocked for %v; must return before the client deadline", d)
	}
	if first.Model != "heuristic" || !first.Unsure {
		t.Fatalf("first call = %+v, want the untouched heuristic decision", first)
	}
	if !strings.Contains(first.Reason, "judge pending") {
		t.Errorf("first call reason %q should say the judge is pending", first.Reason)
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := applyRoutingJudge(cfg, decision, cat, prompt, false)
		if got.Model == "mid" && !got.Unsure && got.Confidence == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("judge verdict never landed in the cache; last = %+v", got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestJudgeAsyncDedupesInflight: N concurrent spawns of the same subtask
// start ONE background `claude -p`, not N.
func TestJudgeAsyncDedupesInflight(t *testing.T) {
	var calls int32
	release := make(chan struct{})
	orig := judgeFunc
	judgeFunc = func(string) (int, bool) { atomic.AddInt32(&calls, 1); <-release; return 0, true }
	defer func() { judgeFunc = orig }()
	cfg, cat, decision := judgeFixture()
	prompt := "TestJudgeAsyncDedupesInflight unique prompt"

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); applyRoutingJudge(cfg, decision, cat, prompt, false) }()
	}
	wg.Wait()
	close(release)
	// Let the single in-flight goroutine finish before counting.
	time.Sleep(20 * time.Millisecond)
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("judgeFunc called %d times for 5 concurrent identical spawns, want 1", n)
	}
}

// TestJudgeWaitPathStillSynchronous: /deadeye-route (wait=true) is a CLI
// dry-run that can afford to wait -- it must show the verdict on the first
// call, or its explanation diverges from what the cache will later serve.
func TestJudgeWaitPathStillSynchronous(t *testing.T) {
	orig := judgeFunc
	judgeFunc = func(string) (int, bool) { return 2, true }
	defer func() { judgeFunc = orig }()
	cfg, cat, decision := judgeFixture()
	got := applyRoutingJudge(cfg, decision, cat, "TestJudgeWaitPathStillSynchronous unique prompt", true)
	if got.Model != "top" || got.Unsure || got.Effort != "high" {
		t.Errorf("wait=true first call = %+v, want the tier-2 verdict applied synchronously", got)
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
