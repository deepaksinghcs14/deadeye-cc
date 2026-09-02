// Package lessons is PLAN.md §8's learning loop, scoped to what the
// available hook surfaces can actually detect (the plan itself says to
// build incrementally: "start with escalation + gate outcomes ... add
// revert/test-fail in a later phase"). Gate outcomes turned out not to be
// one of the cheap ones: Phase 4 found no hook surface reports back which
// way a permission prompt was answered, so gate_accepted/gate_declined
// aren't detectable at all with what's available today. Escalation is:
// if the caller explicitly requests a higher-tier model than deadeye's
// last recommendation for a similar task shape, that's a real signal the
// recommendation was too low. Revert/test-fail detection (transcript
// parsing, git state correlation) is real future work, not attempted here.
//
// docs/PRD-lessons.md (local-only) generalizes this to two more surfaces:
// coder-miss (a review skill confirms a finding in code coder mode just
// wrote) and review-false-positive (the user disputes a reported
// finding). Both are per-repo, unlike escalation's global routing shape --
// see Outcome.Repo.
package lessons

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// WeightEscalation is PLAN.md §8's table: escalation is a strong negative
// signal. (There used to be a WeightClean alongside it for a "no error"
// positive signal -- deleted, unused: nothing in this codebase ever wrote
// a "clean" outcome, and per INV-9 a weak positive can't undo an
// escalation's bias on its own anyway; see recencyWindow instead.)
const WeightEscalation = 1.0

// Outcome is one observed signal attached to a task shape.
//
// Surface and Repo are additive: a pre-existing row with neither field set
// (every row recorded before this field existed) means Surface=="routing"
// and Repo=="" (global) -- today's only outcome kind, unchanged. Callers
// must treat an empty Surface as "routing", never as "unknown surface,
// exclude it" -- that would silently drop every historical escalation.
type Outcome struct {
	TS        string  `json:"ts"`
	SessionID string  `json:"session_id"`
	Surface   string  `json:"surface,omitempty"` // "routing" (default/empty) | "coder" | "pr-review"
	TaskShape string  `json:"task_shape"`
	Model     string  `json:"model"`
	Effort    string  `json:"effort"`
	Kind      string  `json:"kind"` // "escalation" | "coder-miss" | "review-false-positive"
	Weight    float64 `json:"weight"`
	Repo      string  `json:"repo,omitempty"` // gitutil.ProjectKey; "" means global (routing's shape already is)
}

// SurfaceRouting, SurfaceCoder, and SurfacePRReview are Outcome.Surface's
// three values. SurfaceRouting is also the empty string's meaning, for
// back-compat with rows written before this field existed -- never compare
// against "" directly outside this package.
const (
	SurfaceRouting  = "routing"
	SurfaceCoder    = "coder"
	SurfacePRReview = "pr-review"
)

// EffectiveSurface returns o's surface, mapping the back-compat empty
// value (every row written before Surface existed) to SurfaceRouting.
func (o Outcome) EffectiveSurface() string {
	if o.Surface == "" {
		return SurfaceRouting
	}
	return o.Surface
}

// smoothing keeps a single early escalation from permanently maxing out
// the adjusted threshold for a task shape -- PLAN.md §8: "simple
// Beta-style blending is fine, do not over-engineer this."
const smoothing = 3.0

// recencyWindow bounds how long an escalation keeps raising the
// threshold. Without this, a single escalation makes downshifting
// mathematically impossible for that task shape FOREVER: the minimum
// confidence any builtin provider ever emits is 0.8 (testpresence) and the
// maximum is 0.85, so one escalation alone raises the bar to
// 0.8 + 0.2*(1/(1+3)) = 0.85 -- an unreachable threshold, in every
// project (outcomes.jsonl is global, rotated past 10MB like decisions.jsonl), forever. 30 days
// reuses gitchurn's existing "recent activity" window rather than
// inventing a new coefficient.
const recencyWindow = 30 * 24 * time.Hour

