package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
)

// TestLooksImplementationShapedIgnoresSubstrings is the regression test
// for D1: plain strings.Contains matched an implementation verb as a
// SUBSTRING of an unrelated word, firing the plan gate on pure questions.
// Verified live, each of these: "prefix" (fix), "address"/"padding" (add),
// "removed" (remove/move), "changelog" (change).
func TestLooksImplementationShapedIgnoresSubstrings(t *testing.T) {
	falsePositives := []string{
		"explain the prefix handling in the address parser",
		"what does the changelog say",
		"why was this removed",
		"how does padding work here",
	}
	for _, p := range falsePositives {
		if looksImplementationShaped(p) {
			t.Errorf("looksImplementationShaped(%q) = true, want false (verb matched as a substring of an unrelated word)", p)
		}
	}

	realRequests := []string{
		"add a new field to the struct",
		"adds a new field",   // -s inflection
		"adding a new field", // -ing inflection
		"implement the new endpoint",
		"fix the parser",
	}
	for _, p := range realRequests {
		if !looksImplementationShaped(p) {
			t.Errorf("looksImplementationShaped(%q) = false, want true", p)
		}
	}
}

// TestPlanGateThresholdDiscipline is PLAN.md §5.4's explicit requirement:
// "single-file, specific, low-radius prompts must pass through with zero
// friction." Vague prompts should trigger regardless of the working tree;
// purely exploratory prompts (no implementation verb) never should.
func TestPlanGateThresholdDiscipline(t *testing.T) {
	cfg := config.PlanGate{MinFiles: 2}

	cases := []struct {
		name     string
		prompt   string
		wantFire bool
	}{
		{"specific single-file edit", "Rename the variable x to count in main.go", false},
		{"pure question, no implementation verb", "What does the daemon do when the socket already exists?", false},
		{"vague implementation request", "Not sure, maybe look into a redesign of the architecture across the codebase?", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := hookio.Input{Prompt: c.prompt, Cwd: t.TempDir()}
			_, _, fire := planGateSoftTrigger(in, cfg)
			if fire != c.wantFire {
				t.Errorf("planGateSoftTrigger(%q) fired=%v, want %v", c.prompt, fire, c.wantFire)
			}
		})
	}
}

