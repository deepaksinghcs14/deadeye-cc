package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/kernel"
	"github.com/deepaksinghcs14/deadeye-cc/internal/laya"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
)

// sampleHarness builds a daemonState writing to a temp outcomes log, with
// the async seam made synchronous and the judge stubbed, so the sampler's
// gating can be tested without a model call or a goroutine race.
func sampleHarness(t *testing.T, judged int, judgeOK bool) (*daemonState, string) {
	t.Helper()
	dir := t.TempDir()
	outPath := filepath.Join(dir, "outcomes.jsonl")
	st := &daemonState{
		cat:      testCatalogForLessons(),
		logs:     logstore.Open(filepath.Join(dir, "decisions.jsonl")),
		outcomes: lessons.Open(outPath),
		sessions: map[string]*sessionState{},
	}

	prevAsync, prevJudge, prevLaya := tierSampleAsync, judgeFunc, layaAsync
	tierSampleAsync = func(f func()) { f() } // synchronous: the test can assert after the call
	layaAsync = func(f func()) { f() }       // same, for the shadow-rung fire-and-forget
	judgeFunc = func(string) (int, bool) { return judged, judgeOK }
	judgeCache.Clear()
	tierSampleSeen.Store(0)
	t.Cleanup(func() {
		tierSampleAsync, judgeFunc, layaAsync = prevAsync, prevJudge, prevLaya
		judgeCache.Clear()
		tierSampleSeen.Store(0)
		layaClientMu.Lock()
		layaClientCache = map[string]*laya.Client{}
		layaClientMu.Unlock()
	})
	return st, outPath
}

func sampleCfg(mode string, rate int) config.Config {
	cfg := config.Default()
	cfg.Mode.TierSample = mode
	cfg.Mode.RoutingJudge = "on"
	cfg.TierSample.Rate = rate
	return cfg
}

// highTierDecision returns a confident decision on the catalog's top tier,
// which is the only shape the sampler is interested in.
func highTierDecision(t *testing.T, cat catalog.Catalog) kernel.Decision {
	t.Helper()
	m, ok := cat.HighCeiling()
	if !ok {
		t.Skip("test catalog has no high ceiling")
	}
	return kernel.Decision{Model: m.ID, Effort: "high", Confidence: 1, Unsure: false}
}

func disagreements(t *testing.T, path string) []lessons.Outcome {
	t.Helper()
	all, err := lessons.Scan(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []lessons.Outcome
	for _, o := range all {
		if o.Kind == KindTierDisagreement {
			out = append(out, o)
		}
	}
	return out
}

func TestMaybeSampleTierRecordsDisagreement(t *testing.T) {
	st, outPath := sampleHarness(t, 1, true) // judge says tier 1, kernel chose tier 2+
	cfg := sampleCfg("on", 1)                // rate 1 = sample every eligible decision
	d := highTierDecision(t, st.cat)

	st.maybeSampleTier(cfg, d, st.cat, "code-edit", "write a function", "sess", t.TempDir())

	got := disagreements(t, outPath)
	if len(got) != 1 {
		t.Fatalf("got %d disagreement outcomes, want 1", len(got))
	}
	if got[0].JudgedTier == nil || *got[0].JudgedTier != 1 {
		t.Errorf("JudgedTier = %v, want 1", got[0].JudgedTier)
	}
	if got[0].Surface != lessons.SurfaceRouting || got[0].Model != d.Model {
		t.Errorf("outcome did not carry the routed decision: %+v", got[0])
	}
}

// Every gate that must keep the sampler from spending a judge call.
func TestMaybeSampleTierGates(t *testing.T) {
	cases := []struct {
		name    string
		cfg     config.Config
		mutate  func(*kernel.Decision)
		wantOff bool
	}{
		{"sampling off", sampleCfg("off", 1), nil, true},
		{"judge off", func() config.Config { c := sampleCfg("on", 1); c.Mode.RoutingJudge = "off"; return c }(), nil, true},
		// An Unsure decision already went through the judge on the live
		// path; re-asking would only confirm the judge's own answer.
		{"unsure decision", sampleCfg("on", 1), func(d *kernel.Decision) { d.Unsure = true }, true},
		{"enabled and confident", sampleCfg("on", 1), nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, outPath := sampleHarness(t, 0, true)
			d := highTierDecision(t, st.cat)
			if c.mutate != nil {
				c.mutate(&d)
			}
			st.maybeSampleTier(c.cfg, d, st.cat, "shape", "prompt", "sess", t.TempDir())
			got := len(disagreements(t, outPath))
			if c.wantOff && got != 0 {
				t.Errorf("gate should have suppressed sampling, got %d outcomes", got)
			}
			if !c.wantOff && got == 0 {
				t.Error("expected a sampled disagreement, got none")
			}
		})
	}
}

// An empty prompt has nothing to judge, and the cheapest tier has nothing
// cheaper to disagree about.
func TestMaybeSampleTierSkipsEmptyPromptAndLowTier(t *testing.T) {
	st, outPath := sampleHarness(t, 0, true)
	cfg := sampleCfg("on", 1)

	st.maybeSampleTier(cfg, highTierDecision(t, st.cat), st.cat, "shape", "", "sess", t.TempDir())
	if n := len(disagreements(t, outPath)); n != 0 {
		t.Errorf("empty prompt sampled anyway (%d outcomes)", n)
	}

	cheap, ok := st.cat.Cheapest()
	if !ok {
		t.Skip("no cheapest model in test catalog")
	}
	st.maybeSampleTier(cfg, kernel.Decision{Model: cheap.ID, Confidence: 1}, st.cat, "shape", "prompt", "sess", t.TempDir())
	if n := len(disagreements(t, outPath)); n != 0 {
		t.Errorf("tier-0 decision sampled anyway (%d outcomes)", n)
	}
}

