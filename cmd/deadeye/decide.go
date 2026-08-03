package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/inject"
	"github.com/deepaksinghcs14/deadeye-cc/internal/kernel"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/preprocess"
	"github.com/deepaksinghcs14/deadeye-cc/internal/proto"
	"github.com/deepaksinghcs14/deadeye-cc/internal/sessionmem"
	"github.com/deepaksinghcs14/deadeye-cc/internal/signals"
)

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

// decide dispatches by event. A panic anywhere below still yields the
// canonical no-op response (INV-5) -- the recover here is the top of the
// policy call graph; individual handlers don't need their own.
//
// Config is loaded fresh here, once per request, from the SESSION's cwd
// (in.Cwd) and the client's real env-derived kill switches (req.Off) --
// never cached on daemonState across requests. See config.LoadFor's
// comment: the daemon is one long-lived process serving every project and
// session it's asked about, so a per-daemon-lifetime config would let one
// project's .deadeye.json (or a stale env snapshot) govern every other
// project's sessions too.
func decide(req proto.Request, state *daemonState) (out hookio.Output) {
	out = hookio.Empty()
	defer func() { recover() }()

	var in hookio.Input
	_ = json.Unmarshal(req.Payload, &in)
	cfg := config.LoadFor(in.Cwd, req.Off)

	switch req.Event {
	case "UserPromptSubmit":
		out = decideUserPromptSubmit(in, cfg, state)
	case "PreToolUse":
		out = decidePreToolUse(in, cfg, state)
	case "PostToolUse":
		out = decidePostToolUse(in, state)
	case "SubagentStart":
		out = decideSubagentStart(in, cfg, state)
	case "Stop":
		out = decideStop(in, state)
	case "SessionEnd":
		out = decideSessionEnd(in, state)
	default:
		state.log(logstore.Record{
			TS: nowRFC3339(), SessionID: in.SessionID, Surface: req.Event,
			Action: "noop",
		})
	}
	return out
}

// decideUserPromptSubmit delivers the once-per-session advisory injection
// and, on every turn, the plan-gate soft check (PLAN.md §5.4) and the
// workflow advisor (§5.5) -- all three share this event, so a turn that
// trips more than one gets a single combined additionalContext rather
// than separate hook responses (there's only one additionalContext slot
// per response).
//
// SessionStart cannot put anything in the model's context in Claude Code
// v2.1.220 (docs/verified.md §5.1); UserPromptSubmit's additionalContext
// is the confirmed-working replacement, gated to fire exactly once per
// session so it stays byte-stable (INV-4).
func decideUserPromptSubmit(in hookio.Input, cfg config.Config, state *daemonState) hookio.Output {
	var parts []string

	if state.markInjectedIfFirst(in.SessionID) {
		memory := sessionmem.LoadRecent(in.Cwd)
		text := inject.Build(state.cat, memory, cfg.Mode.Effort != "off")
		tokens := inject.EstimateTokens(text)
		reason := "session guidance injection"
		if tokens > cfg.InjectionBudgetTokens {
			reason = "session guidance injection (over INV-4 budget, shipped anyway -- trim before adding more)"
		}
		state.log(logstore.Record{
			TS: nowRFC3339(), SessionID: in.SessionID, Surface: "UserPromptSubmit",
			Action: "inject", Reason: reason, BytesAfter: len(text),
		})
		parts = append(parts, text)
	}

	if suggestion, fired := decidePlanGateSoft(in, cfg, state); fired {
		parts = append(parts, suggestion)
	}

	if suggestion, fired := decideWorkflowHint(in, cfg, state); fired {
		parts = append(parts, suggestion)
	}

	if len(parts) == 0 {
		state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "UserPromptSubmit", Action: "noop"})
		return hookio.Empty()
	}

	out := hookio.ForEvent("UserPromptSubmit")
	out.HookSpecificOutput.AdditionalContext = strings.Join(parts, "\n\n")
	return out
}

