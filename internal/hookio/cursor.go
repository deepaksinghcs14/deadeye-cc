package hookio

import (
	"encoding/json"
	"strings"
)

// Cursor's hook contract (cursor.com/docs/hooks, read 2026-10-11; not yet
// confirmed against a live Cursor) differs from Claude Code's on both
// sides, so unlike Gemini and Codex it needs its INPUT translated too:
// different event names, different field names, flat snake_case output.
//
// Only what Cursor documents is mapped. Its Read tool's preToolUse input
// shape is not documented, so reads come through beforeReadFile -- whose
// file_path is -- and are presented to the deciders as a Read tool call.

// cursorEvents maps a Cursor hook event to the canonical event deadeye's
// deciders switch on. An event absent here is one deadeye has no use for on
// Cursor, usually because the response it would produce has no channel:
// beforeSubmitPrompt and subagentStart cannot inject context, and stop's
// only output field is auto-submitted as the user's next message.
var cursorEvents = map[string]string{
	"sessionStart":   "SessionStart",
	"sessionEnd":     "SessionEnd",
	"preToolUse":     "PreToolUse",
	"beforeReadFile": "PreToolUse",
	"postToolUse":    "PostToolUse",
}

// cursorTools maps Cursor tool names to the names the deciders know.
var cursorTools = map[string]string{"Shell": "Bash", "Task": "Agent"}

// CursorToCanonical translates one Cursor hook request into the event name
// and payload shape the deciders expect. ok is false for an event deadeye
// does not handle on Cursor, or a payload that isn't JSON.
func CursorToCanonical(event string, raw []byte) (canonicalEvent string, payload []byte, ok bool) {
	canonicalEvent, ok = cursorEvents[event]
	if !ok {
		return "", nil, false
	}
	var in struct {
		ConversationID string          `json:"conversation_id"`
		SessionID      string          `json:"session_id"`
		Cwd            string          `json:"cwd"`
		WorkspaceRoots []string        `json:"workspace_roots"`
		TranscriptPath string          `json:"transcript_path"`
		ToolName       string          `json:"tool_name"`
		ToolInput      json.RawMessage `json:"tool_input"`
		ToolOutput     string          `json:"tool_output"`
		ToolUseID      string          `json:"tool_use_id"`
		FilePath       string          `json:"file_path"` // beforeReadFile
	}
	if json.Unmarshal(raw, &in) != nil {
		return "", nil, false
	}
	out := Input{
		// conversation_id is the id every Cursor hook carries; session_id
		// appears only on the two session events.
		SessionID:      firstNonEmpty(in.ConversationID, in.SessionID),
		TranscriptPath: in.TranscriptPath,
		Cwd:            in.Cwd,
		HookEventName:  canonicalEvent,
		ToolInput:      in.ToolInput,
		ToolUseID:      in.ToolUseID,
	}
	if out.Cwd == "" && len(in.WorkspaceRoots) > 0 {
		out.Cwd = in.WorkspaceRoots[0]
	}
	switch {
	case event == "beforeReadFile":
		out.ToolName = "Read"
		out.ToolInput, _ = json.Marshal(map[string]string{"file_path": in.FilePath})
	case strings.HasPrefix(in.ToolName, "MCP:"):
		out.ToolName = "mcp__" + strings.TrimPrefix(in.ToolName, "MCP:")
	case cursorTools[in.ToolName] != "":
		out.ToolName = cursorTools[in.ToolName]
	default:
		out.ToolName = in.ToolName
	}
	if in.ToolOutput != "" {
		// Cursor sends tool_output as a JSON-encoded string; the deciders
		// only ever measure tool_response's size, so it travels as one.
		out.ToolResponse, _ = json.Marshal(in.ToolOutput)
	}
	payload, err := json.Marshal(out)
	return canonicalEvent, payload, err == nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type cursorOutput struct {
	Permission        string          `json:"permission,omitempty"` // allow | deny
	UserMessage       string          `json:"user_message,omitempty"`
	AgentMessage      string          `json:"agent_message,omitempty"`
	UpdatedInput      json.RawMessage `json:"updated_input,omitempty"`
	AdditionalContext string          `json:"additional_context,omitempty"`
}

// MarshalCursor renders out as the response to one Cursor hook event (the
// Cursor event name, not the canonical one -- beforeReadFile and preToolUse
// accept different fields).
//
// The two permission hooks always answer with an explicit permission:
// Cursor blocks the action on a response that doesn't match the schema, so
// "nothing to say" is spelled {"permission":"allow"}, never {}.
//
// An "ask" has no working equivalent on these hooks (Cursor accepts it on
// preToolUse but does not enforce it), so it collapses the way it does for
// Gemini: AskFallbackDeny becomes a deny, anything else passes. Advice
// attached to a pre-tool decision is dropped -- Cursor only delivers
// agent_message on a deny.
func MarshalCursor(event string, out Output) []byte {
	hs := out.HookSpecificOutput
	if hs == nil {
		hs = &HookSpecificOutput{}
	}
	var c cursorOutput
	switch event {
	case "preToolUse", "beforeReadFile":
		c.Permission = PermissionAllow
		if hs.PermissionDecision == PermissionDeny || (hs.PermissionDecision == PermissionAsk && hs.AskFallback == AskFallbackDeny) {
			c.Permission = PermissionDeny
			c.UserMessage = hs.PermissionDecisionReason
			if event == "preToolUse" {
				c.AgentMessage = hs.PermissionDecisionReason
			}
		} else if event == "preToolUse" {
			c.UpdatedInput = hs.UpdatedInput
		}
	case "sessionStart", "postToolUse":
		c.AdditionalContext = hs.AdditionalContext
	}
	b, err := json.Marshal(c)
	if err != nil {
		return []byte("{}")
	}
	return b
}
