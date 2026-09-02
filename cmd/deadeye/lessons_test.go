package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/coder"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/gitutil"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
	"github.com/deepaksinghcs14/deadeye-cc/internal/signals"
)

func testCatalogForLessons() catalog.Catalog {
	return catalog.Catalog{Models: []catalog.Model{
		{ID: "cheap-id", Family: "haiku", Tier: 0},
		{ID: "mid-id", Family: "sonnet", Tier: 1},
		{ID: "top-id", Family: "opus", Tier: 2},
	}}
}

func TestCheckEscalationDetectsHigherTierRequest(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // newDaemonState seeds its outcome cache from disk -- isolate from the real ~/.deadeye
	state := newDaemonState(testCatalogForLessons(), nil)
	state.setLastRouting("s1", "shape-a", "cheap-id", "low", 0)

	checkEscalation(hookio.Input{SessionID: "s1"}, agentInput{Model: "opus"}, "shape-b", state)

	got := state.outcomesSnapshot()
	if len(got) != 1 {
		t.Fatalf("got %d outcomes, want 1", len(got))
	}
	if got[0].Kind != "escalation" || got[0].TaskShape != "shape-a" || got[0].Model != "cheap-id" {
		t.Errorf("outcome = %+v, want escalation recorded against the PRIOR decision (shape-a/cheap-id)", got[0])
	}
}

func TestCheckEscalationIgnoresSameOrLowerTier(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	state.setLastRouting("s1", "shape-a", "top-id", "high", 2)

	checkEscalation(hookio.Input{SessionID: "s1"}, agentInput{Model: "opus"}, "shape-b", state)  // same tier
	checkEscalation(hookio.Input{SessionID: "s1"}, agentInput{Model: "haiku"}, "shape-b", state) // lower tier

	if got := state.outcomesSnapshot(); len(got) != 0 {
		t.Errorf("got %d outcomes, want 0 for same/lower-tier requests", len(got))
	}
}

func TestCheckEscalationNoopWithoutPriorRouting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	checkEscalation(hookio.Input{SessionID: "new-session"}, agentInput{Model: "opus"}, "shape-a", state)
	if got := state.outcomesSnapshot(); len(got) != 0 {
		t.Errorf("got %d outcomes, want 0 with no prior routing decision to compare against", len(got))
	}
}

func TestCheckEscalationNoopWithoutExplicitModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	state.setLastRouting("s1", "shape-a", "cheap-id", "low", 0)
	checkEscalation(hookio.Input{SessionID: "s1"}, agentInput{}, "shape-b", state)
	if got := state.outcomesSnapshot(); len(got) != 0 {
		t.Errorf("got %d outcomes, want 0 when the caller didn't request a specific model", len(got))
	}
}

// TestCheckEscalationClearsLastRoutingAfterRecording is the regression
// test for C3: an outcome must be consumed once. Before this fix, nothing
// cleared lastRouting after grading it, so two consecutive explicit-model
// Agent calls recorded TWO escalation outcomes against the same single
// prior recommendation.
func TestCheckEscalationClearsLastRoutingAfterRecording(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	state.setLastRouting("s1", "shape-a", "cheap-id", "low", 0)

	checkEscalation(hookio.Input{SessionID: "s1"}, agentInput{Model: "opus"}, "shape-b", state)
	checkEscalation(hookio.Input{SessionID: "s1"}, agentInput{Model: "opus"}, "shape-b", state)

	if got := state.outcomesSnapshot(); len(got) != 1 {
		t.Errorf("got %d outcomes, want exactly 1 -- the prior decision should only be graded once", len(got))
	}
}

