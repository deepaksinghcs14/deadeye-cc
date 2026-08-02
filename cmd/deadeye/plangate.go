package main

import (
	"context"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/signals"
)

// implementationVerbs is a small allowlist distinguishing "build/change
// something" prompts from purely exploratory or informational ones
// ("what does X do", "explain Y") -- the plan gate must not fire on the
// latter (PLAN.md §5.4: "single-file, specific, low-radius prompts must
// pass through with zero friction").
var implementationVerbs = []string{
	"add", "implement", "fix", "refactor", "build", "create", "change",
	"update", "remove", "delete", "write", "modify", "rewrite", "migrate",
	"replace", "rename", "move", "redesign",
}

func looksImplementationShaped(prompt string) bool {
	p := strings.ToLower(prompt)
	for _, v := range implementationVerbs {
		if strings.Contains(p, v) {
			return true
		}
	}
	return false
}

// isSyntheticPrompt reports whether a UserPromptSubmit's Prompt is
// actually a system-generated notification (e.g. a background task
// finishing) rather than something the user typed. Caught live: a
// background subagent completion delivered its `<task-notification>...`
// payload through a real UserPromptSubmit event, and the plan gate's
// keyword/vagueness heuristics fired on it as if it were a genuine
// implementation request. Real user prompts don't start with a raw XML
// tag, so this is a cheap, general guard rather than hardcoding just the
// one tag name observed.
func isSyntheticPrompt(prompt string) bool {
	return strings.HasPrefix(strings.TrimSpace(prompt), "<")
}

// planGateSoftTrigger evaluates PLAN.md §5.4's soft-layer triggers: (a)
// vague enough to cause broad scanning, (b) multi-file scope. (c) "blast
// radius > 0" needs the optional greybeard provider, which this plugin
// doesn't depend on (§13 non-goals) -- degrades to just (a)/(b), which is
// the correct behavior per PLAN.md §3.1's "absent or erroring -> kernel
// proceeds with weaker evidence."
func planGateSoftTrigger(in hookio.Input, cfg config.PlanGate) (suggestion, marker string, fire bool) {
	if in.Prompt == "" || !looksImplementationShaped(in.Prompt) {
		return "", "", false
	}

	scope := signals.Scope{Prompt: in.Prompt, Files: scopedFiles(in.Cwd), Repo: in.Cwd}
	evidence := signals.AssessAll(context.Background(), scope, []signals.Signal{signals.PromptShape{}, signals.FileScope{}})

	vague, multiFile := false, false
	for _, e := range evidence {
		switch e.Provider {
		case "promptshape":
			if e.Complexity >= 0.3 {
				vague = true
			}
		case "filescope":
			if n, ok := e.Facts["file_count"].(int); ok && n >= cfg.MinFiles {
				multiFile = true
			}
		}
	}

	if !vague && !multiFile {
		return "", "", false
	}

	reason := "vague scope"
	if multiFile && vague {
		reason = "vague scope, multiple files touched"
	} else if multiFile {
		reason = "multiple files touched"
	}

	marker = in.Prompt
	if len(marker) > 60 {
		marker = marker[:60]
	}
	suggestion = "deadeye: plan gate triggered -- " + reason + ". Before editing files for this task, present a short plan (approach, files to touch, verification step) and wait for the user's go-ahead."
	return suggestion, marker, true
}

// decidePlanGateSoft is called from decideUserPromptSubmit on every turn
// (not just the first) -- a task can start on turn 3 just as easily as
// turn 1.
func decidePlanGateSoft(in hookio.Input, state *daemonState) (suggestion string, fired bool) {
	if state.cfg.Mode.PlanGate == "off" || config.KillSwitchOff("DEADEYE_GATE") || isSyntheticPrompt(in.Prompt) {
		return "", false
	}
	suggestion, marker, fire := planGateSoftTrigger(in, state.cfg.PlanGate)
	if !fire {
		return "", false
	}
	state.setPendingPlan(in.SessionID, marker)
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "UserPromptSubmit", Action: "gate-suggest", Reason: marker})
	return suggestion, true
}

// decidePlanGateHard is PLAN.md §5.4's hard layer: PreToolUse on Edit/Write
// asks for permission when a plan is pending and no consent was recorded.
// Default off (cfg.Mode.PlanGate must be exactly "hard", opted into
// explicitly) -- "hard layer default: off until the soft layer's
// precision is verified on real usage."
//
// INV-7 caveat: this hook surface has no way to observe whether the
// user's subsequent answer to the native permission prompt was approve or
// decline -- there's no follow-up hook call carrying that outcome. So the
// gate is cleared immediately after asking once, rather than left pending
// indefinitely: "consent is sticky" is satisfied in the safe direction
// (never nags twice for the same trigger) even though it can't
// distinguish "approved" from "declined" to log the outcome precisely.
func decidePlanGateHard(in hookio.Input, state *daemonState) hookio.Output {
	if state.cfg.Mode.PlanGate != "hard" || config.KillSwitchOff("DEADEYE_GATE") {
		state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/" + in.ToolName, Action: "noop"})
		return hookio.Empty()
	}
	pending := state.pendingPlan(in.SessionID)
	if pending == "" {
		return hookio.Empty()
	}
	state.clearPendingPlan(in.SessionID)

	out := hookio.ForEvent("PreToolUse")
	out.HookSpecificOutput.PermissionDecision = hookio.PermissionAsk
	out.HookSpecificOutput.PermissionDecisionReason = "deadeye: plan gate asked -- this looks like a multi-file/high-radius change and no plan was approved. Approve to proceed, or ask for a plan."
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/" + in.ToolName, Action: "gate-ask", Reason: pending})
	return out
}
