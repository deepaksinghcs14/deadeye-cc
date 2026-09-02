package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/coder"
	"github.com/deepaksinghcs14/deadeye-cc/internal/gitutil"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
	"github.com/deepaksinghcs14/deadeye-cc/internal/signals"
)

func fileCountBucket(n int) string {
	switch {
	case n == 0:
		return "0"
	case n == 1:
		return "1"
	case n <= 3:
		return "2-3"
	case n <= 8:
		return "4-8"
	default:
		return "9+"
	}
}

// taskShapeKey is PLAN.md §8's coarse categorical feature vector,
// collapsed to a short string: file-count bucket, mechanical vs.
// implementation-shaped, and whether adjacent tests exist. Coarse on
// purpose -- priors need density, not precision.
func taskShapeKey(files []string, prompt string, evidence []signals.Evidence) string {
	hasTests := false
	for _, e := range evidence {
		if e.Provider != "testpresence" {
			continue
		}
		if n, ok := e.Facts["files_with_adjacent_test"].(int); ok && n > 0 {
			hasTests = true
		}
	}
	return fmt.Sprintf("files=%s,impl=%v,tests=%v", fileCountBucket(len(files)), looksImplementationShaped(prompt), hasTests)
}

// checkEscalation is Phase 6's one detectable outcome signal (PLAN.md §8):
// if the caller explicitly requests a higher-tier model than deadeye's
// last recommendation for this session, that's evidence the last
// recommendation was too conservative-cheap for this kind of task.
// Recorded against the PRIOR decision's task shape, per the plan's table
// ("1.0 negative on assigned cell").
func checkEscalation(in hookio.Input, ai agentInput, currentShape string, state *daemonState) {
	if ai.Model == "" {
		return // caller didn't request a specific model -- nothing to compare
	}
	prev := state.getLastRouting(in.SessionID)
	if prev == nil {
		return
	}
	requestedTier, ok := state.cat.TierFor(familyToAnyModelID(state, ai.Model))
	if !ok {
		return
	}
	if requestedTier <= prev.tier {
		return // same or cheaper -- not an escalation
	}

	state.recordOutcome(lessons.Outcome{
		TS:        nowRFC3339(),
		SessionID: in.SessionID,
		TaskShape: prev.taskShape,
		Model:     prev.model,
		Effort:    prev.effort,
		Kind:      "escalation",
		Weight:    lessons.WeightEscalation,
	})
	// The prior decision has now been graded -- clear it so a SECOND
	// consecutive explicit-model call doesn't record a second escalation
	// against the same one.
	state.clearLastRouting(in.SessionID)
	state.log(logstore.Record{
		TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/Agent",
		Action: "escalation-detected", Reason: fmt.Sprintf("shape=%s recommended=%s(tier %d) requested-family=%s(tier %d)", prev.taskShape, prev.model, prev.tier, ai.Model, requestedTier),
	})
}

// familyToAnyModelID resolves a family alias (e.g. "opus") to any one
// matching catalog model id, so TierFor can look up its tier -- the
// Agent tool only ever supplies the family, never a full model id.
func familyToAnyModelID(state *daemonState, family string) string {
	for _, m := range state.cat.Models {
		if m.Family == family {
			return m.ID
		}
	}
	return ""
}

// surfaceDisplayOrder controls `deadeye lessons`' section order: routing
// first since it's the original, longest-running signal and the only one
// that biases a numeric threshold; coder and pr-review are read-only
// reminders/priority inputs, listed after.
var surfaceDisplayOrder = []string{lessons.SurfaceRouting, lessons.SurfaceCoder, lessons.SurfacePRReview}