// AdjustedDownshiftThreshold raises the confidence bar required to
// downshift a task shape, proportional to how recently and how often that
// shape has been escalated. Outcomes older than recencyWindow don't count
// at all -- a stale escalation eventually stops mattering rather than
// pinning the shape to the ceiling forever. now is passed in (rather than
// read internally) so this stays deterministic to test. An outcome whose
// timestamp fails to parse is treated as recent -- unknown routes up here
// same as everywhere else in this kernel, rather than being silently
// dropped as if it never happened. Escalation-free or never-seen shapes
// get the unmodified base threshold -- this only ever makes downshifting
// harder, never easier, consistent with INV-1.
func AdjustedDownshiftThreshold(base float64, outcomes []Outcome, taskShape string, now time.Time) float64 {
	if base >= 1 {
		return base
	}
	var escalationWeight float64
	for _, o := range outcomes {
		if o.EffectiveSurface() != SurfaceRouting || o.TaskShape != taskShape || o.Kind != "escalation" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339, o.TS); err == nil && now.Sub(ts) > recencyWindow {
			continue
		}
		escalationWeight += o.Weight
	}
	if escalationWeight == 0 {
		return base
	}
	bias := escalationWeight / (escalationWeight + smoothing)
	return base + (1-base)*bias
}

// WeightMiss is coder-miss and review-false-positive's per-occurrence
// weight -- the same value as WeightEscalation, kept as its own name since
// the two are recorded by different callers, not because the number
// differs.
const WeightMiss = 1.0

// RankedShape is one TaskShape's aggregated recent activity within a
// surface+repo, for RecentShapes.
type RankedShape struct {
	Shape  string
	Count  int
	Weight float64
}

// RecentShapes ranks the task shapes recorded for surface+repo within the
// same 30-day recencyWindow AdjustedDownshiftThreshold uses (no new
// coefficients), by total weight descending (ties broken by count, then
// shape name, for determinism), capped at n. Used to render a bounded
// "recent misses" list -- never unbounded, per INV-4's no-silent-creep
// intent. repo == "" matches only global-scoped outcomes (routing's
// shape); coder and pr-review outcomes are always repo-scoped in practice.
func RecentShapes(outcomes []Outcome, surface, repo string, now time.Time, n int) []RankedShape {
	if n <= 0 {
		return nil
	}
	agg := map[string]*RankedShape{}
	var order []string
	for _, o := range outcomes {
		if o.EffectiveSurface() != surface || o.Repo != repo {
			continue
		}
		if ts, err := time.Parse(time.RFC3339, o.TS); err == nil && now.Sub(ts) > recencyWindow {
			continue
		}
		r, ok := agg[o.TaskShape]
		if !ok {
			r = &RankedShape{Shape: o.TaskShape}
			agg[o.TaskShape] = r
			order = append(order, o.TaskShape)
		}
		r.Count++
		r.Weight += o.Weight
	}
	ranked := make([]RankedShape, 0, len(order))
	for _, shape := range order {
		ranked = append(ranked, *agg[shape])
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Weight != ranked[j].Weight {
			return ranked[i].Weight > ranked[j].Weight
		}
		if ranked[i].Count != ranked[j].Count {
			return ranked[i].Count > ranked[j].Count
		}
		return ranked[i].Shape < ranked[j].Shape
	})
	if len(ranked) > n {
		ranked = ranked[:n]
	}
	return ranked
}

// Store is an append-only JSONL log, the same pattern as
// internal/logstore but typed for Outcome -- kept separate rather than
// generalizing logstore, since that would mean threading a type
// parameter through every existing decisions.jsonl call site for a
// second file this small.
type Store struct {
	mu   sync.Mutex
	path string
}

func Open(path string) *Store { return &Store{path: path} }

func (s *Store) Append(o Outcome) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	// Same 10MB rotate-to-.1 backstop as logstore: outcomes are rare
	// (escalations only) but this was the one file with zero ceiling, and
	// the daemon mirrors it whole in memory at startup.
	if fi, err := os.Stat(s.path); err == nil && fi.Size() > 10<<20 {
		_ = os.Rename(s.path, s.path+".1")
	}
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(b)
	return err
}

// Scan reads every outcome in the log. A missing file is not an error --
// it means no outcomes have been logged yet.
func Scan(path string) ([]Outcome, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Outcome
	for _, line := range bytes.Split(b, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var o Outcome
		if json.Unmarshal(line, &o) != nil {
			continue // skip malformed lines rather than fail the whole scan
		}
		out = append(out, o)
	}
	return out, nil
}
