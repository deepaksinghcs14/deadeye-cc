package signals

import (
	"context"
	"fmt"

	"github.com/deepaksinghcs14/deadeye-cc/internal/laya"
)

// layaFloor is the certainty a Laya answer needs before it counts as
// evidence at all, and it is deliberately pinned to the kernel's own
// ceiling rather than set to a "reasonable-looking" lower number.
//
// kernel.Decide takes the MINIMUM confidence across all evidence and
// compares it against the downshift threshold, whose default is also 0.8.
// So a Laya answer with certainty anywhere in [floor, 0.8) would drag the
// minimum under the threshold and force the unsure ceiling for EVERY
// decision -- discarding all six heuristics and disabling downshift
// entirely. An earlier 0.6 did exactly that. At the ceiling, a
// contributing answer can never be the reason a decision goes unsure; an
// unsure classifier simply says nothing.
const layaFloor = MaxAchievableConfidence

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
	// A score outside the rubric means the server answered a DIFFERENT
	// rubric than the one asked (a different checkpoint, a 1-based scale),
	// so the number is not comparable. Clamping it would round that
	// confusion up to 1.0 -- "hardest possible task" -- which kernel.Decide
	// upshifts on with no confidence gate at all. Skip instead.
	span := float64(len(layaLevels) - 1)
	if a.Score < 0 || a.Score > span {
		return Evidence{}, fmt.Errorf("laya: score %v outside the %d-level rubric", a.Score, len(layaLevels))
	}
	complexity := a.Score / span
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
