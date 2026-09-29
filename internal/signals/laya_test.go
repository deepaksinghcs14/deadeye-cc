package signals

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/laya"
)

func layaStub(t *testing.T, body string) *laya.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return laya.New(srv.URL, "", 2*time.Second)
}

func TestLayaComplexityIsNotABuiltin(t *testing.T) {
	// The default six-signal decision must be bit-for-bit what it was
	// before Laya existed; the provider is appended by the caller only on
	// the authoritative rung.
	for _, p := range Builtins() {
		if p.Name() == "laya" {
			t.Fatal("LayaComplexity must never be in Builtins()")
		}
	}
}

func TestLayaComplexityIsAQuietSkipper(t *testing.T) {
	var p Signal = LayaComplexity{}
	qs, ok := p.(quietSkipper)
	if !ok || !qs.QuietSkip() {
		t.Error("LayaComplexity must be a quietSkipper: absence is its normal state, not an evidence gap")
	}
}

func TestLayaComplexityNormalizesScore(t *testing.T) {
	cases := []struct {
		score float64
		want  float64
	}{
		{0, 0},     // lowest rubric level
		{1.5, 0.5}, // midpoint of a 4-level rubric (span 3)
		{3, 1},     // top level
		{4.5, 1},   // out of range clamps rather than exceeding 1
		{-2, 0},    // ditto below
	}
	for _, c := range cases {
		cl := layaStub(t, fmt.Sprintf(`{"answers":{"q":{"score":%v,"answer_confidence":0.9}}}`, c.score))
		e, err := LayaComplexity{Client: cl}.Assess(context.Background(), Scope{Prompt: "task"})
		if err != nil {
			t.Fatalf("score %v: unexpected error %v", c.score, err)
		}
		if e.Complexity != c.want {
			t.Errorf("score %v -> complexity %v, want %v", c.score, e.Complexity, c.want)
		}
	}
}

// The load-bearing guard: kernel.Decide takes the MINIMUM confidence across
// evidence, so a hedging classifier would drag every decision down and
// silently make routing pricier. Below the floor it must contribute nothing.
func TestLayaComplexitySkipsBelowCertaintyFloor(t *testing.T) {
	cl := layaStub(t, `{"answers":{"q":{"score":2,"answer_confidence":0.4}}}`)
	_, err := LayaComplexity{Client: cl}.Assess(context.Background(), Scope{Prompt: "task"})
	if err == nil {
		t.Fatal("a certainty below the floor must skip, not contribute low-confidence evidence")
	}
}

// Confidence may never exceed MaxAchievableConfidence: that constant is
// documented as the minimum of every provider's best case, and a bonus
// signal must not quietly invalidate it.
func TestLayaComplexityCapsConfidenceAtCeiling(t *testing.T) {
	cl := layaStub(t, `{"answers":{"q":{"score":2,"answer_confidence":0.99}}}`)
	e, err := LayaComplexity{Client: cl}.Assess(context.Background(), Scope{Prompt: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Confidence > MaxAchievableConfidence {
		t.Errorf("confidence %v exceeds MaxAchievableConfidence %v", e.Confidence, MaxAchievableConfidence)
	}
}

func TestLayaComplexityFailsOpen(t *testing.T) {
	cases := map[string]LayaComplexity{
		"nil client":  {Client: nil},
		"unreachable": {Client: laya.New("http://127.0.0.1:1", "", 200*time.Millisecond)},
	}
	for name, p := range cases {
		if _, err := p.Assess(context.Background(), Scope{Prompt: "task"}); err == nil {
			t.Errorf("%s: expected an error so the provider is skipped", name)
		}
	}
	// An empty prompt has nothing to classify.
	cl := layaStub(t, `{"answers":{"q":{"score":2,"answer_confidence":0.9}}}`)
	if _, err := (LayaComplexity{Client: cl}).Assess(context.Background(), Scope{Prompt: ""}); err == nil {
		t.Error("empty prompt should skip")
	}
}

// AssessAll must not record a skip penalty for this provider, and the
// evidence set must be unchanged when Laya contributes nothing.
func TestAssessAllUnaffectedByASilentLaya(t *testing.T) {
	scope := Scope{Prompt: "add a field to the config struct", Files: []string{"a.go"}}
	base := AssessAll(context.Background(), scope, Builtins())

	dead := LayaComplexity{Client: laya.New("http://127.0.0.1:1", "", 100*time.Millisecond)}
	with := AssessAll(context.Background(), scope, append(Builtins(), dead))

	if len(with) != len(base) {
		t.Errorf("a silent Laya changed the evidence set: %d items vs %d", len(with), len(base))
	}
	for i := range base {
		if with[i].Provider != base[i].Provider || with[i].Confidence != base[i].Confidence {
			t.Errorf("evidence %d differs: %+v vs %+v", i, with[i], base[i])
		}
	}
}

// A contributing Laya shows up as one extra evidence item, named, with its
// facts attached -- the kernel's reasoning stays inspectable.
func TestAssessAllIncludesContributingLaya(t *testing.T) {
	scope := Scope{Prompt: "rewrite the scheduler", Files: []string{"a.go"}}
	base := AssessAll(context.Background(), scope, Builtins())

	cl := layaStub(t, `{"answers":{"q":{"score":3,"answer_confidence":0.78}}}`)
	with := AssessAll(context.Background(), scope, append(Builtins(), LayaComplexity{Client: cl}))

	if len(with) != len(base)+1 {
		t.Fatalf("expected one extra evidence item, got %d vs %d", len(with), len(base))
	}
	// Found by name, never by position: AssessAll appends its own synthetic
	// "unknown" row recording skipped providers, so the last element is
	// whichever of the two came last -- which depends on whether gitchurn
	// had anything to say about the tree the test happens to run in.
	var got Evidence
	for _, e := range with {
		if e.Provider == "laya" {
			got = e
		}
	}
	if got.Provider != "laya" {
		t.Fatalf("no laya evidence in %+v", with)
	}
	if got.Complexity != 1 || got.Confidence != 0.78 {
		t.Errorf("laya evidence = %+v; want complexity 1, confidence 0.78", got)
	}
	if got.Facts["source"] != "laya (local classifier)" {
		t.Errorf("facts should name the source, got %v", got.Facts)
	}
}
