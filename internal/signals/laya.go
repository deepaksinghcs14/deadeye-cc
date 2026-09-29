package signals

import (
	"context"
	"fmt"

	"github.com/deepaksinghcs14/deadeye-cc/internal/laya"
)

// layaFloor is the certainty a Laya answer needs before it counts as
// evidence at all.
//
// Below it the provider skips QUIETLY rather than contributing a
// low-confidence estimate, and that is the load-bearing detail:
// kernel.Decide takes the MINIMUM confidence across all evidence, so a
// hedging classifier would drag the whole decision's confidence down and
// silently make every route more expensive. A signal that isn't sure has
// nothing to add here; it must not be able to tax the ones that are.
const layaFloor = 0.6

// layaLevels is the ordinal rubric. Four levels, lowest first, so the
// normalized score lands on a 0..1 complexity scale the kernel already
// speaks.
var layaLevels = []string{
	"trivial: a one-line or mechanical edit, a lookup, a rename",
	"routine: one self-contained function, file, or package from a clear spec",
	"involved: spans several files or existing code whose behavior must be inferred",
	"hard: architecture decisions, subtle debugging, or security-critical work",
}

// LayaComplexity is an OPTIONAL seventh signal: a local classifier's
// estimate of how much capability a task needs, alongside the six cheap
// heuristics.
//
// It is never in Builtins(). The caller appends it only when mode.laya is
// on the authoritative rung, which keeps two properties true: the default
// six-signal decision is bit-for-bit what it was before Laya existed, and a
// classifier being merely *recorded* (shadow/advise) can never reach the
// kernel.
//
// It is a quietSkipper because absence is its normal state -- unreachable,
// unsure, or simply not configured are all "no extra information", not the
// evidence GAP that AssessAll's skip penalty exists to catch.
type LayaComplexity struct {
	Client *laya.Client
}

func (LayaComplexity) Name() string { return "laya" }

// QuietSkip marks this a bonus signal: skipping is expected, not a gap.
func (LayaComplexity) QuietSkip() bool { return true }

func (l LayaComplexity) Assess(ctx context.Context, s Scope) (Evidence, error) {
	if l.Client == nil || s.Prompt == "" {
		return Evidence{}, fmt.Errorf("laya: not configured")
	}
	a, ok := l.Client.Score(ctx, s.Prompt, "How much engineering capability does this software task need?", layaLevels)
	if !ok {
		return Evidence{}, fmt.Errorf("laya: no answer")
	}
	certainty := a.Certainty()
	if certainty < layaFloor {
		return Evidence{}, fmt.Errorf("laya: certainty %.2f below floor %.2f", certainty, layaFloor)
	}
	// Laya returns the expected level on the rubric; normalize to 0..1.
	span := float64(len(layaLevels) - 1)
	complexity := a.Score / span
	complexity = clamp01(complexity)
	// Capped at MaxAchievableConfidence so this provider can't claim more
	// certainty than the kernel's own ceiling assumes any signal can have --
	// that constant is documented as the minimum of every provider's best
	// case, and a bonus signal must not quietly raise or invalidate it.
	confidence := certainty
	if confidence > MaxAchievableConfidence {
		confidence = MaxAchievableConfidence
	}
	return Evidence{
		Provider:   l.Name(),
		Complexity: complexity,
		Confidence: confidence,
		Facts: map[string]any{
			"score":     a.Score,
			"certainty": certainty,
			"levels":    len(layaLevels),
			"source":    "laya (local classifier)",
		},
	}, nil
}

func clamp01(f float64) float64 {
	switch {
	case f < 0:
		return 0
	case f > 1:
		return 1
	}
	return f
}