// decideStop shows a single terse line when new preprocessing savings have
// accrued since the last turn -- subtle by design: one line, only on
// change, phrased like the rest of Claude Code's own hook feedback rather
// than a banner. Stop fires once per turn, not once per session, so this
// naturally updates as the session progresses without repeating a stale
// total when nothing new happened.
func decideStop(in hookio.Input, state *daemonState) hookio.Output {
	bytesSaved, rewrites, changed := state.newSavingsToShow(in.SessionID)
	if !changed {
		state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "Stop", Action: "noop"})
		return hookio.Empty()
	}

	out := hookio.ForEvent("Stop")
	out.HookSpecificOutput.AdditionalContext = fmt.Sprintf(
		"deadeye: ~%d bytes kept out of context this session (%d rewrite%s).",
		bytesSaved, rewrites, plural(rewrites),
	)
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "Stop", Action: "savings-shown", BytesAfter: bytesSaved})
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// decideSessionEnd writes Phase 1.5's session-memory summary before the
// count of this session's own decisions is polluted by logging the
// SessionEnd event itself.
func decideSessionEnd(in hookio.Input, state *daemonState) hookio.Output {
	count := state.decisionCount(in.SessionID)
	_ = sessionmem.Write(in.Cwd, in.SessionID, count)
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "SessionEnd", Action: "noop"})
	// Session state is daemon-lifetime advisory dedup only, never
	// persisted -- but nothing ever removed an entry, so across a
	// long-lived daemon (idle timeout resets on every connection, so in
	// practice a daily user's daemon runs for as long as the machine is
	// up) every session id the machine has ever seen accumulated here
	// forever. Evict last, after the log/write above, so they still see
	// this session's real decisionCount.
	state.endSession(in.SessionID)
	return hookio.Empty()
}

type bashInput struct {
	Command string `json:"command"`
}

// decidePreToolUse runs Bash preprocessing rules (PLAN.md §5.3), Agent
// subagent routing (§5.2), and the plan-gate hard layer (§5.4) for
// Edit/Write.
func decidePreToolUse(in hookio.Input, cfg config.Config, state *daemonState) hookio.Output {
	switch in.ToolName {
	case "Bash":
		return decideBashPreprocess(in, cfg, state)
	case "Agent":
		return decideAgentRouting(in, cfg, state)
	case "Read":
		return decideReadAdvice(in, cfg, state)
	case "Edit", "Write":
		// An edit invalidates the consecutive-repeat heuristic: re-running
		// the same command AFTER a change is legitimate verification.
		state.clearLastBash(in.SessionID)
		return decidePlanGateHard(in, cfg, state)
	default:
		state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/" + in.ToolName, Action: "noop"})
		return hookio.Empty()
	}
}

type agentInput struct {
	Description  string `json:"description"`
	Prompt       string `json:"prompt"`
	SubagentType string `json:"subagent_type"`
	Model        string `json:"model"`
}

