package hookio

import "encoding/json"

// Gemini CLI's hook response dialect differs from Claude Code's in three
// fields (verified against gemini-cli docs/hooks/reference.md, live
// confirmation pending -- experimental): a BeforeTool rewrite is
// hookSpecificOutput.tool_input (not updatedInput); a block is top-level
// decision:"deny" + reason (Gemini has NO ask/allow, only deny or pass);
// additionalContext is the same. This is the translation layer the daemon
// applies when a request's host is Gemini.
type geminiHookSpecific struct {
	ToolInput         json.RawMessage `json:"tool_input,omitempty"`
	AdditionalContext string          `json:"additionalContext,omitempty"`
}

type geminiOutput struct {
	Decision           string              `json:"decision,omitempty"` // "deny" only
	Reason             string              `json:"reason,omitempty"`
	HookSpecificOutput *geminiHookSpecific `json:"hookSpecificOutput,omitempty"`
}

// MarshalGemini renders out in Gemini's dialect. The permission ladder
// collapses per each ask's AskFallback: "ask"+deny -> a hard deny+reason
// (the exfil guard); "ask"+advise -> the reason as additionalContext, no
// block (the plan gate, vuln-on-add). A bare "deny" passes through as a
// deny. "allow"/"defer" have no Gemini equivalent and become a pass (the
// zero value "{}") -- deadeye only emits those for Claude-only surfaces
// (routing enforce), which a reduced host never reaches.
func MarshalGemini(out Output) []byte {
	hs := out.HookSpecificOutput
	if hs == nil {
		return []byte("{}")
	}
	g := geminiOutput{}
	inner := &geminiHookSpecific{
		ToolInput:         hs.UpdatedInput,
		AdditionalContext: hs.AdditionalContext,
	}

	switch hs.PermissionDecision {
	case PermissionDeny:
		g.Decision = PermissionDeny
		g.Reason = hs.PermissionDecisionReason
	case PermissionAsk:
		if hs.AskFallback == AskFallbackDeny {
			g.Decision = PermissionDeny
			g.Reason = hs.PermissionDecisionReason
		} else {
			// Downgrade to a nudge: surface the reason as context.
			if inner.AdditionalContext == "" {
				inner.AdditionalContext = hs.PermissionDecisionReason
			} else {
				inner.AdditionalContext += " " + hs.PermissionDecisionReason
			}
		}
	}

	if inner.ToolInput != nil || inner.AdditionalContext != "" {
		g.HookSpecificOutput = inner
	}
	b, err := json.Marshal(g)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// MarshalFor renders out in the given host's dialect: Gemini gets the
// translated shape above, every other host (Claude, Codex) gets the
// canonical Output JSON.
func MarshalFor(host string, out Output) []byte {
	if host == "gemini" {
		return MarshalGemini(out)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return []byte("{}")
	}
	return b
}