// TestDecideAgentRoutingSkipsLastRoutingForExplicitModel is the regression
// test for C3's other half: when the caller supplies an explicit model,
// deadeye's own recommendation was never applied, so there's nothing to
// grade against on the NEXT call. Before this fix, setLastRouting ran
// unconditionally, so two consecutive Agent calls that both passed an
// explicit model (routine for e.g. a workflow script that always pins
// model itself) recorded a phantom escalation nobody triggered.
func TestDecideAgentRoutingSkipsLastRoutingForExplicitModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	toolInput, err := json.Marshal(map[string]any{"description": "d", "prompt": "p", "model": "opus"})
	if err != nil {
		t.Fatal(err)
	}
	in := hookio.Input{SessionID: "s1", ToolName: "Agent", Cwd: t.TempDir(), ToolInput: toolInput}
	decideAgentRouting(in, config.Default(), state)

	if lr := state.getLastRouting("s1"); lr != nil {
		t.Errorf("getLastRouting = %+v, want nil -- an explicit caller model means deadeye's recommendation was never applied", lr)
	}
}

// TestDecideAgentRoutingOmitsEffortWhenModeOff is the regression test for
// the mode.effort knob's other half: with effort "off", the Agent
// recommendation must not carry an effort suggestion. Previously the knob
// was printed by /deadeye-status but nothing read it.
func TestDecideAgentRoutingOmitsEffortWhenModeOff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	toolInput, err := json.Marshal(map[string]any{"description": "d", "prompt": "p"})
	if err != nil {
		t.Fatal(err)
	}
	in := hookio.Input{SessionID: "s1", ToolName: "Agent", Cwd: t.TempDir(), ToolInput: toolInput}

	cfg := config.Default()
	cfg.Mode.Effort = "off"
	out := decideAgentRouting(in, cfg, state)
	if out.HookSpecificOutput == nil {
		t.Fatal("expected an advisory output")
	}
	if strings.Contains(out.HookSpecificOutput.AdditionalContext, "effort=") {
		t.Errorf("recommendation still mentions effort with mode.effort=off: %q", out.HookSpecificOutput.AdditionalContext)
	}

	out = decideAgentRouting(hookio.Input{SessionID: "s2", ToolName: "Agent", Cwd: t.TempDir(), ToolInput: toolInput}, config.Default(), state)
	if out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "effort=") {
		t.Error("recommendation should mention effort with the default mode.effort=advise")
	}
}

func TestTaskShapeKeyBucketsFileCount(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{{0, "0"}, {1, "1"}, {3, "2-3"}, {8, "4-8"}, {50, "9+"}}
	for _, c := range cases {
		files := make([]string, c.n)
		key := taskShapeKey(files, "add a feature", nil)
		if want := "files=" + c.want; !strings.Contains(key, want) {
			t.Errorf("taskShapeKey with %d files = %q, want it to contain %q", c.n, key, want)
		}
	}
}

func TestTaskShapeKeyReflectsTestPresence(t *testing.T) {
	withTests := taskShapeKey([]string{"a.go"}, "add x", []signals.Evidence{
		{Provider: "testpresence", Facts: map[string]any{"files_with_adjacent_test": 1}},
	})
	withoutTests := taskShapeKey([]string{"a.go"}, "add x", []signals.Evidence{
		{Provider: "testpresence", Facts: map[string]any{"files_with_adjacent_test": 0}},
	})
	if withTests == withoutTests {
		t.Errorf("task shape key did not distinguish test presence: %q == %q", withTests, withoutTests)
	}
}

func TestSurfaceArgParsesValue(t *testing.T) {
	if got := surfaceArg([]string{"--surface", "coder"}); got != "coder" {
		t.Errorf("got %q, want %q", got, "coder")
	}
}

