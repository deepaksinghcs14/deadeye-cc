package receipts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendScanRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipts.jsonl")
	s := Open(path)
	in := []Receipt{
		{TS: "2026-09-29T00:00:00Z", Repo: "deadeye-cc", Commit: "abc123", Kind: KindReview, Scope: "diff", Paths: []string{"a.go", "b.go"}},
		{TS: "2026-09-29T01:00:00Z", Repo: "deadeye-cc", Commit: "def456", Kind: KindCoder, Level: "marksman"},
	}
	for _, r := range in {
		if err := s.Append(r); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	got, err := Scan(path)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d receipts, want 2", len(got))
	}
	if got[0].Scope != "diff" || len(got[0].Paths) != 2 || got[0].Paths[1] != "b.go" {
		t.Errorf("review receipt did not round-trip: %+v", got[0])
	}
	if got[1].Level != "marksman" || got[1].Kind != KindCoder {
		t.Errorf("coder receipt did not round-trip: %+v", got[1])
	}
}

// A missing file means "deadeye hasn't recorded a pass yet" -- the normal
// state until the first review after this ships, not an error the reports
// should surface.
func TestScanMissingFileIsNotAnError(t *testing.T) {
	got, err := Scan(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil {
		t.Fatalf("missing file returned error: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// One corrupt line (a half-written record from a killed process) must not
// discard every other receipt in the file.
func TestScanSkipsMalformedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipts.jsonl")
	body := `{"ts":"2026-09-29T00:00:00Z","repo":"r","commit":"a","kind":"review"}
{"ts":"trunc
{"ts":"2026-09-29T02:00:00Z","repo":"r","commit":"b","kind":"coder"}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Scan(path)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d receipts, want the 2 well-formed ones", len(got))
	}
}

func TestAppendCreatesStateDirAndPrivatePerms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "receipts.jsonl")
	if err := Open(path).Append(Receipt{TS: "t", Repo: "r", Kind: KindCoder}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("perms %o, want 600 -- receipts carry repo paths", got)
	}
}

func TestRotatesPastTenMB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipts.jsonl")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", (10<<20)+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Open(path).Append(Receipt{TS: "t", Repo: "r", Kind: KindCoder}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("oversized log was not rotated to .1: %v", err)
	}
	got, err := Scan(path)
	if err != nil || len(got) != 1 {
		t.Errorf("post-rotation log holds %d receipts (err %v), want the 1 just appended", len(got), err)
	}
}

func TestForRepoFiltersRepoAndKind(t *testing.T) {
	all := []Receipt{
		{Repo: "a", Kind: KindReview},
		{Repo: "a", Kind: KindCoder},
		{Repo: "b", Kind: KindReview},
		{Repo: "a", Kind: KindReview},
	}
	if got := ForRepo(all, "a", KindReview); len(got) != 2 {
		t.Errorf("got %d, want 2 review receipts for repo a", len(got))
	}
	if got := ForRepo(all, "a", KindCoder); len(got) != 1 {
		t.Errorf("got %d, want 1 coder receipt for repo a", len(got))
	}
	if got := ForRepo(all, "zzz", KindReview); got != nil {
		t.Errorf("got %v, want nil for an unknown repo", got)
	}
}
