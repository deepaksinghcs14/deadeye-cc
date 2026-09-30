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

	prevAsync, prevJudge := tierSampleAsync, judgeFunc
	tierSampleAsync = func(f func()) { f() } // synchronous: the test can assert after the call
	judgeFunc = func(string) (int, bool) { return judged, judgeOK }
	judgeCache.Clear()
	tierSampleSeen.Store(0)
	t.Cleanup(func() {
		tierSampleAsync, judgeFunc = prevAsync, prevJudge
		judgeCache.Clear()
		tierSampleSeen.Store(0)
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
