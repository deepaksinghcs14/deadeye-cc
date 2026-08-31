package counter

import (
	"sync"
	"testing"
)

func TestBenchCounterBasic(t *testing.T) {
	c := New()
	c.Add("a")
	c.Add("a")
	c.Add("b")
	if got := c.Total("a"); got != 2 {
		t.Errorf("Total(a)=%d want 2", got)
	}
	if got := c.Total("b"); got != 1 {
		t.Errorf("Total(b)=%d want 1", got)
	}
	if got := c.Total("unseen"); got != 0 {
		t.Errorf("Total(unseen)=%d want 0", got)
	}
}

// Exact totals under heavy contention. An unsynchronized map both trips -race
// and loses updates, so this fails on correctness even without the race flag.
func TestBenchCounterConcurrent(t *testing.T) {
	c := New()
	const goroutines, iters, keys = 100, 1000, 10
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				c.Add(string(rune('a' + (j % keys))))
			}
		}()
	}
	wg.Wait()
	want := goroutines * iters / keys // 10000 per key
	for k := 0; k < keys; k++ {
		key := string(rune('a' + k))
		if got := c.Total(key); got != want {
			t.Errorf("Total(%q)=%d want %d (lost updates)", key, got, want)
		}
	}
}