// surfaceArg parses an optional `--surface <value>` out of args, the same
// hand-rolled flag-with-value shape hook.go's --host uses -- this repo has
// no flag package. Returns "" if absent.
func surfaceArg(args []string) string {
	for i, a := range args {
		if a == "--surface" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// runLessons backs `deadeye lessons [reset [--surface <s>]]`: the grouped
// view (and reset, whole or filtered) for the outcomes store. Routing
// outcomes bias the downshift threshold (internal/lessons.
// AdjustedDownshiftThreshold); coder outcomes bias the "recent misses"
// reminder injected per repo; pr-review outcomes bias finding priority in
// the PR rubric (docs/PRD-lessons.md §7). One bad early signal otherwise
// biases its target for up to the 30-day recency window with no way to
// inspect or clear it.
func runLessons(args []string) {
	if len(args) > 0 && args[0] == "record" {
		runLessonsRecord(args[1:])
		return
	}
	if len(args) > 0 && args[0] == "priority" {
		runLessonsPriority()
		return
	}
	if len(args) > 0 && args[0] == "reset" {
		surface := surfaceArg(args[1:])
		outcomes, _ := lessons.Scan(meta.OutcomesPath())
		if surface == "" {
			if err := os.Remove(meta.OutcomesPath()); err != nil && !os.IsNotExist(err) {
				fmt.Fprintln(os.Stderr, "deadeye lessons reset:", err)
				os.Exit(1)
			}
			fmt.Printf("cleared %d recorded outcomes.\n", len(outcomes))
		} else {
			kept := make([]lessons.Outcome, 0, len(outcomes))
			removed := 0
			for _, o := range outcomes {
				if o.EffectiveSurface() == surface {
					removed++
					continue
				}
				kept = append(kept, o)
			}
			if err := rewriteOutcomes(meta.OutcomesPath(), kept); err != nil {
				fmt.Fprintln(os.Stderr, "deadeye lessons reset:", err)
				os.Exit(1)
			}
			fmt.Printf("cleared %d recorded outcomes for surface %q.\n", removed, surface)
		}
		fmt.Println(cDim("A running daemon keeps its in-memory copy until it restarts;"))
		fmt.Println(cDim("it exits after 30 idle minutes, or stop it with: deadeye uninstall (any hook respawns it)."))
		return
	}

	outcomes, err := lessons.Scan(meta.OutcomesPath())
	if err != nil || len(outcomes) == 0 {
		fmt.Println("No recorded outcomes. Routing thresholds are unbiased, and no repo has a recent-misses reminder.")
		return
	}
	fmt.Println(cHead("Recorded outcomes") + cDim("  (30-day recency window per surface)"))
	for _, surface := range surfaceDisplayOrder {
		var section []lessons.Outcome
		for _, o := range outcomes {
			if o.EffectiveSurface() == surface {
				section = append(section, o)
			}
		}
		if len(section) == 0 {
			continue
		}
		fmt.Println()
		fmt.Println(cHead(surface))
		for _, o := range section {
			fmt.Printf("  %s  %-22s %-40s %s/%s  weight %.1f\n", cDim(o.TS), o.Kind, o.TaskShape, o.Model, o.Effort, o.Weight)
		}
	}
	fmt.Println()
	fmt.Println(cDim("Clear everything: ") + cValue("deadeye lessons reset"))
	fmt.Println(cDim("Clear one surface: ") + cValue("deadeye lessons reset --surface coder"))
}

// missesToShow caps the injected "recent misses" line at 3 shapes -- INV-4's
// no-silent-creep intent applies to this reminder just as much as to any
// other SessionStart/UserPromptSubmit content, and an unboundedly growing
// checklist is exactly what coder mode's own "no scaffolding for later"
// philosophy (internal/coder/ruleset.md) argues against.
const missesToShow = 3

// lessonsMissesText renders the "recent misses in this repo" reminder from
// coder-miss outcomes recorded for cwd's repo, or "" when there's nothing
// to show. Called only when mode.codemap is on (same gate as mapText) and
// after a state.reloadOutcomes(), so a `deadeye lessons record` write from
// a prior turn's skill call is visible even though it ran in a separate
// process.
func lessonsMissesText(state *daemonState, cwd string) string {
	repo := gitutil.ProjectKey(cwd)
	shapes := lessons.RecentShapes(state.outcomesSnapshot(), lessons.SurfaceCoder, repo, time.Now(), missesToShow)
	if len(shapes) == 0 {
		return ""
	}
	return "deadeye: recent misses in this repo: " + renderShapes(shapes) +
		" -- recheck these before calling a change done."
}

// renderShapes formats ranked shapes as "shape (Nx), shape (Nx)", shared by
// the SessionStart misses reminder and `deadeye lessons priority`.
func renderShapes(shapes []lessons.RankedShape) string {
	items := make([]string, len(shapes))
	for i, s := range shapes {
		items[i] = fmt.Sprintf("%s (%d×)", s.Shape, s.Count)
	}
	return strings.Join(items, ", ")
}

// runLessonsPriority backs `deadeye lessons priority`, a read-only view for
// /deadeye-pr's repo-scoped weighting pass (docs/PRD-lessons.md §7): recent
// coder-miss shapes for this repo warrant closer scrutiny; recent
// review-false-positive shapes warrant stronger proof before being reported
// again. Per INV-1/INV-9, this is guidance to weight, never a signal to
// silence a lens/tag outright -- the PR rubric enforces that, this command
// only surfaces the raw signal. Unlike the SessionStart misses reminder,
// this runs on demand, not injected, and reads both surfaces, not just
// coder.
func runLessonsPriority() {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	repo := gitutil.ProjectKey(cwd)
	outcomes, _ := lessons.Scan(meta.OutcomesPath())
	now := time.Now()
	misses := lessons.RecentShapes(outcomes, lessons.SurfaceCoder, repo, now, missesToShow)
	disputes := lessons.RecentShapes(outcomes, lessons.SurfacePRReview, repo, now, missesToShow)
	if len(misses) == 0 && len(disputes) == 0 {
		fmt.Println("No repo-scoped priority signal yet.")
		return
	}
	if len(misses) > 0 {
		fmt.Println("Scrutinize harder (recent coder misses): " + renderShapes(misses))
	}
	if len(disputes) > 0 {
		fmt.Println("Recently disputed, need stronger proof before reporting again (never skip outright): " + renderShapes(disputes))
	}
}

// recordableKinds maps a `deadeye lessons record` kind to its surface.
// escalation is deliberately absent: it's recorded internally by
// checkEscalation from a live routing decision, never through this CLI.
//
// external-miss shares coder-miss's surface (SurfaceCoder) deliberately --
// both feed the exact same "recent misses in this repo" consumption
// (RecentShapes only ever filters by surface, never by kind), so a real bug
// shape another reviewer caught is just as worth reminding coder mode about
// as one deadeye's own review caught. Kind still distinguishes the two in
// the raw `deadeye lessons` log, for audit.
var recordableKinds = map[string]string{
	"coder-miss":            lessons.SurfaceCoder,
	"external-miss":         lessons.SurfaceCoder,
	"review-false-positive": lessons.SurfacePRReview,
}

const recordUsage = "usage: deadeye lessons record <coder-miss|external-miss|review-false-positive> <lens:tag>"

// shapeRe validates a lens:tag shape (e.g. "security:inject") against
// internal/prreview/ruleset.md's finding-tag FORMAT, without duplicating
// its 26-entry vocabulary here -- that list already has one source of
// truth (the markdown, canary-tested against skills/deadeye-pr/SKILL.md);
// a second hardcoded Go copy would just be something to drift out of sync.
var shapeRe = regexp.MustCompile(`^[a-z][a-z-]*:[a-z][a-z0-9-]*$`)

// runLessonsRecord backs `deadeye lessons record <kind> <shape>`, called by
// /deadeye-guard and /deadeye-review after CONFIRMING a finding (never a
// raw candidate) in code touched by the current diff (docs/PRD-lessons.md
// §6), and by /deadeye-pr for a finding another reviewer caught that it
// verified but didn't itself report (§9 Phase E). Best-effort by contract,
// same as notes-append -- any failure here is a quiet exit, the calling
// skill is told to move on regardless.
//
// Attribution differs by kind:
//   - coder-miss: strict (the plan's "Attribution" decision) -- recorded
//     only when coder mode is currently active, per coderModeActive. Avoids
//     blaming coder mode for pre-existing/third-party code a session
//     happened to scan with the persona off.
//   - external-miss: no such gate -- it names a real bug shape another
//     reviewer caught in a SHIPPED PR, which "was coder mode on right now"
//     can't answer either way after the fact. Known limitation: this kind's
//     signal quality rests entirely on the skill's own verification
//     discipline, not a mechanical check like coderModeActive.
//   - review-false-positive: no gate -- a user can dispute a finding
//     whether or not coder mode ever ran.
func runLessonsRecord(args []string) {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, recordUsage)
		os.Exit(2)
	}
	kind, shape := args[0], args[1]
	surface, ok := recordableKinds[kind]
	if !ok {
		fmt.Fprintln(os.Stderr, recordUsage)
		os.Exit(2)
	}
	if !shapeRe.MatchString(shape) {
		fmt.Fprintln(os.Stderr, `usage: deadeye lessons record <kind> <lens:tag>  (shape must look like "security:inject")`)
		os.Exit(2)
	}
	if kind == "coder-miss" && !coderModeActive() {
		os.Exit(1)
	}
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(1)
	}
	o := lessons.Outcome{
		TS:        nowRFC3339(),
		Surface:   surface,
		TaskShape: shape,
		Kind:      kind,
		Weight:    lessons.WeightMiss,
		Repo:      gitutil.ProjectKey(cwd),
	}
	if err := lessons.Open(meta.OutcomesPath()).Append(o); err != nil {
		os.Exit(1)
	}
}

