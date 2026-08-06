package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/codemap"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/inject"
	"github.com/deepaksinghcs14/deadeye-cc/internal/kernel"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
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
	defer func() {
		if rec := recover(); rec != nil {
			// Fail open (INV-5) but leave a trace -- a silent swallow makes
			// "deadeye stopped advising" undiagnosable. Both trails are
			// best-effort: the decision log surfaces it in /deadeye-audit,
			// panics.log survives even if logging itself was the panic.
			logPanic("daemon/"+req.Event, rec)
			state.log(logstore.Record{TS: nowRFC3339(), Surface: req.Event, Action: "panic", Reason: truncatedMarker(fmt.Sprint(rec))})
		}
	}()

	var in hookio.Input
	_ = json.Unmarshal(req.Payload, &in)
	cfg := config.LoadFor(in.Cwd, req.Off)

	switch req.Event {
	case "SessionStart":
		out = decideCoderSessionStart(in, cfg, req.PluginRoot, req.ConfigDir, req.Host, state)
	case "PostCompact":
		// Codex has no SessionStart source=compact -- PostCompact is its
		// compaction-survival surface. Same handler, same semantics.
		in.Source = "compact"
		out = decideCoderSessionStart(in, cfg, req.PluginRoot, req.ConfigDir, req.Host, state)
	case "UserPromptSubmit":
		out = decideUserPromptSubmit(in, cfg, req.ClientVersion, req.Host, state)
	case "PreToolUse":
		out = decidePreToolUse(in, cfg, state)
	case "PostToolUse":
		out = decidePostToolUse(in, cfg, state)
	case "SubagentStart":
		out = decideSubagentStart(in, cfg, state)
	case "Stop":
		out = decideStop(in, cfg, state)
	case "SessionEnd":
		out = decideSessionEnd(in, cfg, state)
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
// SessionStart CAN inject via raw stdout (docs/verified.md §11, which
// superseded §5.1's "impossible" conclusion) -- the coder persona uses
// exactly that. THIS injection stays on UserPromptSubmit anyway: it fires
// exactly once per session for INV-4 byte-stability, which SessionStart's
// startup/resume/clear/compact multiplicity can't offer.
func decideUserPromptSubmit(in hookio.Input, cfg config.Config, clientVersion, host string, state *daemonState) hookio.Output {
	var parts []string

	// Coder-mode tracker first: a /deadeye-coder command's confirmation
	// leads the combined context. Synthetic prompts can't be commands, but
	// the house guard applies everywhere prompts are parsed.
	if !isSyntheticPrompt(in.Prompt) {
		// A real user prompt legitimately restarts orientation -- without
		// this, a few reads per turn across several turns would read as one
		// long streak and false-fire delegate-explore.
		state.resetExploreStreak(in.SessionID)
		if confirm := muteTracker(in, state); confirm != "" {
			parts = append(parts, confirm)
		}
		if confirm := coderTracker(in, cfg, state); confirm != "" {
			parts = append(parts, confirm)
		}
	}

	if state.markInjectedIfFirst(in.SessionID) {
		// Native-restore guard (PLAN.md §5.7/§10.10): a resumed or
		// compacted session already carries its own context, so the memory
		// paragraph -- and the codebase map, whose content the replayed
		// transcript's own exploration already covers -- would be a
		// redundant repeat of what Claude Code itself just restored. The
		// rest of the injection still applies.
		memory := ""
		mapText := ""
		if !state.nativeRestoreFor(in.SessionID) {
			memory = sessionmem.LoadRecent(in.Cwd)
			if cfg.Mode.Codemap == "on" && in.Cwd != "" {
				mapText = codemap.Text(in.Cwd)
			}
		}
		text := inject.Build(state.cat, memory, cfg.Mode.Effort != "off", host)
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
		if mapText != "" {
			// Composed here, never inside inject.Build: codemap is
			// independently switched (mode.codemap) and separately logged,
			// so /deadeye-audit can attribute its real byte cost.
			state.log(logstore.Record{
				TS: nowRFC3339(), SessionID: in.SessionID, Surface: "UserPromptSubmit",
				Action: "inject-codemap", Reason: "codebase map", BytesAfter: len(mapText),
			})
			parts = append(parts, mapText)
		}
	}

	if !state.isMuted(in.SessionID) {
		if suggestion, fired := decidePlanGateSoft(in, cfg, state); fired {
			parts = append(parts, suggestion)
		}
		if suggestion, fired := decideWorkflowHint(in, cfg, clientVersion, state); fired && host != "codex" {
			parts = append(parts, suggestion)
		}
		if suggestion, fired := decideLargePaste(in, cfg, state); fired {
			parts = append(parts, suggestion)
		}
	}

	if len(parts) == 0 {
		return hookio.Empty()
	}

	out := hookio.ForEvent("UserPromptSubmit")
	out.HookSpecificOutput.AdditionalContext = strings.Join(parts, "\n\n")
	return out
}

// largePasteBytes is where a prompt stops being typed text and starts
// being a paste: nobody types 20KB, and 20KB (~5k tokens) stays resident
// in context for the whole session.
const largePasteBytes = 20 << 10

// decideLargePaste nudges once per session when a (real, non-synthetic)
// prompt arrives large enough to be a pasted log/dump -- a file plus
// targeted Grep/Read is cheaper than 20KB+ resident for the session.
// Synthetic prompts (task notifications carry big XML payloads) never
// fire this. The caller holds the mute gate.
func decideLargePaste(in hookio.Input, cfg config.Config, state *daemonState) (string, bool) {
	if cfg.Mode.Preprocess != "on" || cfg.DisabledRuleSet()["large-paste"] ||
		len(in.Prompt) <= largePasteBytes || isSyntheticPrompt(in.Prompt) {
		return "", false
	}
	if !state.markSuggestedIfFirst(in.SessionID, "large-paste") {
		return "", false
	}
	state.log(logstore.Record{
		TS: nowRFC3339(), SessionID: in.SessionID, Surface: "UserPromptSubmit",
		Action: "advise", Reason: "large-paste", BytesAfter: len(in.Prompt),
	})
	return fmt.Sprintf(
		"deadeye: this prompt is ~%d KB -- a paste this size stays in context for the whole session; drop it in a file instead and let me Grep/Read the parts that matter.",
		len(in.Prompt)>>10,
	), true
}

// compactAdviseBytes is where accumulated tool-response arrivals make a
// chosen /compact worth suggesting: ~75k tokens at ~4 bytes/token -- a
// real fraction of the window, well before auto-compact forces the issue
// mid-task.
const compactAdviseBytes = 300 << 10

// decideStop shows a single terse line when new preprocessing savings have
// accrued since the last turn -- subtle by design: one line, only on
// change, phrased like the rest of Claude Code's own hook feedback rather
// than a banner. Stop fires once per turn, not once per session, so this
// naturally updates as the session progresses without repeating a stale
// total when nothing new happened. Stop is also a natural task boundary,
// which makes it the right moment for the compact-timing advisory: a
// compaction chosen here beats an automatic one landing mid-task.
func decideStop(in hookio.Input, cfg config.Config, state *daemonState) hookio.Output {
	var parts []string

	// The savings line keeps its original behavior: not mode- or
	// mute-gated (it reports work already done, not a nag).
	if bytesSaved, rewrites, changed := state.newSavingsToShow(in.SessionID); changed {
		parts = append(parts, fmt.Sprintf(
			"deadeye: ~%d bytes kept out of context this session (%d rewrite%s).",
			bytesSaved, rewrites, plural(rewrites),
		))
		state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "Stop", Action: "savings-shown", BytesAfter: bytesSaved})
	}

	if cfg.Mode.Preprocess == "on" && !cfg.DisabledRuleSet()["compact-timing"] && !state.isMuted(in.SessionID) {
		if arrived, fire := state.shouldAdviseCompact(in.SessionID, compactAdviseBytes); fire {
			parts = append(parts, fmt.Sprintf(
				"deadeye: ~%d KB of tool output has entered context this session -- a /compact now, at a task boundary, beats an auto-compact landing mid-task.",
				arrived>>10,
			))
			state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "Stop", Action: "advise", Reason: "compact-timing", BytesAfter: arrived})
		}
	}

	if len(parts) == 0 {
		return hookio.Empty()
	}
	out := hookio.ForEvent("Stop")
	out.HookSpecificOutput.AdditionalContext = strings.Join(parts, "\n\n")
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// decideSessionEnd writes Phase 1.5's session-memory summary, then evicts
// the session's in-memory state.
func decideSessionEnd(in hookio.Input, cfg config.Config, state *daemonState) hookio.Output {
	count := state.decisionCount(in.SessionID)
	_ = sessionmem.Write(in.Cwd, in.SessionID, count)
	// Codemap's writes are gated on mode.codemap too, not just its
	// injection -- "off" must mean nothing is written, or the switch is a
	// documented setting that silently half-works (the exact antipattern
	// this codebase deleted posture and radius_trigger for).
	if cfg.Mode.Codemap == "on" && in.Cwd != "" {
		_ = codemap.MergeTouched(in.Cwd, state.readFilesSnapshot(in.SessionID), time.Now().Unix())
		_ = codemap.PruneNotes(in.Cwd)
		_ = codemap.Regenerate(in.Cwd)
	}
	os.Remove(meta.CoderModePathFor(in.SessionID)) // this session's statusline badge file
	// Session state is daemon-lifetime advisory dedup only, never
	// persisted -- but nothing ever removed an entry, so across a
	// long-lived daemon (idle timeout resets on every connection, so in
	// practice a daily user's daemon runs for as long as the machine is
	// up) every session id the machine has ever seen accumulated here
	// forever. Evict last, after the writes above -- codemap's snapshot
	// and sessionmem's decisionCount both need the session's state alive.
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
		return withDelegateExplore(in, cfg, state, decideReadAdvice(in, cfg, state))
	case "Grep":
		return withDelegateExplore(in, cfg, state, decideGrepAdvice(in, cfg, state))
	case "WebFetch":
		return decideWebFetchAdvice(in, cfg, state)
	case "Edit", "Write", "apply_patch":
		// An edit invalidates the consecutive-repeat heuristic: re-running
		// the same command AFTER a change is legitimate verification. It
		// ends an orientation phase too -- belt and braces beside the
		// PostToolUse reset, for hosts where PostToolUse Edit never arrives.
		state.clearLastBash(in.SessionID)
		state.resetExploreStreak(in.SessionID)
		// Both can fire on the same edit: the gate asks permission
		// (PermissionDecision), the vuln advisory adds context
		// (AdditionalContext) -- different fields of the same
		// hookSpecificOutput, so both survive in one response rather than
		// one silently dropping the other. Always run the gate first: it
		// has its own state to clear (clearPendingPlan) regardless of
		// whether this edit trips a vuln rule.
		out := decidePlanGateHard(in, cfg, state)
		if vuln := decideVulnAdvice(in, cfg, state); vuln.HookSpecificOutput != nil {
			if out.HookSpecificOutput == nil {
				return vuln
			}
			out.HookSpecificOutput.AdditionalContext = vuln.HookSpecificOutput.AdditionalContext
		}
		return out
	default:
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
	// Bounded like every other git call site (route.go's gitTimeout): the
	// churn provider shells to git, and a stalled network mount or held
	// .git/index.lock must fail open into "unknown" evidence, not hang the
	// hook (INV-5).
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	evidence := signals.AssessAll(ctx, scope, signals.Builtins())
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
	// The retry key catches the same loop with shifting flags.
	retryKey, _ := normalizeBashRetryKey(bi.Command)
	repeat, retryCount := state.noteBashCommand(in.SessionID, bi.Command, retryKey)

	disabled := cfg.DisabledRuleSet()

	// bash-retry: the flag-escalation variant of repeat-command -- same
	// target re-run with only option changes and no edits in between.
	// Suppressed on a raw-identical repeat (that's repeat-command's line,
	// never both in one turn), and advised once per key per session. This
	// must attach on EVERY return path below including rewrites: the
	// canonical loops (go test, pytest) are exactly the commands the
	// rewrite rules match.
	retryAdvice := ""
	if retryCount >= bashRetryStreak && !repeat &&
		!disabled["bash-retry"] && !state.isMuted(in.SessionID) &&
		state.markSuggestedIfFirst(in.SessionID, "bash-retry:"+retryKey) {
		retryAdvice = "deadeye: third run of the same target with only flag changes and no edits in between -- the output won't change until the code does; read the first failure instead."
		state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/Bash", Action: "advise", Reason: "bash-retry"})
	}

	rule, newCmd, applied := preprocess.Apply(in.Cwd, bi.Command, disabled)
	if !applied {
		if repeat && !disabled["repeat-command"] && !state.isMuted(in.SessionID) {
			out := hookio.ForEvent("PreToolUse")
			out.HookSpecificOutput.AdditionalContext = "deadeye: identical to the previous command, with no file changes in between -- the output is unlikely to differ."
			state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/Bash", Action: "advise", Reason: "repeat-command"})
			return out
		}
		if retryAdvice != "" {
			out := hookio.ForEvent("PreToolUse")
			out.HookSpecificOutput.AdditionalContext = retryAdvice
			return out
		}
		return hookio.Empty()
	}

	out := hookio.ForEvent("PreToolUse")
	if rule.Advisory {
		if state.isMuted(in.SessionID) {
			return hookio.Empty()
		}
		out.HookSpecificOutput.AdditionalContext = "deadeye: " + rule.Note
		if retryAdvice != "" {
			out.HookSpecificOutput.AdditionalContext += "\n" + retryAdvice
		}
		state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/Bash", Action: "advise", Reason: rule.Name})
		return out
	}
	if retryAdvice != "" {
		// Rides the rewrite response: AdditionalContext and UpdatedInput are
		// different fields of the same hookSpecificOutput, so both survive.
		out.HookSpecificOutput.AdditionalContext = retryAdvice
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
// The visible advisory is gated under mode.preprocess (context hygiene,
// same family as the Bash-output rules); the silent markFileRead TRACKING
// also feeds codemap's touch-frequency counter, so it runs whenever EITHER
// surface wants it -- turning off command-output hygiene must not silently
// starve a codebase map someone left on, and vice versa.
func decideReadAdvice(in hookio.Input, cfg config.Config, state *daemonState) hookio.Output {
	if cfg.Mode.Preprocess != "on" && cfg.Mode.Codemap != "on" {
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
	unchangedRepeat := state.markFileRead(in.SessionID, ri.FilePath, fi.ModTime().UnixNano())

	// The VISIBLE advisory keeps its original gate exactly -- only the
	// tracking above became independent of mode.preprocess.
	if cfg.Mode.Preprocess != "on" || cfg.DisabledRuleSet()["read-advice"] || state.isMuted(in.SessionID) {
		return hookio.Empty()
	}

	var advice []string
	if unchangedRepeat {
		advice = append(advice, "deadeye: "+filepath.Base(ri.FilePath)+" was already read this session and hasn't changed since.")
	}
	// A partial read (offset/limit) of a big file is exactly the right
	// move -- only a full read of one earns the suggestion.
	if fi.Size() > largeReadBytes && ri.Offset == 0 && ri.Limit == 0 {
		advice = append(advice, fmt.Sprintf("deadeye: %s is %dKB -- consider Grep or an offset/limit read instead of the whole file.", filepath.Base(ri.FilePath), fi.Size()/1024))
	}

	if len(advice) == 0 {
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
type grepInput struct {
	OutputMode string `json:"output_mode"`
	HeadLimit  int    `json:"head_limit"`
}

// decideGrepAdvice flags the one Grep shape with unbounded output --
// content mode with no head_limit dumps every match across the search
// scope. Advisory only, once per session (a reminder, not a nag);
// files_with_matches/count modes are already bounded.
func decideGrepAdvice(in hookio.Input, cfg config.Config, state *daemonState) hookio.Output {
	if cfg.Mode.Preprocess != "on" || cfg.DisabledRuleSet()["grep-limit"] || state.isMuted(in.SessionID) {
		return hookio.Empty()
	}
	var gi grepInput
	if err := json.Unmarshal(in.ToolInput, &gi); err != nil || gi.OutputMode != "content" || gi.HeadLimit > 0 {
		return hookio.Empty()
	}
	if !state.markSuggestedIfFirst(in.SessionID, "grep-limit") {
		return hookio.Empty()
	}
	out := hookio.ForEvent("PreToolUse")
	out.HookSpecificOutput.AdditionalContext = "deadeye: content-mode Grep with no head_limit can dump every match in scope -- consider head_limit, or files_with_matches first."
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/Grep", Action: "advise", Reason: "grep-limit"})
	return out
}

// exploreStreakThreshold is where consecutive exploration turns from
// targeted orientation (3-8 reads before an edit is normal) into survey
// work an Explore subagent should absorb -- its reads land in a
// disposable context, the parent pays only for the summary.
const exploreStreakThreshold = 12

// withDelegateExplore appends the delegate-explore advisory to a Read/Grep
// PreToolUse response when the session's exploration streak (counted at
// PostToolUse, reset by edits/runs/prompts) says this is survey work.
// Space-joined onto whatever the per-tool advice already said, same shape
// as decideReadAdvice's own two-advisory join.
func withDelegateExplore(in hookio.Input, cfg config.Config, state *daemonState, out hookio.Output) hookio.Output {
	if cfg.Mode.Preprocess != "on" || cfg.DisabledRuleSet()["delegate-explore"] || state.isMuted(in.SessionID) {
		return out
	}
	if state.exploreStreakFor(in.SessionID) < exploreStreakThreshold {
		return out
	}
	if !state.markSuggestedIfFirst(in.SessionID, "delegate-explore") {
		return out
	}
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/" + in.ToolName, Action: "advise", Reason: "delegate-explore"})
	const advice = "deadeye: 12+ exploration calls in a row with no edits -- these reads all stay in this context; an Explore subagent pays for orientation in its own disposable window and returns only a summary."
	if out.HookSpecificOutput == nil {
		out = hookio.ForEvent("PreToolUse")
		out.HookSpecificOutput.AdditionalContext = advice
		return out
	}
	out.HookSpecificOutput.AdditionalContext += " " + advice
	return out
}

type webFetchInput struct {
	URL string `json:"url"`
}

// normalizeFetchURL folds the URL variants that name the SAME resource:
// a #fragment (client-side only, identical response) and one trailing
// slash. Nothing else -- a different query string (?page=2) is a genuinely
// different fetch, so exact match otherwise.
func normalizeFetchURL(url string) string {
	if i := strings.IndexByte(url, '#'); i >= 0 {
		url = url[:i]
	}
	return strings.TrimSuffix(url, "/")
}

// decideWebFetchAdvice flags a re-fetch of a URL already fetched this
// session -- the first response is still in context, so the repeat buys
// nothing unless the page itself changed. Advised once per URL (a third
// fetch isn't re-nagged; a different repeated URL still is).
func decideWebFetchAdvice(in hookio.Input, cfg config.Config, state *daemonState) hookio.Output {
	if cfg.Mode.Preprocess != "on" {
		return hookio.Empty()
	}
	var wi webFetchInput
	if err := json.Unmarshal(in.ToolInput, &wi); err != nil || wi.URL == "" {
		return hookio.Empty() // malformed input fails open
	}
	url := normalizeFetchURL(wi.URL)
	repeat := state.noteWebFetch(in.SessionID, url) // track even when the advisory below is off
	if !repeat || cfg.DisabledRuleSet()["repeat-webfetch"] || state.isMuted(in.SessionID) {
		return hookio.Empty()
	}
	if !state.markSuggestedIfFirst(in.SessionID, "webfetch:"+url) {
		return hookio.Empty()
	}
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/WebFetch", Action: "advise", Reason: "repeat-webfetch"})
	out := hookio.ForEvent("PreToolUse")
	out.HookSpecificOutput.AdditionalContext = "deadeye: this URL was already fetched this session -- the first response is still in context; re-fetch only if you expect the page to have changed."
	return out
}

func decidePostToolUse(in hookio.Input, cfg config.Config, state *daemonState) hookio.Output {
	// The whole surface is context hygiene, so the preprocess switch
	// governs it -- "every axis independently switchable" includes the
	// observation logging, not just the surfaces that touch context.
	if cfg.Mode.Preprocess != "on" {
		return hookio.Empty()
	}
	// Exploration-streak accounting for delegate-explore: Read/Grep/Glob
	// completions extend the streak, any other completed tool ends the
	// orientation phase.
	switch in.ToolName {
	case "Read", "Grep", "Glob":
		state.noteExploreTool(in.SessionID)
	default:
		state.resetExploreStreak(in.SessionID)
	}
	// Context-arrival accounting for compact-timing: EVERY tool response,
	// explicitly -- only >8KB outliers are ever logged, so deriving this
	// from log rows would undercount badly. Raw-JSON length slightly
	// overstates the visible text, which is why the advisory says "~".
	state.noteArrivalBytes(in.SessionID, len(in.ToolResponse))
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
		return decideMCPOversize(in, cfg, state)
	case in.ToolName == "Read" || in.ToolName == "Grep" || in.ToolName == "Glob" ||
		in.ToolName == "WebFetch" || in.ToolName == "WebSearch":
		// Observation only, never context: this is the evidence base future
		// rules get justified from ("measured, not estimated"). The
		// threshold keeps small responses out of the log -- without it this
		// would re-create the noop bloat removed in 0.7.0.
		if len(in.ToolResponse) > observeThresholdBytes {
			state.log(logstore.Record{
				TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PostToolUse",
				Action: "observed", Reason: in.ToolName, BytesAfter: len(in.ToolResponse),
			})
		}
		return hookio.Empty()
	}
	return hookio.Empty()
}

// observeThresholdBytes gates size observation of Read/Grep/Glob/WebFetch/
// WebSearch responses: only outliers are evidence worth keeping.
const observeThresholdBytes = 8 << 10

// mcpOversizeBytes is where a single MCP response goes from "observed" to
// "advised on": 4x the observe threshold, ~8k tokens landing in context
// from one call -- outsized by this project's own evidence bar, and almost
// always narrowable at the query (filters, pagination, fewer fields).
const mcpOversizeBytes = 32 << 10

// decideMCPOversize advises once per MCP tool per session when a single
// response exceeds mcpOversizeBytes -- post-hoc by nature (the bytes are
// already in context), so the advice targets the NEXT call. PostToolUse
// additionalContext is schema-legal (hookio types doc) but this is its
// first use here; if a host ignores it, the advise log row still records
// the firing -- same posture as decideSubagentStart's unverified surface.
func decideMCPOversize(in hookio.Input, cfg config.Config, state *daemonState) hookio.Output {
	if len(in.ToolResponse) <= mcpOversizeBytes ||
		cfg.DisabledRuleSet()["mcp-oversize"] || state.isMuted(in.SessionID) {
		return hookio.Empty()
	}
	if !state.markSuggestedIfFirst(in.SessionID, "mcp-size:"+in.ToolName) {
		return hookio.Empty()
	}
	state.log(logstore.Record{
		TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PostToolUse",
		Action: "advise", Reason: "mcp-oversize", BytesAfter: len(in.ToolResponse),
	})
	out := hookio.ForEvent("PostToolUse")
	out.HookSpecificOutput.AdditionalContext = fmt.Sprintf(
		"deadeye: %s returned ~%d KB in one response -- narrow the next call (filters, pagination, fewer fields); all of this stays in context.",
		in.ToolName, len(in.ToolResponse)>>10,
	)
	return out
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
	const brevityNote = "deadeye: return terse, structured results -- your full output lands in the parent agent's context, so every byte of prose padding is paid for twice."
	var parts []string
	if cfg.Mode.Preprocess == "on" {
		parts = append(parts, brevityNote)
		// BytesAfter recorded so /deadeye-context can attribute this
		// injection's real cost like every other inject-* row.
		state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "SubagentStart", Action: "inject-subagent", BytesAfter: len(brevityNote)})
	}
	// Coder persona travels into subagents too -- SessionStart context is
	// parent-thread only and never reaches them (verified in Phase 0:
	// SubagentStart additionalContext does).
	if text := coderSubagentText(in, cfg, state); text != "" {
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		return hookio.Empty()
	}
	out := hookio.ForEvent("SubagentStart")
	out.HookSpecificOutput.AdditionalContext = strings.Join(parts, "\n\n")
	return out
}