// planRepo makes a git repo with a.go..c.go committed and every one of
// them modified (unstaged), i.e. the busy dirty tree that used to make the
// gate fire on any prompt containing "fix" or "change".
func planRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	files := []string{"a.go", "b.go", "c.go"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("package p\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", ".")
	run("commit", "-q", "-m", "initial")
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("package p\n// changed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestPlanGateMeasuresThePromptNotTheWorkingTree replaces
// TestPlanGateFiresOnMultiFileWorkingTree, which pinned the opposite
// intent: a dirty tree says nothing about the size of THIS request. With 3
// modified files, prompts naming 0 or 1 of them must stay quiet; a prompt
// naming 3 real files and "refactor" must still fire.
func TestPlanGateMeasuresThePromptNotTheWorkingTree(t *testing.T) {
	dir := planRepo(t)
	cfg := config.PlanGate{MinFiles: 2}
	cases := []struct {
		name, prompt string
		want         bool
	}{
		{"names no file", "Fix the typo in the readme wording", false},
		{"no code changes yet", "ask pm to review this, no code changes yet", false},
		{"names one file", "Update a.go to add a new field", false},
		{"names two files", "Update a.go and b.go to add a new field", true},
		{"names three files, refactor", "Refactor a.go, b.go and c.go to share one helper", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, fire := planGateSoftTrigger(hookio.Input{Prompt: c.prompt, Cwd: dir}, cfg)
			if fire != c.want {
				t.Errorf("fired=%v, want %v for %q", fire, c.want, c.prompt)
			}
		})
	}
}

// TestPlanGateQuestionMarksAreNotVagueness: promptshape adds 0.1 per "?",
// so three honest questions crossed the 0.3 line on their own. A prompt that
// ends in "?" is scored without them; real vague wording still fires (see
// TestPlanGateSoftFiresOncePerMarker's prompt).
func TestPlanGateQuestionMarksAreNotVagueness(t *testing.T) {
	prompt := "Should we change the retry logic? What about the timeout? And the backoff?"
	_, _, fire := planGateSoftTrigger(hookio.Input{Prompt: prompt, Cwd: t.TempDir()}, config.PlanGate{MinFiles: 2})
	if fire {
		t.Errorf("plan gate fired on a pure question: %q", prompt)
	}
}

// TestPlanGateLongBriefIsNotVague: a pasted multi-paragraph brief is the
// opposite of vague. The same broad wording in a short prompt still fires.
func TestPlanGateLongBriefIsNotVague(t *testing.T) {
	cfg := config.PlanGate{MinFiles: 2}
	short := "Not sure, maybe look into a redesign of the architecture across the codebase"
	long := short + ". " + strings.Repeat("The requirements, owners, deadlines and acceptance checks for each part are listed below in detail. ", 8)
	if _, _, fire := planGateSoftTrigger(hookio.Input{Prompt: short, Cwd: t.TempDir()}, cfg); !fire {
		t.Fatal("control: the short vague prompt should fire")
	}
	if _, _, fire := planGateSoftTrigger(hookio.Input{Prompt: long, Cwd: t.TempDir()}, cfg); fire {
		t.Error("a 100+ word brief fired as vague scope")
	}
}

// TestPlanGateIgnoresEnvelopesAnywhere feeds the gate the raw prompt field
// of hook inputs that are not user requests. Each would fire if typed by a
// user (implementation verb + vague words); none may, including envelopes
// that do not start with "<".
func TestPlanGateIgnoresEnvelopesAnywhere(t *testing.T) {
	body := "fix this somehow, maybe look into the whole architecture and rewrite it"
	prompts := map[string]string{
		"teammate message":      `<teammate-message teammate_id="eng" color="blue">` + body + `</teammate-message>`,
		"teammate after text":   "queued: <teammate-message teammate_id=\"eng\">" + body + "</teammate-message>",
		"idle notification":     `{"type":"idle_notification","from":"eng","note":"` + body + `"}`,
		"task notification":     "  \n<task-notification><summary>" + body + "</summary></task-notification>",
		"task notification mid": "background job done. <task-notification>" + body + "</task-notification>",
		"compact summary":       "This session is being continued from a previous conversation that ran out of context. " + body,
	}
	for name, prompt := range prompts {
		t.Run(name, func(t *testing.T) {
			state := newDaemonState(catalog.Catalog{}, nil)
			in := hookio.Input{SessionID: "s", Prompt: prompt, Cwd: t.TempDir()}
			if _, fired := decidePlanGateSoft(in, config.Default(), state); fired {
				t.Errorf("plan gate fired on %s", name)
			}
		})
	}
	// Control: the same body typed by a user does fire, so the cases above
	// are not passing for an unrelated reason.
	state := newDaemonState(catalog.Catalog{}, nil)
	if _, fired := decidePlanGateSoft(hookio.Input{SessionID: "s", Prompt: body, Cwd: t.TempDir()}, config.Default(), state); !fired {
		t.Error("control prompt did not fire; envelope tests prove nothing")
	}
}

// TestPlanGateSoftFiresOncePerMarker is the regression test for D2: the
// soft gate previously re-suggested (and re-armed pendingPlanTask) on
// every turn a trigger condition was re-met, even for the byte-identical
// prompt -- INV-7's "never nag twice for the same trigger" only held
// within a single already-pending cycle, not across the whole session.
// Uses a vague prompt (fires on prompt shape alone) so this doesn't need a
// working tree.
func TestPlanGateSoftFiresOncePerMarker(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	cfg := config.Default()
	prompt := "Not sure, maybe look into a redesign of the architecture across the codebase?"
	in := hookio.Input{SessionID: "s1", Prompt: prompt, Cwd: t.TempDir()}

	if _, fired := decidePlanGateSoft(in, cfg, state); !fired {
		t.Fatal("expected the first call to fire")
	}
	if _, fired := decidePlanGateSoft(in, cfg, state); fired {
		t.Error("plan gate re-fired for the identical prompt marker within the same session")
	}

	// A genuinely different implementation-shaped prompt is a different
	// task and must still be able to fire.
	other := hookio.Input{SessionID: "s1", Prompt: "Not sure, maybe look into a redesign of the billing architecture?", Cwd: t.TempDir()}
	if _, fired := decidePlanGateSoft(other, cfg, state); !fired {
		t.Error("a different implementation-shaped prompt should still fire -- it's a different task")
	}
}

// TestTruncatedMarkerIsRuneSafe is the regression test for the other half
// of D2: marker[:60] byte-slicing could split a multi-byte UTF-8 rune in
// half, corrupting the marker stored in session state and logged to
// decisions.jsonl.
func TestTruncatedMarkerIsRuneSafe(t *testing.T) {
	prompt := strings.Repeat("界", 70) // 70 runes, 3 bytes each
	got := truncatedMarker(prompt)
	if !utf8.ValidString(got) {
		t.Fatalf("truncatedMarker produced invalid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != 60 {
		t.Errorf("rune count = %d, want 60", n)
	}
}

// TestPlanGateIgnoresSyntheticPrompts is the regression test for a real
// bug caught live: a background subagent completing delivers a
// `<task-notification>...` payload through a genuine UserPromptSubmit
// event, and the plan gate's keyword heuristics fired on it as if a user
// had typed an implementation request.
func TestPlanGateIgnoresSyntheticPrompts(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	synthetic := "<task-notification>\n<task-id>abc123</task-id>\n<summary>Agent finished, please add these changes</summary>\n</task-notification>"
	in := hookio.Input{SessionID: "s1", Prompt: synthetic, Cwd: t.TempDir()}
	if _, fired := decidePlanGateSoft(in, config.Default(), state); fired {
		t.Error("plan gate fired on a synthetic task-notification prompt")
	}
}

func TestPlanGateHardDefaultsOff(t *testing.T) {
	state := newDaemonState(catalog.Catalog{}, nil)
	state.setPendingPlan("sess1", "some pending task")

	out := decidePlanGateHard(hookio.Input{SessionID: "sess1", ToolName: "Edit"}, config.Default(), state)
	if out.HookSpecificOutput != nil {
		t.Errorf("hard layer fired with default config (plan_gate=%q), want no-op", config.Default().Mode.PlanGate)
	}
}

func TestPlanGateHardAsksOnceThenClears(t *testing.T) {
	cfg := config.Default()
	cfg.Mode.PlanGate = "hard"
	state := newDaemonState(catalog.Catalog{}, nil)
	state.setPendingPlan("sess1", "pending task")

	in := hookio.Input{SessionID: "sess1", ToolName: "Edit"}
	first := decidePlanGateHard(in, cfg, state)
	if first.HookSpecificOutput == nil || first.HookSpecificOutput.PermissionDecision != hookio.PermissionAsk {
		t.Fatalf("first Edit with a pending plan = %+v, want permissionDecision=ask", first)
	}

	second := decidePlanGateHard(in, cfg, state)
	if second.HookSpecificOutput != nil {
		t.Errorf("second Edit after the gate already asked once = %+v, want no-op (gate clears after asking)", second)
	}
}