func TestSurfaceArgAbsentReturnsEmpty(t *testing.T) {
	if got := surfaceArg([]string{"reset"}); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestRunLessonsGroupsBySurface: mixed-surface outcomes must render under
// their own section header, in surfaceDisplayOrder, so a repo's coder
// misses and pr-review signals are as inspectable as routing escalations
// always were.
func TestRunLessonsGroupsBySurface(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := lessons.Open(meta.OutcomesPath())
	store.Append(lessons.Outcome{TS: "2026-08-02T00:00:00Z", TaskShape: "files=1,impl=true,tests=false", Kind: "escalation", Weight: 1.0})
	store.Append(lessons.Outcome{TS: "2026-08-02T00:00:00Z", Surface: lessons.SurfaceCoder, Repo: "deadeye-cc", TaskShape: "security:inject", Kind: "coder-miss", Weight: 1.0})

	out := captureStdout(t, func() { runLessons(nil) })

	routingIdx := strings.Index(out, "routing")
	coderIdx := strings.Index(out, "coder")
	if routingIdx == -1 || coderIdx == -1 || routingIdx > coderIdx {
		t.Errorf("expected a routing section before a coder section, got:\n%s", out)
	}
	if !strings.Contains(out, "security:inject") {
		t.Errorf("coder-miss outcome missing from output:\n%s", out)
	}
}

// TestRunLessonsResetSurfaceKeepsOtherSurfaces is the regression test for
// the filtered reset: `reset --surface coder` must remove only coder rows,
// never touch routing's escalation history or another repo's pr-review
// rows -- an unscoped reset already covers "clear everything".
func TestRunLessonsResetSurfaceKeepsOtherSurfaces(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := lessons.Open(meta.OutcomesPath())
	store.Append(lessons.Outcome{TS: "2026-08-02T00:00:00Z", TaskShape: "files=1,impl=true,tests=false", Kind: "escalation", Weight: 1.0})
	store.Append(lessons.Outcome{TS: "2026-08-02T00:00:00Z", Surface: lessons.SurfaceCoder, Repo: "deadeye-cc", TaskShape: "security:inject", Kind: "coder-miss", Weight: 1.0})

	captureStdout(t, func() { runLessons([]string{"reset", "--surface", "coder"}) })

	got, err := lessons.Scan(meta.OutcomesPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "escalation" {
		t.Errorf("got %+v, want only the routing escalation left", got)
	}
}

func TestShapeRegexAcceptsLensTag(t *testing.T) {
	for _, shape := range []string{"security:inject", "correctness:leak", "over-engineering:yagni"} {
		if !shapeRe.MatchString(shape) {
			t.Errorf("shapeRe rejected valid shape %q", shape)
		}
	}
}

func TestShapeRegexRejectsMalformed(t *testing.T) {
	for _, shape := range []string{"inject", "security:", ":inject", "security inject", "SECURITY:INJECT"} {
		if shapeRe.MatchString(shape) {
			t.Errorf("shapeRe accepted malformed shape %q", shape)
		}
	}
}

// TestCoderModeActiveReadsGlobalFile: coderModeActive is the gate that
// keeps a coder-miss from being attributed when coder mode never ran.
func TestCoderModeActiveReadsGlobalFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if coderModeActive() {
		t.Error("no coder-mode file at all should read as inactive")
	}
	os.MkdirAll(meta.StateDir(), 0o700)
	os.WriteFile(meta.CoderModePath(), []byte("marksman\n"), 0o600)
	if !coderModeActive() {
		t.Error("a file holding an active level should read as active")
	}
	os.WriteFile(meta.CoderModePath(), []byte(coder.LevelOff+"\n"), 0o600)
	if coderModeActive() {
		t.Error("a file holding \"off\" should read as inactive")
	}
}

// TestRunLessonsRecordCoderMissWritesWhenActive is the happy path: valid
// kind/shape, coder mode active, a real (temp) cwd -- exercises the full
// runLessonsRecord write, the only path that never calls os.Exit.
func TestRunLessonsRecordCoderMissWritesWhenActive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repoDir := t.TempDir()
	t.Chdir(repoDir)
	os.MkdirAll(meta.StateDir(), 0o700)
	os.WriteFile(meta.CoderModePath(), []byte("marksman\n"), 0o600)

	runLessonsRecord([]string{"coder-miss", "security:inject"})

	got, err := lessons.Scan(meta.OutcomesPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d outcomes, want 1", len(got))
	}
	o := got[0]
	if o.Surface != lessons.SurfaceCoder || o.Kind != "coder-miss" || o.TaskShape != "security:inject" || o.Weight != lessons.WeightMiss {
		t.Errorf("got %+v, want a coder-surface coder-miss outcome for security:inject weight %v", o, lessons.WeightMiss)
	}
	if o.Repo != gitutil.ProjectKey(repoDir) {
		t.Errorf("Repo = %q, want gitutil.ProjectKey(%q) = %q", o.Repo, repoDir, gitutil.ProjectKey(repoDir))
	}
}

