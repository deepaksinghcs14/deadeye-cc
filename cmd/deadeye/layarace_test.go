package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/kernel"
)

// Concurrent hook requests are the daemon's normal state. This drives the
// real goroutine path (not the synchronous test seam) so -race can see the
// client cache, the outcome store and the async recorders under contention.
func TestLayaConcurrentHookLoad(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"answers":{"q":{"choice":"0","noul":0.9,"answer_confidence":0.9}}}`))
	}))
	defer srv.Close()
	st, _ := sampleHarness(t, 0, true)

	// Real goroutines, but tracked, so nothing outlives the test and races
	// with t.TempDir cleanup (which is a test artifact, not a code defect).
	var async sync.WaitGroup
	layaAsync = func(f func()) {
		async.Add(1)
		go func() { defer async.Done(); f() }()
	}
	dir := t.TempDir()

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cfg := layaCfg(layaShadow, srv.URL)
			d := kernel.Decision{Model: "top-id", Unsure: true}
			st.layaRouting(cfg, d, "task", "shape", "sess", dir)
			st.layaConfirmsGate(cfg, sitePlanGate, "p", "q?", "m", "sess", dir)
			layaClient(cfg)
		}()
	}
	wg.Wait()
	async.Wait()
}
