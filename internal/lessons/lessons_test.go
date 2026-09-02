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

// TestAdjustedThresholdIgnoresNonRoutingSurface is the regression test for
// the Phase A generalization: a coder-miss or review-false-positive
// outcome must never move routing's downshift threshold, even when it
// happens to share a TaskShape string with a routing outcome (the two
// surfaces use unrelated shape vocabularies, but nothing stops them from
// colliding by coincidence).
func TestAdjustedThresholdIgnoresNonRoutingSurface(t *testing.T) {
	shape := "files=1,impl=true,tests=false"
	got := AdjustedDownshiftThreshold(0.8, []Outcome{
		{Surface: SurfaceCoder, TaskShape: shape, Kind: "coder-miss", Weight: 1.0},
		{Surface: SurfacePRReview, TaskShape: shape, Kind: "review-false-positive", Weight: 1.0},
	}, shape, time.Now())
	if got != 0.8 {
		t.Errorf("a coder/pr-review outcome moved routing's threshold: got %v, want unchanged base 0.8", got)
	}
}

// TestAdjustedThresholdTreatsEmptySurfaceAsRouting: every outcome recorded
// before Surface existed has Surface=="" on disk. Those rows must keep
// counting as routing escalations, not silently stop mattering.
func TestAdjustedThresholdTreatsEmptySurfaceAsRouting(t *testing.T) {
	shape := "files=1,impl=true,tests=false"
	got := AdjustedDownshiftThreshold(0.8, []Outcome{
		{TaskShape: shape, Kind: "escalation", Weight: 1.0}, // Surface left zero-value, as pre-existing rows are
	}, shape, time.Now())
	if !(got > 0.8) {
		t.Errorf("a pre-existing (Surface-less) escalation stopped counting: got %v, want > 0.8", got)
	}
}

func TestRecentShapesRanksByWeightDescending(t *testing.T) {
	now := time.Now()
	outcomes := []Outcome{
		{Surface: SurfaceCoder, Repo: "deadeye-cc", TaskShape: "security:inject", Weight: 1.0, TS: now.Format(time.RFC3339)},
		{Surface: SurfaceCoder, Repo: "deadeye-cc", TaskShape: "security:inject", Weight: 1.0, TS: now.Format(time.RFC3339)},
		{Surface: SurfaceCoder, Repo: "deadeye-cc", TaskShape: "correctness:leak", Weight: 1.0, TS: now.Format(time.RFC3339)},
	}
	got := RecentShapes(outcomes, SurfaceCoder, "deadeye-cc", now, 3)
	if len(got) != 2 || got[0].Shape != "security:inject" || got[0].Count != 2 || got[1].Shape != "correctness:leak" {
		t.Errorf("got %+v, want security:inject (count 2) ranked above correctness:leak (count 1)", got)
	}
}

// TestRecentShapesScopedToSurfaceAndRepo: an outcome from a different
// surface or a different repo must never leak into another repo's or
// surface's ranking -- coder-miss/review-false-positive are per-repo
// signals by design (docs/PRD-lessons.md §5), unlike routing's global
// shape.
func TestRecentShapesScopedToSurfaceAndRepo(t *testing.T) {
	now := time.Now()
	outcomes := []Outcome{
		{Surface: SurfaceCoder, Repo: "other-repo", TaskShape: "security:inject", Weight: 1.0, TS: now.Format(time.RFC3339)},
		{Surface: SurfacePRReview, Repo: "deadeye-cc", TaskShape: "security:inject", Weight: 1.0, TS: now.Format(time.RFC3339)},
	}
	got := RecentShapes(outcomes, SurfaceCoder, "deadeye-cc", now, 3)
	if len(got) != 0 {
		t.Errorf("got %+v, want none -- both outcomes are out of scope (wrong repo, wrong surface)", got)
	}
}

// TestRecentShapesCapsAtN: the injected "recent misses" line must stay
// bounded no matter how many distinct shapes accumulate -- an unbounded
// list is the exact failure mode INV-4 and the coder persona's own
// no-scaffolding philosophy warn against.
func TestRecentShapesCapsAtN(t *testing.T) {
	now := time.Now()
	var outcomes []Outcome
	for _, shape := range []string{"security:inject", "correctness:leak", "correctness:race", "perf:alloc"} {
		outcomes = append(outcomes, Outcome{Surface: SurfaceCoder, Repo: "deadeye-cc", TaskShape: shape, Weight: 1.0, TS: now.Format(time.RFC3339)})
	}
	got := RecentShapes(outcomes, SurfaceCoder, "deadeye-cc", now, 2)
	if len(got) != 2 {
		t.Errorf("got %d shapes, want capped at 2", len(got))
	}
}

// TestRecentShapesIgnoresStale reuses AdjustedDownshiftThreshold's
// recencyWindow rather than a new coefficient -- a miss older than 30 days
// must fall out of the ranking so it doesn't nag forever.
func TestRecentShapesIgnoresStale(t *testing.T) {
	now := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	stale := now.Add(-40 * 24 * time.Hour).Format(time.RFC3339)
	got := RecentShapes([]Outcome{
		{Surface: SurfaceCoder, Repo: "deadeye-cc", TaskShape: "security:inject", Weight: 1.0, TS: stale},
	}, SurfaceCoder, "deadeye-cc", now, 3)
	if len(got) != 0 {
		t.Errorf("got %+v, want a 40-day-old miss excluded", got)
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
