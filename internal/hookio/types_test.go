package hookio

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestEmptyMarshalsToBraces enforces INV-5/INV-8's contract: a no-op
// decision must be exactly the two bytes "{}", never null, never an object
// with empty-but-present fields (which would still cost stdout parsing and
// could confuse a strict client).
func TestEmptyMarshalsToBraces(t *testing.T) {
	b, err := json.Marshal(Empty())
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "{}" {
		t.Fatalf("Empty() marshaled to %q, want \"{}\"", b)
	}
}

// TestForEventSetsRequiredHookEventName guards the bug the live validator
// caught in Phase 0: hookSpecificOutput.hookEventName is REQUIRED, and a
// response missing it is rejected outright (not merely defaulted). ForEvent
// is the only sanctioned way to build a HookSpecificOutput so this can't
// regress.
func TestForEventSetsRequiredHookEventName(t *testing.T) {
	out := ForEvent("PreToolUse")
	if out.HookSpecificOutput == nil || out.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Fatalf("ForEvent(%q) = %+v, want HookEventName set", "PreToolUse", out.HookSpecificOutput)
	}
}

// TestMergeToolInputPreservesOtherFields is the regression test for the
// bug a live session caught: UpdatedInput replaces tool_input wholesale,
// it does not merge server-side. A rewrite that only sends the changed
// key would silently drop every other required field -- confirmed live
// against the real Agent tool ("The required parameter `description` is
// missing") even though the same shortcut happened to work harmlessly for
// Bash, whose other fields are all optional.
func TestMergeToolInputPreservesOtherFields(t *testing.T) {
	original := json.RawMessage(`{"description":"d","prompt":"p","subagent_type":"general-purpose"}`)
	merged, err := MergeToolInput(original, map[string]any{"model": "haiku"})
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"description": "d", "prompt": "p", "subagent_type": "general-purpose", "model": "haiku"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("merged[%q] = %v, want %v (full merged: %s)", k, got[k], v, merged)
		}
	}
}

func TestMergeToolInputHandlesEmptyOriginal(t *testing.T) {
	merged, err := MergeToolInput(nil, map[string]any{"command": "echo hi"})
	if err != nil {
		t.Fatal(err)
	}
	if string(merged) != `{"command":"echo hi"}` {
		t.Errorf("merged = %s, want {\"command\":\"echo hi\"}", merged)
	}
}

// TestGoldenPayloads unmarshals every captured fixture (real payloads from
// a live `claude -p --plugin-dir` session against Claude Code v2.1.220,
// DEADEYE_CAPTURE=1 -- see docs/verified.md) and checks the fields this
// package reads survive the round trip.
func TestGoldenPayloads(t *testing.T) {
	files, err := filepath.Glob("../../testdata/payloads/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no fixtures found under testdata/payloads/")
	}
	for _, f := range files {
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var in Input
			if err := json.Unmarshal(raw, &in); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if in.HookEventName == "" {
				t.Error("hook_event_name missing after unmarshal")
			}
			if in.SessionID == "" {
				t.Error("session_id missing after unmarshal")
			}
		})
	}
}

// TestRawNeverSerializes: Output.Raw is a transport instruction (write
// verbatim), never a JSON field -- an Output that also carried Raw in its
// marshaled form would leak bytes into a JSON hook response.
func TestRawNeverSerializes(t *testing.T) {
	out := Empty()
	out.Raw = []byte("DEADEYE CODER ACTIVE")
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "{}" {
		t.Errorf("marshal = %s, want {} (Raw must be json:\"-\")", b)
	}
}
