package main

import (
	"strings"
	"testing"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
)

func disagreementCfg(mode string) config.Config {
	cfg := config.Default()
	cfg.Mode.TierSample = mode
	return cfg
}

// The default is off, and a report that silently showed "0 disagreements"
// while measuring nothing would read as a clean bill of health.
func TestRenderDisagreementSaysWhenSamplingIsOff(t *testing.T) {
	out := captureStdout(t, func() {
		renderDisagreement(writeOutcomes(t), disagreementCfg("off"), time.Now())
	})
	if !strings.Contains(out, "Sampling is off") {
		t.Errorf("must disclose that nothing is being measured:\n%s", out)
	}
	if !strings.Contains(out, "mode.tier_sample on") {
		t.Errorf("must say how to turn it on:\n%s", out)
	}
}

func TestRenderDisagreementCountsAndGroupsByShape(t *testing.T) {
	now := time.Now()
	ts := now.Add(-2 * time.Hour).Format(time.RFC3339)
	one, zero := 1, 0
	op := writeOutcomes(t,
		lessons.Outcome{TS: ts, Surface: lessons.SurfaceRouting, TaskShape: "code-edit", Model: "claude-opus-5", Kind: KindTierDisagreement, JudgedTier: &one},
		lessons.Outcome{TS: ts, Surface: lessons.SurfaceRouting, TaskShape: "code-edit", Model: "claude-opus-5", Kind: KindTierDisagreement, JudgedTier: &zero},
		lessons.Outcome{TS: ts, Surface: lessons.SurfaceRouting, TaskShape: "search", Model: "claude-opus-5", Kind: KindTierDisagreement, JudgedTier: &zero},
		lessons.Outcome{TS: ts, Surface: lessons.SurfaceRouting, TaskShape: "code-edit", Kind: "escalation", Weight: 1},
	)
	out := captureStdout(t, func() { renderDisagreement(op, disagreementCfg("on"), now) })

	if !strings.Contains(out, "code-edit") || !strings.Contains(out, "search") {
		t.Errorf("both task shapes should be listed:\n%s", out)
	}
	if !strings.Contains(out, "tier1 x1") || !strings.Contains(out, "tier0 x1") {
		t.Errorf("per-tier breakdown missing:\n%s", out)
	}
	// The opposite direction is shown beside it so neither looks like the
	// whole picture.
	if !strings.Contains(out, "Escalations") {
		t.Errorf("escalations in the same window should be shown:\n%s", out)
	}
}

// Outcomes older than the 30-day window the rest of the lessons store
// reasons in don't belong in a "last 30 days" count.
func TestRenderDisagreementIgnoresStaleOutcomes(t *testing.T) {
	now := time.Now()
	old := now.Add(-60 * 24 * time.Hour).Format(time.RFC3339)
	one := 1
	op := writeOutcomes(t, lessons.Outcome{
		TS: old, Surface: lessons.SurfaceRouting, TaskShape: "code-edit",
		Kind: KindTierDisagreement, JudgedTier: &one,
	})
	out := captureStdout(t, func() { renderDisagreement(op, disagreementCfg("on"), now) })
	if !strings.Contains(out, "No sampled disagreements") {
		t.Errorf("a 60-day-old outcome should fall outside the window:\n%s", out)
	}
}

// The load-bearing caveat: this is an opinion, not a measured over-route
// rate, because a production subtask has no grader.
func TestRenderDisagreementCarriesTheOpinionCaveat(t *testing.T) {
	now := time.Now()
	one := 1
	op := writeOutcomes(t, lessons.Outcome{
		TS: now.Format(time.RFC3339), Surface: lessons.SurfaceRouting,
		TaskShape: "code-edit", Kind: KindTierDisagreement, JudgedTier: &one,
	})
	out := captureStdout(t, func() { renderDisagreement(op, disagreementCfg("on"), now) })
	for _, want := range []string{"OPINION", "no grader", "Nothing here changes routing"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing caveat %q:\n%s", want, out)
		}
	}
}
