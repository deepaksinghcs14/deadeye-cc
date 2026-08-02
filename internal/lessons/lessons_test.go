package lessons

import (
	"path/filepath"
	"testing"
)

func TestAdjustedThresholdUnaffectedByOtherShapes(t *testing.T) {
	outcomes := []Outcome{
		{TaskShape: "files=1,impl=true,tests=false", Kind: "escalation", Weight: 1.0},
	}
	got := AdjustedDownshiftThreshold(0.8, outcomes, "files=9+,impl=false,tests=true")
	if got != 0.8 {
		t.Errorf("got %v, want unchanged base 0.8 for an unrelated shape", got)
	}
}

func TestAdjustedThresholdNeverSeenShapeIsUnchanged(t *testing.T) {
	got := AdjustedDownshiftThreshold(0.8, nil, "files=1,impl=true,tests=false")
	if got != 0.8 {
		t.Errorf("got %v, want unchanged base 0.8 for a shape with no history", got)
	}
}

func TestAdjustedThresholdRisesWithEscalations(t *testing.T) {
	shape := "files=1,impl=true,tests=false"
	one := AdjustedDownshiftThreshold(0.8, []Outcome{
		{TaskShape: shape, Kind: "escalation", Weight: 1.0},
	}, shape)
	many := AdjustedDownshiftThreshold(0.8, []Outcome{
		{TaskShape: shape, Kind: "escalation", Weight: 1.0},
		{TaskShape: shape, Kind: "escalation", Weight: 1.0},
		{TaskShape: shape, Kind: "escalation", Weight: 1.0},
		{TaskShape: shape, Kind: "escalation", Weight: 1.0},
	}, shape)
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
	}, shape)
	if got < 0.8 {
		t.Errorf("got %v, threshold must never drop below base 0.8", got)
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