// TestRunLessonsRecordReviewFalsePositiveIgnoresCoderMode: unlike
// coder-miss, a dispute doesn't require coder mode to have been active --
// a user can dismiss a finding regardless of who wrote the code.
func TestRunLessonsRecordReviewFalsePositiveIgnoresCoderMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	runLessonsRecord([]string{"review-false-positive", "correctness:race"})

	got, err := lessons.Scan(meta.OutcomesPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Surface != lessons.SurfacePRReview || got[0].Kind != "review-false-positive" {
		t.Errorf("got %+v, want one pr-review review-false-positive outcome", got)
	}
}

// TestRunLessonsRecordExternalMissIgnoresCoderMode is the regression test
// for Phase E: unlike coder-miss, an external-miss (another reviewer caught
// something /deadeye-pr didn't) must record even with no coder-mode file
// present -- "was coder mode on right now" can't be answered for a PR
// reviewed after the fact, so it carries no such gate.
func TestRunLessonsRecordExternalMissIgnoresCoderMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	runLessonsRecord([]string{"external-miss", "security:authz"})

	got, err := lessons.Scan(meta.OutcomesPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Surface != lessons.SurfaceCoder || got[0].Kind != "external-miss" {
		t.Errorf("got %+v, want one coder-surface external-miss outcome", got)
	}
}

// TestExternalMissFeedsSameConsumptionAsCoderMiss confirms the whole point
// of sharing SurfaceCoder: RecentShapes filters by surface, never kind, so
// an external-miss shows up in both the SessionStart reminder and
// `deadeye lessons priority`'s "scrutinize harder" line exactly like a
// coder-miss does, with zero new consumption code.
func TestExternalMissFeedsSameConsumptionAsCoderMiss(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repoDir := t.TempDir()
	t.Chdir(repoDir)

	runLessonsRecord([]string{"external-miss", "security:authz"})

	state := newDaemonState(testCatalogForLessons(), nil)
	if got := lessonsMissesText(state, repoDir); !strings.Contains(got, "security:authz") {
		t.Errorf("SessionStart reminder = %q, want it to contain the external-miss shape", got)
	}
	if got := captureStdout(t, runLessonsPriority); !strings.Contains(got, "security:authz") {
		t.Errorf("lessons priority = %q, want it to contain the external-miss shape", got)
	}
}

// TestLessonsMissesTextRendersCappedList exercises the injection text
// end to end: reloadOutcomes-then-RecentShapes-then-render, the same path
// decide.go's UserPromptSubmit handler takes.
func TestLessonsMissesTextRendersCappedList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repoDir := t.TempDir()
	state := newDaemonState(testCatalogForLessons(), nil)
	repo := gitutil.ProjectKey(repoDir)
	store := lessons.Open(meta.OutcomesPath())
	store.Append(lessons.Outcome{TS: nowRFC3339(), Surface: lessons.SurfaceCoder, Repo: repo, TaskShape: "security:inject", Kind: "coder-miss", Weight: 1.0})
	store.Append(lessons.Outcome{TS: nowRFC3339(), Surface: lessons.SurfaceCoder, Repo: repo, TaskShape: "security:inject", Kind: "coder-miss", Weight: 1.0})
	state.reloadOutcomes()

	got := lessonsMissesText(state, repoDir)
	if !strings.Contains(got, "security:inject (2×)") {
		t.Errorf("got %q, want it to contain \"security:inject (2×)\"", got)
	}
}