// decideAgentRouting recommends (advise) or rewrites (enforce) the model
// for a subagent delegation. Confirmed live (docs/verified.md §10.1/§10.2):
// PreToolUse fires for the Agent tool call with full tool_input visible,
// and the tool has no effort parameter at all -- only model, and only as
// the short family alias (sonnet|opus|haiku|fable), not a full model id.
//
// Enforce mode only fills in model when the caller left it unset. An
// explicit caller-chosen model is never overridden -- PLAN.md's "enforce
// may always raise, may lower only above the confidence threshold" is
// about the kernel's own default (which is already threshold-gated
// internally, see internal/kernel), not about second-guessing a model the
// caller specifically asked for.
func decideAgentRouting(in hookio.Input, cfg config.Config, state *daemonState) hookio.Output {
	if cfg.Mode.Routing == "off" {
		return hookio.Empty()
	}

	var ai agentInput
	_ = json.Unmarshal(in.ToolInput, &ai)

	scope := newScope(ai.Description+" "+ai.Prompt, in.Cwd)
	evidence := signals.AssessAll(context.Background(), scope, signals.Builtins())
	shape := taskShapeKey(scope.Files, scope.Prompt, evidence)
	threshold := lessons.AdjustedDownshiftThreshold(cfg.DownshiftThreshold, state.outcomesSnapshot(), shape, time.Now())
	decision := kernel.Decide(evidence, state.cat, threshold)

	checkEscalation(in, ai, shape, state)
	// Only record this decision as "last routing" when the caller left
	// model unset -- deadeye's recommendation was actually the one in
	// play. When the caller passed an explicit model, this decision was
	// never applied; recording it would let the NEXT call's explicit
	// model get graded as an "escalation" against advice nobody acted on
	// (verified: Claude passing model:"opus" on two consecutive Agent
	// calls recorded a phantom escalation the user never triggered).
	if ai.Model == "" {
		if tier, ok := state.cat.TierFor(decision.Model); ok {
			state.setLastRouting(in.SessionID, shape, decision.Model, decision.Effort, tier)
		}
	}

	out := hookio.ForEvent("PreToolUse")
	// mode.effort=off suppresses the effort half of the recommendation --
	// previously the knob was printed by /deadeye-status but nothing ever
	// read it, so turning it off silently did nothing.
	reason := fmt.Sprintf("deadeye recommends model=%s effort=%s -- %s", decision.Model, decision.Effort, decision.Reason)
	if cfg.Mode.Effort == "off" {
		reason = fmt.Sprintf("deadeye recommends model=%s -- %s", decision.Model, decision.Reason)
	}

	if cfg.Mode.Routing == "enforce" && ai.Model == "" {
		if family, ok := state.cat.FamilyFor(decision.Model); ok {
			updated, err := hookio.MergeToolInput(in.ToolInput, map[string]any{"model": family})
			if err == nil {
				out.HookSpecificOutput.UpdatedInput = updated
				out.HookSpecificOutput.PermissionDecision = hookio.PermissionAllow
				out.HookSpecificOutput.PermissionDecisionReason = reason
				state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/Agent", Action: "enforce", Reason: decision.Reason})
				return out
			}
		}
	}

	out.HookSpecificOutput.AdditionalContext = reason
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/Agent", Action: "advise", Reason: decision.Reason})
	return out
}

func decideBashPreprocess(in hookio.Input, cfg config.Config, state *daemonState) hookio.Output {
	if cfg.Mode.Preprocess != "on" {
		return hookio.Empty()
	}

	var bi bashInput
	if err := json.Unmarshal(in.ToolInput, &bi); err != nil || bi.Command == "" {
		return hookio.Empty()
	}

	// Consecutive-repeat check: the same command run twice with no
	// Edit/Write in between (Edit/Write clears the marker) is the
	// retry-loop pathology -- nothing changed, so the output won't either.
	repeat := state.noteBashCommand(in.SessionID, bi.Command)

	rule, newCmd, applied := preprocess.Apply(in.Cwd, bi.Command, cfg.DisabledRuleSet())
	if !applied {
		if repeat {
			out := hookio.ForEvent("PreToolUse")
			out.HookSpecificOutput.AdditionalContext = "deadeye: identical to the previous command, with no file changes in between -- the output is unlikely to differ."
			state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/Bash", Action: "advise", Reason: "repeat-command"})
			return out
		}
		return hookio.Empty()
	}

	out := hookio.ForEvent("PreToolUse")
	if rule.Advisory {
		out.HookSpecificOutput.AdditionalContext = "deadeye: " + rule.Note
		state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/Bash", Action: "advise", Reason: rule.Name})
		return out
	}

	updated, err := hookio.MergeToolInput(in.ToolInput, map[string]any{"command": newCmd})
	if err != nil {
		return hookio.Empty()
	}
	out.HookSpecificOutput.UpdatedInput = updated
	// Remember the rewritten command so PostToolUse can attribute the REAL
	// output size back to this rule -- the logged bytes here are estimates
	// (the command hasn't run yet), the PostToolUse "measured" record is
	// the ground truth.
	state.notePendingRewrite(in.SessionID, newCmd, rule.Name)
	state.log(logstore.Record{
		TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/Bash",
		Action: "rewrite", Reason: rule.Name,
		BytesBeforeEst: rule.EstBeforeBytes, BytesAfter: rule.EstAfterBytes,
	})
	return out
}

type readInput struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
}

// largeReadBytes is the size past which a full-file Read gets a
// grep-first suggestion -- same threshold the log-tail rewrite uses.
const largeReadBytes = 200 * 1024

