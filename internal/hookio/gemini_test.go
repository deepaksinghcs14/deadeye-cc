package hookio

import (
	"encoding/json"
	"strings"
	"testing"
)

// decode is a tiny helper to inspect Gemini-marshaled bytes as a map.
func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("output isn't valid JSON: %v (%s)", err, b)
	}
	return m
}

func TestMarshalGeminiEmpty(t *testing.T) {
	if got := string(MarshalGemini(Empty())); got != "{}" {
		t.Errorf("empty output = %q, want {}", got)
	}
}

func TestMarshalGeminiAdditionalContext(t *testing.T) {
	out := ForEvent("SessionStart")
	out.HookSpecificOutput.AdditionalContext = "deadeye guidance"
	m := decode(t, MarshalGemini(out))
	hs, _ := m["hookSpecificOutput"].(map[string]any)
	if hs == nil || hs["additionalContext"] != "deadeye guidance" {
		t.Errorf("additionalContext not carried: %v", m)
	}
	// Claude-only field names must not appear.
	if _, has := m["permissionDecision"]; has {
		t.Error("Gemini output must not carry permissionDecision")
	}
}

func TestMarshalGeminiToolInputRename(t *testing.T) {
	out := ForEvent("PreToolUse")
	out.HookSpecificOutput.UpdatedInput = json.RawMessage(`{"command":"echo hi"}`)
	m := decode(t, MarshalGemini(out))
	hs, _ := m["hookSpecificOutput"].(map[string]any)
	if hs == nil || hs["tool_input"] == nil {
		t.Errorf("updatedInput not renamed to tool_input: %v", m)
	}
	if _, has := hs["updatedInput"]; has {
		t.Error("Gemini output must not carry updatedInput (Claude's name)")
	}
}

// TestMarshalGeminiAskDeny: an exfil-style ask (AskFallback deny) becomes a
// hard deny + reason on Gemini -- the model is blocked, not nudged.
func TestMarshalGeminiAskDeny(t *testing.T) {
	out := ForEvent("PreToolUse")
	out.HookSpecificOutput.PermissionDecision = PermissionAsk
	out.HookSpecificOutput.AskFallback = AskFallbackDeny
	out.HookSpecificOutput.PermissionDecisionReason = "credential egress blocked"

	m := decode(t, MarshalGemini(out))
	if m["decision"] != "deny" {
		t.Errorf("ask+deny should become decision:deny, got %v", m["decision"])
	}
	if !strings.Contains(m["reason"].(string), "credential egress") {
		t.Errorf("deny should carry the reason, got %v", m["reason"])
	}
}

// TestMarshalGeminiAskAdvise: a plan-gate/vuln-add ask (AskFallback advise)
// downgrades to additionalContext -- never blocks -- on Gemini.
func TestMarshalGeminiAskAdvise(t *testing.T) {
	out := ForEvent("PreToolUse")
	out.HookSpecificOutput.PermissionDecision = PermissionAsk
	out.HookSpecificOutput.AskFallback = AskFallbackAdvise
	out.HookSpecificOutput.PermissionDecisionReason = "plan gate: multi-file change"

	m := decode(t, MarshalGemini(out))
	if _, blocked := m["decision"]; blocked {
		t.Errorf("ask+advise must NOT block on Gemini, got decision=%v", m["decision"])
	}
	hs, _ := m["hookSpecificOutput"].(map[string]any)
	if hs == nil || !strings.Contains(hs["additionalContext"].(string), "plan gate") {
		t.Errorf("ask+advise should surface the reason as context, got %v", m)
	}
}

// TestMarshalForDialect: hosts get the dialect their hook engine accepts.
func TestMarshalForDialect(t *testing.T) {
	out := ForEvent("PreToolUse")
	out.HookSpecificOutput.PermissionDecision = PermissionAsk
	out.HookSpecificOutput.AskFallback = AskFallbackDeny
	out.HookSpecificOutput.PermissionDecisionReason = "r"

	for _, host := range []string{"", "claude"} {
		m := decode(t, MarshalFor(host, out))
		hs, _ := m["hookSpecificOutput"].(map[string]any)
		if hs == nil || hs["permissionDecision"] != "ask" {
			t.Errorf("host %q should keep Claude-dialect permissionDecision:ask, got %v", host, m)
		}
	}
	if m := decode(t, MarshalFor("codex", out)); m["hookSpecificOutput"].(map[string]any)["permissionDecision"] != "deny" {
		t.Errorf("codex should translate ask+deny to nested permissionDecision:deny, got %v", m)
	}
	// Gemini translates ask+deny to a top-level deny.
	if m := decode(t, MarshalFor("gemini", out)); m["decision"] != "deny" {
		t.Errorf("gemini should translate to decision:deny, got %v", m)
	}
}

func TestMarshalCodexAskAdvise(t *testing.T) {
	out := ForEvent("PreToolUse")
	out.HookSpecificOutput.PermissionDecision = PermissionAsk
	out.HookSpecificOutput.AskFallback = AskFallbackAdvise
	out.HookSpecificOutput.PermissionDecisionReason = "plan gate"

	m := decode(t, MarshalCodex(out))
	hs, _ := m["hookSpecificOutput"].(map[string]any)
	if hs == nil || hs["permissionDecision"] != nil {
		t.Errorf("ask+advise must not emit permissionDecision on Codex, got %v", m)
	}
	if !strings.Contains(hs["additionalContext"].(string), "plan gate") {
		t.Errorf("ask+advise should surface the reason as context, got %v", m)
	}
}

func TestMarshalCodexStopUsesCommonOutput(t *testing.T) {
	out := ForEvent("Stop")
	out.HookSpecificOutput.AdditionalContext = "deadeye: summary"

	m := decode(t, MarshalCodex(out))
	if _, ok := m["hookSpecificOutput"]; ok {
		t.Errorf("Codex Stop must not emit hookSpecificOutput, got %v", m)
	}
	if m["systemMessage"] != "deadeye: summary" {
		t.Errorf("Codex Stop should carry the message as systemMessage, got %v", m)
	}
}
