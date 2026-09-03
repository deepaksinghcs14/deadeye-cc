package main

import (
	"sync/atomic"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
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