// coderModeActive reports whether the global coder-mode state file
// (written at every SessionStart and by every /deadeye-coder switch,
// cmd/deadeye/coder.go's writeCoderModeFile) currently holds an active
// level. Fail-open to false, same posture as every other read of this
// file: a missing file (coder mode never enabled, or its per-session file
// already cleaned up at SessionEnd) means "not active", not an error.
func coderModeActive() bool {
	b, err := os.ReadFile(meta.CoderModePath())
	if err != nil {
		return false
	}
	level := strings.TrimSpace(string(b))
	return level != "" && level != coder.LevelOff
}

// rewriteOutcomes atomically replaces outcomes.jsonl with exactly kept --
// used by a filtered reset, which is a delete-some, not delete-all, so it
// can't just os.Remove the file. Temp+rename keeps a concurrent Scan (the
// daemon's own startup load, or another `deadeye lessons` invocation) from
// ever observing a torn file, same discipline as codemap's atomicWriteFile.
func rewriteOutcomes(path string, kept []lessons.Outcome) error {
	var buf bytes.Buffer
	for _, o := range kept {
		b, err := json.Marshal(o)
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".outcomes-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_, werr := tmp.Write(buf.Bytes())
	cerr := tmp.Close()
	if werr != nil {
		os.Remove(tmpPath)
		return werr
	}
	if cerr != nil {
		os.Remove(tmpPath)
		return cerr
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
