package main

import (
	"encoding/json"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/inject"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/preprocess"
	"github.com/deepaksinghcs14/deadeye-cc/internal/proto"
)

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

// decide dispatches by event. A panic anywhere below still yields the
// canonical no-op response (INV-5) -- the recover here is the top of the
// policy call graph; individual handlers don't need their own.
func decide(req proto.Request, state *daemonState) (out hookio.Output) {
	out = hookio.Empty()
	defer func() { recover() }()

	var in hookio.Input
	_ = json.Unmarshal(req.Payload, &in)

	switch req.Event {
	case "UserPromptSubmit":
		out = decideUserPromptSubmit(in, state)
	case "PreToolUse":
		out = decidePreToolUse(in, state)
	default:
		state.log(logstore.Record{
			TS: nowRFC3339(), SessionID: in.SessionID, Surface: req.Event,
			Action: "noop",
		})
	}
	return out
}

// decideUserPromptSubmit delivers the once-per-session advisory injection.
// SessionStart cannot put anything in the model's context in Claude Code
// v2.1.220 (docs/verified.md §5.1); UserPromptSubmit's additionalContext
// is the confirmed-working replacement, gated to fire exactly once per
// session so it stays byte-stable (INV-4).
func decideUserPromptSubmit(in hookio.Input, state *daemonState) hookio.Output {
	if !state.markInjectedIfFirst(in.SessionID) {
		state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "UserPromptSubmit", Action: "noop", Reason: "already injected this session"})
		return hookio.Empty()
	}

	text := inject.Build(state.cat, "")
	tokens := inject.EstimateTokens(text)
	reason := "session guidance injection"
	if tokens > state.cfg.InjectionBudgetTokens {
		reason = "session guidance injection (over INV-4 budget, shipped anyway -- trim before adding more)"
	}
	state.log(logstore.Record{
		TS: nowRFC3339(), SessionID: in.SessionID, Surface: "UserPromptSubmit",
		Action: "inject", Reason: reason, BytesAfter: len(text),
	})

	out := hookio.ForEvent("UserPromptSubmit")
	out.HookSpecificOutput.AdditionalContext = text
	return out
}

type bashInput struct {
	Command string `json:"command"`
}

// decidePreToolUse runs Bash preprocessing rules (PLAN.md §5.3). Edit/Write
// gets the plan-gate hard layer in Phase 4.
func decidePreToolUse(in hookio.Input, state *daemonState) hookio.Output {
	if in.ToolName != "Bash" {
		state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/" + in.ToolName, Action: "noop"})
		return hookio.Empty()
	}
	return decideBashPreprocess(in, state)
}

func decideBashPreprocess(in hookio.Input, state *daemonState) hookio.Output {
	if state.cfg.Mode.Preprocess != "on" || config.KillSwitchOff("DEADEYE_PREPROCESS") {
		return hookio.Empty()
	}

	var bi bashInput
	if err := json.Unmarshal(in.ToolInput, &bi); err != nil || bi.Command == "" {
		return hookio.Empty()
	}

	rule, newCmd, applied := preprocess.Apply(bi.Command, state.cfg.DisabledRuleSet())
	if !applied {
		return hookio.Empty()
	}

	out := hookio.ForEvent("PreToolUse")
	if rule.Advisory {
		out.HookSpecificOutput.AdditionalContext = "deadeye: " + rule.Note
		state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/Bash", Action: "advise", Reason: rule.Name})
		return out
	}

	updated, err := json.Marshal(map[string]string{"command": newCmd})
	if err != nil {
		return hookio.Empty()
	}
	out.HookSpecificOutput.UpdatedInput = updated
	state.log(logstore.Record{
		TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/Bash",
		Action: "rewrite", Reason: rule.Name,
		BytesBeforeEst: rule.EstBeforeBytes, BytesAfter: rule.EstAfterBytes,
	})
	return out
}