// decideReadAdvice targets the two biggest silent token sinks in long
// sessions: re-reading a file that hasn't changed since it was last read,
// and full-reading a large file that a Grep or offset/limit read would
// have answered. Advisory only -- a Read is never blocked or rewritten.
// Gated under mode.preprocess: it's context hygiene, same family as the
// Bash-output rules, and every surface must have an off switch.
func decideReadAdvice(in hookio.Input, cfg config.Config, state *daemonState) hookio.Output {
	if cfg.Mode.Preprocess != "on" {
		return hookio.Empty()
	}
	var ri readInput
	if err := json.Unmarshal(in.ToolInput, &ri); err != nil || ri.FilePath == "" {
		return hookio.Empty()
	}

	fi, err := os.Stat(ri.FilePath)
	if err != nil {
		return hookio.Empty() // can't assess -- stay quiet, the Read itself will surface the error
	}

	var advice []string
	if state.markFileRead(in.SessionID, ri.FilePath, fi.ModTime().UnixNano()) {
		advice = append(advice, "deadeye: "+filepath.Base(ri.FilePath)+" was already read this session and hasn't changed since.")
	}
	// A partial read (offset/limit) of a big file is exactly the right
	// move -- only a full read of one earns the suggestion.
	if fi.Size() > largeReadBytes && ri.Offset == 0 && ri.Limit == 0 {
		advice = append(advice, fmt.Sprintf("deadeye: %s is %dKB -- consider Grep or an offset/limit read instead of the whole file.", filepath.Base(ri.FilePath), fi.Size()/1024))
	}

	if len(advice) == 0 {
		state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/Read", Action: "noop"})
		return hookio.Empty()
	}
	out := hookio.ForEvent("PreToolUse")
	out.HookSpecificOutput.AdditionalContext = strings.Join(advice, " ")
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/Read", Action: "advise", Reason: "read-advice"})
	return out
}

// decidePostToolUse measures instead of deciding: a rewritten Bash
// command's REAL output size (the logged rewrite bytes are per-rule
// estimates made before the command ran), and the response sizes of MCP
// tools (evidence for which ones deserve a rule of their own -- their
// inputs can't be rewritten safely, so observation comes first).
func decidePostToolUse(in hookio.Input, state *daemonState) hookio.Output {
	switch {
	case in.ToolName == "Bash":
		var bi bashInput
		if err := json.Unmarshal(in.ToolInput, &bi); err == nil && bi.Command != "" {
			if rule := state.consumePendingRewrite(in.SessionID, bi.Command); rule != "" {
				state.log(logstore.Record{
					TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PostToolUse",
					Action: "measured", Reason: rule, BytesAfter: len(in.ToolResponse),
				})
				return hookio.Empty()
			}
		}
	case strings.HasPrefix(in.ToolName, "mcp__"):
		state.log(logstore.Record{
			TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PostToolUse",
			Action: "observed", Reason: in.ToolName, BytesAfter: len(in.ToolResponse),
		})
		return hookio.Empty()
	}
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PostToolUse", Action: "noop"})
	return hookio.Empty()
}

// decideSubagentStart injects one line asking the subagent for terse,
// structured results -- subagent output lands in the parent's context
// whole, so prose padding there is paid for twice. UNVERIFIED surface:
// docs/verified.md confirmed SessionStart cannot inject context in Claude
// Code v2.1.220, and SubagentStart hasn't been probed either way. If the
// surface ignores additionalContext this is a harmless no-op; the
// decision log will show inject-subagent firing either way, which is how
// to correlate whether it lands. Gated under mode.preprocess -- context
// hygiene, and every surface must have an off switch.
func decideSubagentStart(in hookio.Input, cfg config.Config, state *daemonState) hookio.Output {
	if cfg.Mode.Preprocess != "on" {
		return hookio.Empty()
	}
	out := hookio.ForEvent("SubagentStart")
	out.HookSpecificOutput.AdditionalContext = "deadeye: return terse, structured results -- your full output lands in the parent agent's context, so every byte of prose padding is paid for twice."
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "SubagentStart", Action: "inject-subagent"})
	return out
}
