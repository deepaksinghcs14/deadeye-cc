package logstore

import (
	"path/filepath"
	"testing"
)

func TestAppendAndScan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.jsonl")
	s := Open(path)

	want := []Record{
		{TS: "2026-08-02T00:00:00Z", Surface: "PreToolUse/Bash", Action: "noop"},
		{TS: "2026-08-02T00:00:01Z", Surface: "SessionStart", Action: "noop", Reason: "phase0"},
	}
	for _, r := range want {
		if err := s.Append(r); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Scan(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d records, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("record %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestScanMissingFileIsNotError(t *testing.T) {
	got, err := Scan(filepath.Join(t.TempDir(), "does-not-exist.jsonl"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil records, got %v", got)
	}
}