// The judge agreeing (or failing) records nothing: this report counts
// disagreements, and a silent judge must not look like agreement.
func TestMaybeSampleTierRecordsNothingWhenJudgeAgreesOrFails(t *testing.T) {
	for _, c := range []struct {
		name   string
		judged int
		ok     bool
	}{
		{"judge agrees at tier 2", 2, true},
		{"judge call failed", 1, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			st, outPath := sampleHarness(t, c.judged, c.ok)
			st.maybeSampleTier(sampleCfg("on", 1), highTierDecision(t, st.cat), st.cat, "shape", "prompt", "sess", t.TempDir())
			if n := len(disagreements(t, outPath)); n != 0 {
				t.Errorf("got %d outcomes, want none", n)
			}
		})
	}
}

// 1-in-N means one call in N, not every call: the sampler's whole cost
// control is this counter.
func TestMaybeSampleTierHonoursRate(t *testing.T) {
	st, outPath := sampleHarness(t, 1, true)
	cfg := sampleCfg("on", 3)
	d := highTierDecision(t, st.cat)
	for i := 0; i < 6; i++ {
		// Distinct prompts: the judge cache is keyed by task text, and a
		// repeated prompt would be answered from cache.
		st.maybeSampleTier(cfg, d, st.cat, "shape", "prompt "+string(rune('a'+i)), "sess", t.TempDir())
	}
	if n := len(disagreements(t, outPath)); n != 2 {
		t.Errorf("got %d samples from 6 eligible decisions at rate 3, want 2", n)
	}
}

// A rate of 0 from a partial config file must fall back to the default,
// never to "sample everything" -- this sampler spends money per call.
func TestMaybeSampleTierZeroRateFallsBackToDefault(t *testing.T) {
	st, outPath := sampleHarness(t, 1, true)
	cfg := sampleCfg("on", 0)
	d := highTierDecision(t, st.cat)
	for i := 0; i < 9; i++ {
		st.maybeSampleTier(cfg, d, st.cat, "shape", "p"+string(rune('a'+i)), "sess", t.TempDir())
	}
	if n := len(disagreements(t, outPath)); n != 0 {
		t.Errorf("rate 0 sampled %d of 9 calls; want 0 (default 1-in-10 not yet reached)", n)
	}
}

// INV-1: escalation bias is one-directional. A tier-disagreement must be
// inert in the threshold calculation, or this measurement would silently
// start steering routing before anyone trusted it.
func TestTierDisagreementDoesNotMoveDownshiftThreshold(t *testing.T) {
	now := time.Now()
	ts := now.Add(-time.Hour).Format(time.RFC3339)
	jt := 1
	// Below signals.MaxAchievableConfidence (0.8): at or above the ceiling
	// AdjustedDownshiftThreshold returns base unchanged by design, which
	// would make this test pass for the wrong reason.
	base := 0.6
	withDisagreements := []lessons.Outcome{
		{TS: ts, Surface: lessons.SurfaceRouting, TaskShape: "code-edit", Kind: KindTierDisagreement, Weight: 1, JudgedTier: &jt},
		{TS: ts, Surface: lessons.SurfaceRouting, TaskShape: "code-edit", Kind: KindTierDisagreement, Weight: 1, JudgedTier: &jt},
	}
	got := lessons.AdjustedDownshiftThreshold(base, withDisagreements, "code-edit", now)
	if got != base {
		t.Errorf("threshold moved to %v on disagreement outcomes alone; want the untouched base %v", got, base)
	}

	// Sanity check the other direction still works, so this test can't pass
	// because the whole mechanism is broken.
	esc := []lessons.Outcome{{TS: ts, Surface: lessons.SurfaceRouting, TaskShape: "code-edit", Kind: "escalation", Weight: 1}}
	if lessons.AdjustedDownshiftThreshold(base, esc, "code-edit", now) <= base {
		t.Error("an escalation no longer raises the threshold -- the mechanism under test is broken")
	}
}

// JudgedTier is new: every outcome written before it existed must still
// parse, or this field would quietly invalidate the whole history.
func TestOutcomeWithoutJudgedTierStillParses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outcomes.jsonl")
	legacy := `{"ts":"2026-01-01T00:00:00Z","surface":"routing","task_shape":"code-edit","kind":"escalation","weight":1}` + "\n"
	if err := writeFileString(path, legacy); err != nil {
		t.Fatal(err)
	}
	got, err := lessons.Scan(path)
	if err != nil || len(got) != 1 {
		t.Fatalf("legacy outcome failed to parse: %v (%d records)", err, len(got))
	}
	if got[0].JudgedTier != nil {
		t.Errorf("JudgedTier = %v on a legacy record, want nil", got[0].JudgedTier)
	}
	if !strings.Contains(legacy, "escalation") {
		t.Fatal("fixture drifted")
	}
}

func writeFileString(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}
