package main

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/gitutil"
	"github.com/deepaksinghcs14/deadeye-cc/internal/kernel"
	"github.com/deepaksinghcs14/deadeye-cc/internal/laya"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
)

// KindTierDisagreement marks a sampled decision the judge would have
// routed cheaper than the kernel confidently did.
const KindTierDisagreement = "tier-disagreement"

// sampleTier is the minimum tier worth sampling. Below it there is no
// cheaper tier to disagree about, and a wrong cheap route shows up as an
// escalation anyway -- that direction is already measured.
const sampleTier = 2

// tierSampleSeen counts eligible decisions for the 1-in-N sampler.
// Process-global rather than per-session: the sample rate is about how
// much money the sampler is allowed to spend on this machine, which is a
// property of the daemon, not of any one session.
var tierSampleSeen atomic.Uint64

// tierSampleAsync is the seam tests replace -- the real one spawns a
// goroutine that outlives the hook response, which a test can't wait on.
var tierSampleAsync = func(f func()) { go f() }

// maybeSampleTier looks for over-routing, the blind spot in the routing
// feedback loop.
//
// applyRoutingJudge only runs when a decision is Unsure, so a CONFIDENT
// high-tier route is never second-guessed: if the heuristics systematically
// over-route some shape of task, nothing in the system would ever say so.
// Escalations cover the opposite direction already (routed too cheap, had
// to go up) and bias routing upward. This samples the other side.
//
// What it produces is a DISAGREEMENT, not a verdict. The judge saying
// "tier 1" is not proof a tier-1 model would have done the job -- there is
// no grader for an arbitrary production subtask, which is why this samples
// an opinion instead of replaying the task. `deadeye disagreement` labels
// it that way, and nothing here feeds a routing change.
//
// Runs asynchronously and best-effort: the judge takes up to 10s, and this
// sits on the PreToolUse path that gates a real tool call (INV-8). The
// decision has already been returned by the time this fires.
func (d *daemonState) maybeSampleTier(cfg config.Config, decision kernel.Decision, cat catalog.Catalog, shape, prompt, sessionID, cwd string) {
	if cfg.Mode.TierSample != "on" || cfg.Mode.RoutingJudge != "on" || prompt == "" {
		return
	}
	// Only confident decisions are interesting: an Unsure one already went
	// through the judge on the live path, so re-asking would just confirm
	// the judge's own answer.
	if decision.Unsure {
		return
	}
	tier, ok := cat.TierFor(decision.Model)
	if !ok || tier < sampleTier {
		return
	}
	// With Laya configured, the sample rate stops being the cost control:
	// a local classifier is free, so EVERY eligible decision gets screened
	// and only the ones Laya thinks were over-routed cost a `claude -p`
	// call to confirm. Two independent opinions before anything is
	// recorded, and denser coverage than a 1-in-N sonnet sample could ever
	// justify. Laya agreeing ends the check silently -- this report counts
	// disagreements, and agreement is not evidence of correctness.
	// The screen is measurement, never a decision, so it belongs entirely
	// off the hook path -- an earlier version ran it inline here and could
	// add a full Laya timeout to every eligible Agent call on top of the
	// routing call's own. The rate check moves inside too, so the counter
	// only advances on samples that actually proceed.
	// Only the authoritative rung screens. On shadow and advise, Laya is
	// explicitly not allowed to change behavior -- and letting it decide
	// which decisions get sampled IS a behavior change, one that silently
	// replaced the documented 1-in-N sampler the moment an endpoint was
	// configured.
	var layaScreen *laya.Client
	if layaDecides(cfg) {
		layaScreen = layaClient(cfg)
	}
	rateCheck := func() bool {
		rate := cfg.TierSample.Rate
		if rate < 1 {
			rate = 10
		}
		return tierSampleSeen.Add(1)%uint64(rate) == 0
	}
	repo := gitutil.ProjectKey(cwd)
	tierSampleAsync(func() {
		screened := false
		if layaScreen != nil {
			sctx, scancel := context.WithTimeout(context.Background(), layaTimeout(cfg))
			layaT, _, _, lok := layaTier(sctx, layaScreen, prompt)
			scancel()
			switch {
			case !lok:
				// Configured but silent. Falling through to the paid sampler
				// is the fail-open answer (INV-5): a stopped laya-serve must
				// degrade to the PRIOR behavior, not switch measurement off
				// entirely and report zero disagreements forever.
			case layaT >= tier:
				return // the two agree; nothing to confirm and nothing to record
			default:
				screened = true
			}
		}
		if !screened && !rateCheck() {
			return
		}
		judged, ok := judgeTierCached(prompt)
		if !ok || judged >= tier {
			return
		}
		jt := judged
		d.recordOutcome(lessons.Outcome{
			TS:         nowRFC3339(),
			SessionID:  sessionID,
			Surface:    lessons.SurfaceRouting,
			TaskShape:  shape,
			Model:      decision.Model,
			Effort:     decision.Effort,
			Kind:       KindTierDisagreement,
			Weight:     lessons.WeightEscalation,
			Repo:       repo,
			JudgedTier: &jt,
		})
	})
}

// judgedTierAge bounds how far back `deadeye disagreement` looks, matching
// the 30-day recency window the rest of the lessons store reasons in.
const judgedTierAge = 30 * 24 * time.Hour
