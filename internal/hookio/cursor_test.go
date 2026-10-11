package hookio

import (
	"encoding/json"
	"testing"
)

// Payloads below are the examples from cursor.com/docs/hooks.

func TestCursorToCanonicalMapsADocumentedShellCall(t *testing.T) {
	raw := []byte(`{"conversation_id":"conv-1","generation_id":"g","hook_event_name":"preToolUse","workspace_roots":["/root"],
		"tool_name":"Shell","tool_input":{"command":"npm install","working_directory":"/project"},"tool_use_id":"abc123","cwd":"/project"}`)
	event, payload, ok := CursorToCanonical("preToolUse", raw)
	if !ok || event != "PreToolUse" {
		t.Fatalf("got (%q, %v), want (PreToolUse, true)", event, ok)
	}
	var in Input
	if err := json.Unmarshal(payload, &in); err != nil {
		t.Fatal(err)
	}
	if in.SessionID != "conv-1" || in.ToolName != "Bash" || in.Cwd != "/project" || in.ToolUseID != "abc123" || in.HookEventName != "PreToolUse" {
		t.Errorf("input = %+v", in)
	}
	var ti map[string]string
	_ = json.Unmarshal(in.ToolInput, &ti)
	if ti["command"] != "npm install" || ti["working_directory"] != "/project" {
		t.Errorf("tool_input lost fields: %v", ti)
	}
}

// A read arrives through beforeReadFile (its file_path is documented; the
// Read tool's own input shape is not) and must look like a Read call, or the
// credential-file guard never sees it.
func TestCursorReadComesThroughBeforeReadFile(t *testing.T) {
	raw := []byte(`{"conversation_id":"c","workspace_roots":["/w"],"file_path":"/home/u/.aws/credentials","content":"secret"}`)
	event, payload, ok := CursorToCanonical("beforeReadFile", raw)
	if !ok || event != "PreToolUse" {
		t.Fatalf("got (%q, %v)", event, ok)
	}
	var in Input
	_ = json.Unmarshal(payload, &in)
	var ti map[string]string
	_ = json.Unmarshal(in.ToolInput, &ti)
	if in.ToolName != "Read" || ti["file_path"] != "/home/u/.aws/credentials" || in.Cwd != "/w" {
		t.Errorf("input = %+v, tool_input = %v", in, ti)
	}
	if _, leaked := ti["content"]; leaked {
		t.Error("file contents were forwarded into tool_input")
	}
}

func TestCursorToCanonicalSkipsEventsWithNoChannel(t *testing.T) {
	for _, event := range []string{"beforeSubmitPrompt", "subagentStart", "stop", "afterFileEdit", ""} {
		if _, _, ok := CursorToCanonical(event, []byte(`{}`)); ok {
			t.Errorf("%q was accepted; deadeye has no response Cursor would deliver for it", event)
		}
	}
	if _, _, ok := CursorToCanonical("preToolUse", []byte(`not json`)); ok {
		t.Error("a non-JSON payload was accepted")
	}
}

func TestCursorToolNames(t *testing.T) {
	for cursor, want := range map[string]string{"Shell": "Bash", "Task": "Agent", "Grep": "Grep", "MCP:github_search": "mcp__github_search"} {
		_, payload, _ := CursorToCanonical("postToolUse", []byte(`{"conversation_id":"c","tool_name":"`+cursor+`","tool_output":"{\"exitCode\":0}"}`))
		var in Input
		_ = json.Unmarshal(payload, &in)
		if in.ToolName != want {
			t.Errorf("%s -> %s, want %s", cursor, in.ToolName, want)
		}
		if len(in.ToolResponse) == 0 {
			t.Errorf("%s: tool_output was not carried into tool_response", cursor)
		}
	}
}

// Cursor blocks an action whose permission-hook response doesn't match the
// schema, so "no opinion" must be an explicit allow -- on a deny-by-mismatch
// host, {} is the dangerous answer.
func TestMarshalCursorPermissionHooksAlwaysAnswer(t *testing.T) {
	for _, event := range []string{"preToolUse", "beforeReadFile"} {
		for name, out := range map[string]Output{"empty": Empty(), "advice only": {HookSpecificOutput: &HookSpecificOutput{AdditionalContext: "tip"}}} {
			m := decode(t, MarshalCursor(event, out))
			if m["permission"] != "allow" {
				t.Errorf("%s / %s: %v, want an explicit allow", event, name, m)
			}
		}
	}
}

func TestMarshalCursorDecisions(t *testing.T) {
	rewrite := Output{HookSpecificOutput: &HookSpecificOutput{PermissionDecision: PermissionAllow, UpdatedInput: json.RawMessage(`{"command":"npm ci"}`)}}
	m := decode(t, MarshalCursor("preToolUse", rewrite))
	if m["permission"] != "allow" || m["updated_input"].(map[string]any)["command"] != "npm ci" {
		t.Errorf("rewrite = %v", m)
	}

	guard := Output{HookSpecificOutput: &HookSpecificOutput{PermissionDecision: PermissionAsk, AskFallback: AskFallbackDeny, PermissionDecisionReason: "credential file"}}
	for _, event := range []string{"preToolUse", "beforeReadFile"} {
		m := decode(t, MarshalCursor(event, guard))
		if m["permission"] != "deny" || m["user_message"] != "credential file" {
			t.Errorf("%s: a guard ask must become a deny, got %v", event, m)
		}
	}
	if m := decode(t, MarshalCursor("beforeReadFile", guard)); m["agent_message"] != nil {
		t.Errorf("beforeReadFile has no agent_message field: %v", m)
	}

	nudge := Output{HookSpecificOutput: &HookSpecificOutput{PermissionDecision: PermissionAsk, AskFallback: AskFallbackAdvise, PermissionDecisionReason: "plan first"}}
	if m := decode(t, MarshalCursor("preToolUse", nudge)); m["permission"] != "allow" {
		t.Errorf("an advisory ask must not block on Cursor: %v", m)
	}

	ctx := Output{HookSpecificOutput: &HookSpecificOutput{AdditionalContext: "persona"}}
	for _, event := range []string{"sessionStart", "postToolUse"} {
		if m := decode(t, MarshalCursor(event, ctx)); m["additional_context"] != "persona" || m["permission"] != nil {
			t.Errorf("%s = %v", event, m)
		}
	}
	// stop's only output field is auto-submitted as the next user message.
	if got := string(MarshalCursor("stop", ctx)); got != "{}" {
		t.Errorf("stop = %s, want {}", got)
	}
}
