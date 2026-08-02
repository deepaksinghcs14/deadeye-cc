package lessons

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAdjustedThresholdUnaffectedByOtherShapes(t *testing.T) {
	outcomes := []Outcome{
		{TaskShape: "files=1,impl=true,tests=false", Kind: "escalation", Weight: 1.0},
	}
	got := AdjustedDownshiftThreshold(0.8, outcomes, "files=9+,impl=false,tests=true", time.Now())
	if got != 0.8 {
		t.Errorf("got %v, want unchanged base 0.8 for an unrelated shape", got)
	}
}

func TestAdjustedThresholdNeverSeenShapeIsUnchanged(t *testing.T) {
	got := AdjustedDownshiftThreshold(0.8, nil, "files=1,impl=true,tests=false", time.Now())
	if got != 0.8 {
		t.Errorf("got %v, want unchanged base 0.8 for a shape with no history", got)
	}
}

func TestAdjustedThresholdRisesWithEscalations(t *testing.T) {
	shape := "files=1,impl=true,tests=false"
	one := AdjustedDownshiftThreshold(0.8, []Outcome{
		{TaskShape: shape, Kind: "escalation", Weight: 1.0},
	}, shape, time.Now())
	many := AdjustedDownshiftThreshold(0.8, []Outcome{
		{TaskShape: shape, Kind: "escalation", Weight: 1.0},
		{TaskShape: shape, Kind: "escalation", Weight: 1.0},
		{TaskShape: shape, Kind: "escalation", Weight: 1.0},
		{TaskShape: shape, Kind: "escalation", Weight: 1.0},
	}, shape, time.Now())
	if !(one > 0.8) {
		t.Errorf("one escalation: threshold = %v, want > 0.8", one)
	}
	if !(many > one) {
		t.Errorf("more escalations should raise the threshold further: one=%v many=%v", one, many)
	}
	if many >= 1.0 {
		t.Errorf("threshold must stay below 1.0 (a never-satisfiable bar): got %v", many)
	}
}

func TestAdjustedThresholdNeverLowersBelowBase(t *testing.T) {
	shape := "files=1,impl=true,tests=false"
	// A shape with history but zero escalations (e.g. only "clean"
	// outcomes) must not lower the bar -- INV-1 only ever makes
	// downshifting harder here, never easier.
	got := AdjustedDownshiftThreshold(0.8, []Outcome{
		{TaskShape: shape, Kind: "clean", Weight: 0.05},
	}, shape, time.Now())
	if got < 0.8 {
		t.Errorf("got %v, threshold must never drop below base 0.8", got)
	}
}

// TestAdjustedThresholdIgnoresStaleEscalations is the regression test for
// C3: a single escalation used to raise the threshold to
// 0.8 + 0.2*(1/4) = 0.85 PERMANENTLY -- but the maximum confidence any
// builtin provider ever emits is 0.85 and the minimum is 0.8, so that one
// escalation made downshifting mathematically impossible for the shape
// forever, in every project, since outcomes.jsonl is global and never
// rotated. An escalation older than recencyWindow (30 days) must stop
// counting; one within the window must still raise the bar.
func TestAdjustedThresholdIgnoresStaleEscalations(t *testing.T) {
	shape := "files=1,impl=true,tests=false"
	now := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)

	stale := now.Add(-40 * 24 * time.Hour).Format(time.RFC3339)
	got := AdjustedDownshiftThreshold(0.8, []Outcome{
		{TaskShape: shape, TS: stale, Kind: "escalation", Weight: 1.0},
	}, shape, now)
	if got != 0.8 {
		t.Errorf("a 40-day-old escalation still raised the threshold: got %v, want unchanged base 0.8", got)
	}

	recent := now.Add(-1 * 24 * time.Hour).Format(time.RFC3339)
	got = AdjustedDownshiftThreshold(0.8, []Outcome{
		{TaskShape: shape, TS: recent, Kind: "escalation", Weight: 1.0},
	}, shape, now)
	if !(got > 0.8) {
		t.Errorf("a 1-day-old escalation did not raise the threshold: got %v, want > 0.8", got)
	}
}

// TestAdjustedThresholdTreatsUnparseableTimestampAsRecent: unknown routes
// up here same as everywhere else in this kernel -- an outcome with a
// missing/malformed TS must still count, not be silently ignored as if it
// never happened.
func TestAdjustedThresholdTreatsUnparseableTimestampAsRecent(t *testing.T) {
	shape := "files=1,impl=true,tests=false"
	got := AdjustedDownshiftThreshold(0.8, []Outcome{
		{TaskShape: shape, TS: "not-a-timestamp", Kind: "escalation", Weight: 1.0},
	}, shape, time.Now())
	if !(got > 0.8) {
		t.Errorf("an outcome with an unparseable timestamp was ignored: got %v, want > 0.8", got)
	}
}

func TestAppendAndScan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outcomes.jsonl")
	s := Open(path)
	want := Outcome{TS: "2026-08-02T00:00:00Z", SessionID: "s1", TaskShape: "files=1,impl=true,tests=false", Model: "cheap", Effort: "low", Kind: "escalation", Weight: 1.0}
	if err := s.Append(want); err != nil {
		t.Fatal(err)
	}
	got, err := Scan(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, want [%+v]", got, want)
	}
}

func TestScanMissingFileIsNotError(t *testing.T) {
	got, err := Scan(filepath.Join(t.TempDir(), "does-not-exist.jsonl"))
	if err != nil || got != nil {
		t.Errorf("got (%v, %v), want (nil, nil)", got, err)
	}
}
