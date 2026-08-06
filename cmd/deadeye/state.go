package main

import (
	"sort"
	"sync"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
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
	suggested           map[string]bool // dedupe key ("plan:"/"workflow:" + marker) -> already suggested this task
	decisionCount       int             // decisions logged this session (Phase 1.5 session-memory input)
	lastRouting         *lastRouting
	bytesSaved          int // cumulative estimated bytes kept out of context by preprocessing rewrites this session
	rewriteCount        int
	lastShownBytesSaved int               // bytesSaved value at the last Stop summary shown -- avoids repeating a stale line
	readFiles           map[string]int64  // Read'd file path -> mtime (unix nanos) at read time, for duplicate-read advice
	lastBashCommand     string            // previous Bash command, cleared by any Edit/Write -- consecutive-repeat detection
	pendingRewrites     map[string]string // rewritten command -> rule name, consumed at PostToolUse to measure real output size
	fetchedURLs         map[string]bool   // normalized WebFetch URLs seen this session -- repeat-fetch advice
	coderLevel          string            // coder-mode session level; "" = unset (config default applies), "off" = explicitly off
	nativeRestore       bool              // SessionStart arrived with source resume/compact -- Claude Code restored context itself
	muted               bool              // /deadeye-mute: advisory surfaces stand down for this session (rewrites and the hard gate stay)
}

