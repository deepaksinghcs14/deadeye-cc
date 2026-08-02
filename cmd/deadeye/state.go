package main

import (
	"sync"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
)

// sessionState tracks per-session dedup flags for the advisory surfaces
// (once-per-session injection, plan gate, workflow hint). Daemon-lifetime
// only, not persisted -- these are advisory/dedup, not correctness
// critical, so a fresh daemon mid-session just re-injects once more,
// which is harmless.
type sessionState struct {
	injected          bool
	pendingPlanTask   string          // non-empty = an edit/write is gated pending consent
	workflowSuggested map[string]bool // task marker -> already suggested this task
	decisionCount     int             // decisions logged this session (Phase 1.5 session-memory input)
}

// daemonState is the daemon's whole world: config/catalog loaded once at
// startup, the decision log, and per-session state. All mutation goes
// through its methods so the same mutex always guards sessionState field
// access -- session() alone returning a bare pointer would let two
// goroutines race on its fields.
type daemonState struct {
	mu       sync.Mutex
	cfg      config.Config
	cat      catalog.Catalog
	logs     *logstore.Store
	sessions map[string]*sessionState
}

func newDaemonState(cfg config.Config, cat catalog.Catalog, logs *logstore.Store) *daemonState {
	return &daemonState{cfg: cfg, cat: cat, logs: logs, sessions: map[string]*sessionState{}}
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
// running total (used by Phase 1.5's session-memory write at SessionEnd).
func (d *daemonState) log(r logstore.Record) {
	if r.SessionID != "" {
		d.mu.Lock()
		d.getOrCreate(r.SessionID).decisionCount++
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