// TestRunLessonsPriorityReportsBothDirections: /deadeye-pr's consumption
// needs both signals in one read -- coder misses (scrutinize harder) and
// disputes (need stronger proof) for the SAME repo, never mixed with
// another repo's history.
func TestRunLessonsPriorityReportsBothDirections(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repoDir := t.TempDir()
	t.Chdir(repoDir)
	repo := gitutil.ProjectKey(repoDir)
	store := lessons.Open(meta.OutcomesPath())
	store.Append(lessons.Outcome{TS: nowRFC3339(), Surface: lessons.SurfaceCoder, Repo: repo, TaskShape: "security:inject", Kind: "coder-miss", Weight: 1.0})
	store.Append(lessons.Outcome{TS: nowRFC3339(), Surface: lessons.SurfacePRReview, Repo: repo, TaskShape: "correctness:race", Kind: "review-false-positive", Weight: 1.0})
	store.Append(lessons.Outcome{TS: nowRFC3339(), Surface: lessons.SurfaceCoder, Repo: "some-other-repo", TaskShape: "security:secret", Kind: "coder-miss", Weight: 1.0})

	out := captureStdout(t, runLessonsPriority)

	if !strings.Contains(out, "security:inject") {
		t.Errorf("missing this repo's coder-miss signal:\n%s", out)
	}
	if !strings.Contains(out, "correctness:race") {
		t.Errorf("missing this repo's dispute signal:\n%s", out)
	}
	if strings.Contains(out, "security:secret") {
		t.Errorf("leaked another repo's outcome into this repo's priority view:\n%s", out)
	}
}

func TestRunLessonsPriorityEmptyWhenNoOutcomes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	out := captureStdout(t, runLessonsPriority)
	if !strings.Contains(out, "No repo-scoped priority signal yet.") {
		t.Errorf("got %q, want the no-signal message", out)
	}
}

func TestLessonsMissesTextEmptyWhenNoOutcomes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	state := newDaemonState(testCatalogForLessons(), nil)
	if got := lessonsMissesText(state, t.TempDir()); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestMissesInjectionSizeBudget bounds the worst-case rendered "recent
// misses" line -- missesToShow already caps the item COUNT at 3; this
// bounds the resulting BYTE size, using the longest lens ("over-engineering",
// 16 chars) and longest tag ("complexity", 10 chars) in
// internal/prreview/ruleset.md's taxonomy (not a real pairing -- an upper
// bound doesn't need to be), double-digit counts, and 3 distinct shapes so
// the cap is actually exercised. Growth in the taxonomy, missesToShow, or
// the template must stay a deliberate, visible change here, the same
// discipline internal/coder/size_test.go applies to the ruleset injection.
func TestMissesInjectionSizeBudget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repoDir := t.TempDir()
	state := newDaemonState(testCatalogForLessons(), nil)
	repo := gitutil.ProjectKey(repoDir)
	store := lessons.Open(meta.OutcomesPath())
	for _, shape := range []string{"over-engineering:complexity-a", "over-engineering:complexity-b", "over-engineering:complexity-c"} {
		for range 9 {
			store.Append(lessons.Outcome{TS: nowRFC3339(), Surface: lessons.SurfaceCoder, Repo: repo, TaskShape: shape, Kind: "coder-miss", Weight: 1.0})
		}
	}
	state.reloadOutcomes()

	if n := len(lessonsMissesText(state, repoDir)); n > 300 {
		t.Errorf("worst-case 3-shape misses injection is %d bytes -- trim before missesToShow or the shape vocabulary grows further", n)
	}
}