// daemonState is the daemon's whole world: catalog loaded once at startup,
// the decision log, the outcomes log, and per-session state. Config is
// deliberately NOT part of this -- the daemon serves every project and
// session it's asked about, so config (including project-level
// .deadeye.json and env-derived kill switches) is loaded fresh per request
// via config.LoadFor, not cached here for the daemon's whole lifetime; see
// LoadFor's comment. All mutation goes through daemonState's methods so the
// same mutex always guards sessionState field access -- session() alone
// returning a bare pointer would let two goroutines race on its fields.
type daemonState struct {
	mu       sync.Mutex
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

func newDaemonState(cat catalog.Catalog, logs *logstore.Store) *daemonState {
	cached, _ := lessons.Scan(meta.OutcomesPath())
	return &daemonState{
		cat: cat, logs: logs,
		outcomes:     lessons.Open(meta.OutcomesPath()),
		outcomeCache: cached,
		sessions:     map[string]*sessionState{},
	}
}

func (d *daemonState) getOrCreate(sessionID string) *sessionState {
	s, ok := d.sessions[sessionID]
	if !ok {
		s = &sessionState{
			suggested:       map[string]bool{},
			readFiles:       map[string]int64{},
			pendingRewrites: map[string]string{},
			fetchedURLs:     map[string]bool{},
		}
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

// markSuggestedIfFirst reports whether the given dedupe key (a surface
// prefix -- "plan:" or "workflow:" -- plus a task marker) has already fired
// this session. Shared by the plan gate and the workflow advisor
// (INV-2/§5.5: neither should nag more than once per task) so both get the
// same once-per-task guarantee from one map instead of two.
func (d *daemonState) markSuggestedIfFirst(sessionID, key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.getOrCreate(sessionID)
	if s.suggested[key] {
		return false
	}
	s.suggested[key] = true
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

// markFileRead records path's mtime for sessionID and reports whether it
// was already read this session WITH the same mtime -- i.e. a repeat read
// of an unchanged file, the one case where re-reading buys nothing. A
// changed mtime (the model or the user edited it) is not a repeat.
func (d *daemonState) markFileRead(sessionID, path string, mtime int64) (unchangedRepeat bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.getOrCreate(sessionID)
	prev, seen := s.readFiles[path]
	s.readFiles[path] = mtime
	return seen && prev == mtime
}

// readFilesSnapshot returns the paths this session Read, sorted (map
// iteration order is randomized, and this feeds codemap's persisted
// touch-frequency file). endSession destroys the map, so SessionEnd is the
// last moment "which files this project's work actually touches" exists
// anywhere -- call this before eviction.
func (d *daemonState) readFilesSnapshot(sessionID string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.getOrCreate(sessionID)
	paths := make([]string, 0, len(s.readFiles))
	for p := range s.readFiles {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// noteWebFetch records url (already normalized by the caller) as fetched
// this session and reports whether it was fetched before -- the first
// response is still in context, so a repeat buys nothing unless the page
// itself changed.
func (d *daemonState) noteWebFetch(sessionID, url string) (repeat bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.getOrCreate(sessionID)
	seen := s.fetchedURLs[url]
	s.fetchedURLs[url] = true
	return seen
}

// noteBashCommand records cmd as the session's most recent Bash command
// and reports whether it's an immediate repeat -- the same command run
// again with no Edit/Write in between (clearLastBash handles the
// in-between part), which is the retry-loop pathology worth flagging.
func (d *daemonState) noteBashCommand(sessionID, cmd string) (consecutiveRepeat bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.getOrCreate(sessionID)
	repeat := s.lastBashCommand != "" && s.lastBashCommand == cmd
	s.lastBashCommand = cmd
	return repeat
}

// clearLastBash forgets the session's previous Bash command -- called on
// Edit/Write, since a repeat AFTER a change is a legitimate re-run, not a
// retry loop.
func (d *daemonState) clearLastBash(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.getOrCreate(sessionID).lastBashCommand = ""
}

// notePendingRewrite records that rewrittenCmd (the command actually
// executed) came from rule, so PostToolUse can attribute the real output
// size back to the rule that produced it.
func (d *daemonState) notePendingRewrite(sessionID, rewrittenCmd, rule string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.getOrCreate(sessionID).pendingRewrites[rewrittenCmd] = rule
}

// consumePendingRewrite returns (and forgets) the rule name that produced
// rewrittenCmd, or "" if this command wasn't one of ours.
func (d *daemonState) consumePendingRewrite(sessionID, rewrittenCmd string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.getOrCreate(sessionID)
	rule := s.pendingRewrites[rewrittenCmd]
	delete(s.pendingRewrites, rewrittenCmd)
	return rule
}

// setCoderLevel records the session's coder-mode level ("" = unset, "off"
// = explicitly off -- the distinction matters: unset falls back to the
// config default at the next SessionStart, explicit off stays off).
func (d *daemonState) setCoderLevel(sessionID, level string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.getOrCreate(sessionID).coderLevel = level
}

// coderLevel returns the session's coder-mode level ("" if unset).
func (d *daemonState) coderLevelFor(sessionID string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getOrCreate(sessionID).coderLevel
}

// markNativeRestore records that this session's context was restored by
// Claude Code itself (SessionStart source resume/compact) -- the
// next-prompt injection then skips the session-memory paragraph, whose
// whole content the restored transcript or compaction summary already
// covers (PLAN.md §5.7 / §10.10).
func (d *daemonState) markNativeRestore(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.getOrCreate(sessionID).nativeRestore = true
}

func (d *daemonState) nativeRestoreFor(sessionID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getOrCreate(sessionID).nativeRestore
}

func (d *daemonState) setMuted(sessionID string, muted bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.getOrCreate(sessionID).muted = muted
}

func (d *daemonState) isMuted(sessionID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getOrCreate(sessionID).muted
}

// endSession evicts sessionID's in-memory state at SessionEnd. Call this
// last, after anything else in the SessionEnd handler still needs the
// session's current counters.
func (d *daemonState) endSession(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.sessions, sessionID)
}

// clearLastRouting consumes the session's last routing decision after an
// escalation has been recorded against it -- a prior decision should only
// ever be graded once. Without this, two consecutive Agent calls that both
// requested an explicit higher-tier model would record TWO escalation
// outcomes against the same single prior recommendation.
func (d *daemonState) clearLastRouting(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.getOrCreate(sessionID).lastRouting = nil
}
