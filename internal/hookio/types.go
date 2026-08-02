// Package hookio defines the JSON contract Claude Code speaks to hook
// commands on stdin/stdout. Confirmed live against Claude Code v2.1.220's
// own hook-output validator (its error path echoes the exact accepted
// schema) -- see docs/verified.md V3/V6 for how. Do not add fields back
// from memory/docs without the same live check: the docs-derived first
// draft of this file had two real bugs the validator caught immediately
// (hookEventName wrongly marked optional; SessionStart wrongly assumed to
// accept hookSpecificOutput at all).
package hookio

import "encoding/json"

// Effort mirrors the effort object Claude Code includes in hook input.
type Effort struct {
	Level string `json:"level"`
}

// Input is the stdin payload for every hook event. Not every field is
// populated for every event; e.g. ToolName/ToolInput only appear for
// PreToolUse/PostToolUse, AgentID/AgentType only inside subagents.
type Input struct {
	SessionID      string          `json:"session_id"`
	PromptID       string          `json:"prompt_id,omitempty"`
	TranscriptPath string          `json:"transcript_path,omitempty"`
	Cwd            string          `json:"cwd,omitempty"`
	PermissionMode string          `json:"permission_mode,omitempty"`
	Effort         *Effort         `json:"effort,omitempty"`
	HookEventName  string          `json:"hook_event_name"`
	ToolName       string          `json:"tool_name,omitempty"`
	ToolInput      json.RawMessage `json:"tool_input,omitempty"`
	ToolUseID      string          `json:"tool_use_id,omitempty"`
	AgentID        string          `json:"agent_id,omitempty"`
	AgentType      string          `json:"agent_type,omitempty"`
}

// PermissionDecision values. The top-level Output.PermissionDecision
// accepts only allow|deny|ask; hookSpecificOutput's PreToolUse variant
// additionally accepts "defer" -- both confirmed against the live
// validator's error dump.
const (
	PermissionAllow = "allow"
	PermissionDeny  = "deny"
	PermissionAsk   = "ask"
	PermissionDefer = "defer" // PreToolUse hookSpecificOutput only
)

// HookSpecificOutput is the per-event response shape nested under
// hookSpecificOutput. HookEventName is REQUIRED whenever this struct is
// present -- Claude Code rejects the entire response otherwise. Only
// PreToolUse, UserPromptSubmit, PostToolUse, PostToolBatch, and
// Stop/SubagentStop accept hookSpecificOutput at all; SessionStart does
// NOT (use Output.SystemMessage for that event instead -- see ForEvent
// and docs/verified.md V6 for how this was confirmed live).
type HookSpecificOutput struct {
	HookEventName            string          `json:"hookEventName"`
	PermissionDecision       string          `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string          `json:"permissionDecisionReason,omitempty"`
	UpdatedInput             json.RawMessage `json:"updatedInput,omitempty"`
	AdditionalContext        string          `json:"additionalContext,omitempty"`
}

// Output is the full stdout payload. The zero value marshals to exactly
// "{}" -- the canonical no-op response required by INV-5 (fail open) and
// by every event Phase 0 doesn't act on yet.
type Output struct {
	Continue           *bool               `json:"continue,omitempty"`
	SuppressOutput     *bool               `json:"suppressOutput,omitempty"`
	StopReason         string              `json:"stopReason,omitempty"`
	Decision           string              `json:"decision,omitempty"` // "approve" | "block"
	Reason             string              `json:"reason,omitempty"`
	SystemMessage      string              `json:"systemMessage,omitempty"`
	TerminalSequence   string              `json:"terminalSequence,omitempty"`
	PermissionDecision string              `json:"permissionDecision,omitempty"` // allow|deny|ask (top-level; no defer)
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// Empty is the canonical no-op hook response.
func Empty() Output { return Output{} }

// ForEvent returns an Output with hookSpecificOutput pre-populated and
// HookEventName filled in for the given event, so that required field can
// never be forgotten by a caller building a PreToolUse/UserPromptSubmit/
// PostToolUse/PostToolBatch/Stop/SubagentStop response. Do not use this
// for SessionStart -- see Output.SystemMessage instead.
func ForEvent(event string) Output {
	return Output{HookSpecificOutput: &HookSpecificOutput{HookEventName: event}}
}
