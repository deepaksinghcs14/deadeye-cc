package main

import (
	"sync"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// lastRouting is the most recent Agent-routing decision made this
// session, kept so the next Agent call can be checked for escalation
// (Phase 6 / PLAN.md §8).
type lastRouting struct {
	taskShape string
	model     string
	effort    string
	tier      int
}

// sessionState tracks per-session dedup flags for the advisory surfaces
// (once-per-session injection, plan gate, workflow hint). Daemon-lifetime
// only, not persisted -- these are advisory/dedup, not correctness
// critical, so a fresh daemon mid-session just re-injects once more,
// which is harmless.
type sessionState struct {
	injected            bool
	pendingPlanTask     string          // non-empty = an edit/write is gated pending consent
	workflowSuggested   map[string]bool // task marker -> already suggested this task
	decisionCount       int             // decisions logged this session (Phase 1.5 session-memory input)
	lastRouting         *lastRouting
	bytesSaved          int // cumulative estimated bytes kept out of context by preprocessing rewrites this session
	rewriteCount        int
	lastShownBytesSaved int // bytesSaved value at the last Stop summary shown -- avoids repeating a stale line
}

// daemonState is the daemon's whole world: config/catalog loaded once at
// startup, the decision log, the outcomes log, and per-session state. All
// mutation goes through its methods so the same mutex always guards
// sessionState field access -- session() alone returning a bare pointer
// would let two goroutines race on its fields.
type daemonState struct {
	mu       sync.Mutex
	cfg      config.Config
	cat      catalog.Catalog
	logs     *logstore.Store
	outcomes *lessons.Store
	// outcomeCache mirrors what's on disk in outcomes.jsonl so Phase 6's
	// per-call threshold adjustment doesn't re-read the file on every
	// Agent routing decision. Loaded once at startup, appended to in
	// lockstep with outcomes.Append.
	outcomeCache []lessons.Outcome
	sessions     map[string]*sessionState
}

func newDaemonState(cfg config.Config, cat catalog.Catalog, logs *logstore.Store) *daemonState {
	cached, _ := lessons.Scan(meta.OutcomesPath())
	return &daemonState{
		cfg: cfg, cat: cat, logs: logs,
		outcomes:     lessons.Open(meta.OutcomesPath()),
		outcomeCache: cached,
		sessions:     map[string]*sessionState{},
	}
}

func (d *daemonState) getOrCreate(sessionID string) *sessionState {
	s, ok := d.sessions[sessionID]
	if !ok {
		s = &sessionState{workflowSuggested: map[string]bool{}}
		d.sessions[sessionID] = s
	}
	return s
}

// markInjectedIfFirst reports whether this call is the first for
// sessionID, atomically marking it injected either way -- the
// once-per-session gate for the advisory injection (INV-4).
func (d *daemonState) markInjectedIfFirst(sessionID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.getOrCreate(sessionID)
	if s.injected {
		return false
	}
	s.injected = true
	return true
}

// setPendingPlan records that an implementation-shaped prompt triggered
// the soft plan gate for taskMarker.
func (d *daemonState) setPendingPlan(sessionID, taskMarker string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.getOrCreate(sessionID).pendingPlanTask = taskMarker
}

// pendingPlan returns the current gated task marker for sessionID, or "".
func (d *daemonState) pendingPlan(sessionID string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getOrCreate(sessionID).pendingPlanTask
}

// clearPendingPlan records consent (or its absence no longer mattering,
// e.g. the task moved on) -- INV-7: a decline also clears it, so the gate
// never re-asks within the same task.
func (d *daemonState) clearPendingPlan(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.getOrCreate(sessionID).pendingPlanTask = ""
}

// markWorkflowSuggestedIfFirst reports whether the workflow advisor has
// already fired for this task marker this session (INV-2/§5.5: never more
// than once per task).
func (d *daemonState) markWorkflowSuggestedIfFirst(sessionID, taskMarker string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.getOrCreate(sessionID)
	if s.workflowSuggested[taskMarker] {
		return false
	}
	s.workflowSuggested[taskMarker] = true
	return true
}

// log appends r to the decision log and counts it against r.SessionID's
// running total (used by Phase 1.5's session-memory write at SessionEnd,
// and the Stop savings summary).
func (d *daemonState) log(r logstore.Record) {
	if r.SessionID != "" {
		d.mu.Lock()
		s := d.getOrCreate(r.SessionID)
		s.decisionCount++
		if r.Action == "rewrite" {
			s.bytesSaved += r.BytesBeforeEst - r.BytesAfter
			s.rewriteCount++
		}
		d.mu.Unlock()
	}
	if d.logs == nil {
		return
	}
	_ = d.logs.Append(r)
}

func (d *daemonState) decisionCount(sessionID string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getOrCreate(sessionID).decisionCount
}

// newSavingsToShow reports the session's current cumulative bytesSaved and
// rewriteCount, and whether that total has grown since the last time a
// Stop summary was shown -- so the one-line note only appears when there's
// something new to say, not on every single turn.
func (d *daemonState) newSavingsToShow(sessionID string) (bytesSaved, rewrites int, changed bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.getOrCreate(sessionID)
	if s.bytesSaved <= s.lastShownBytesSaved {
		return s.bytesSaved, s.rewriteCount, false
	}
	s.lastShownBytesSaved = s.bytesSaved
	return s.bytesSaved, s.rewriteCount, true
}

// recordOutcome appends to outcomes.jsonl and the in-memory cache used by
// AdjustedDownshiftThreshold, keeping both in lockstep.
func (d *daemonState) recordOutcome(o lessons.Outcome) {
	d.mu.Lock()
	d.outcomeCache = append(d.outcomeCache, o)
	d.mu.Unlock()
	if d.outcomes != nil {
		_ = d.outcomes.Append(o)
	}
}

func (d *daemonState) outcomesSnapshot() []lessons.Outcome {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]lessons.Outcome, len(d.outcomeCache))
	copy(out, d.outcomeCache)
	return out
}

// setLastRouting records the most recent Agent-routing decision for
// escalation detection on the session's next Agent call.
func (d *daemonState) setLastRouting(sessionID, taskShape, model, effort string, tier int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.getOrCreate(sessionID).lastRouting = &lastRouting{taskShape: taskShape, model: model, effort: effort, tier: tier}
}

// getLastRouting returns a copy of the session's last routing decision,
// or nil if none has been made yet.
func (d *daemonState) getLastRouting(sessionID string) *lastRouting {
	d.mu.Lock()
	defer d.mu.Unlock()
	lr := d.getOrCreate(sessionID).lastRouting
	if lr == nil {
		return nil
	}
	cp := *lr
	return &cp
}
