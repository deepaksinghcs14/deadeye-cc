package main

import (
	"sync/atomic"
	"testing"
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
