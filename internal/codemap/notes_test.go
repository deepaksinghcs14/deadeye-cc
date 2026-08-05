package codemap

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestAppendNoteThenPrune(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initGitRepo(t, "a.go")

	for i := 0; i < keepNotes+3; i++ {
		if err := AppendNote(repo, "explore", fmt.Sprintf("finding number %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := PruneNotes(repo); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(NotesPath(repo))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Count(string(b), "## ")
	if got != keepNotes {
		t.Errorf("got %d sections after prune, want %d", got, keepNotes)
	}
	// The NEWEST sections must be the survivors.
	if !strings.Contains(string(b), fmt.Sprintf("finding number %d", keepNotes+2)) {
		t.Error("newest section did not survive the prune")
	}
	if strings.Contains(string(b), "finding number 0") {
		t.Error("oldest section survived the prune")
	}
}

func TestPruneSurvivesFreeformText(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initGitRepo(t, "a.go")

	// A malformed append with no heading -- folded, never lost or fatal.
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(NotesPath(repo), []byte("stray text with no heading\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < keepNotes+2; i++ {
		if err := AppendNote(repo, "explore", fmt.Sprintf("n%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := PruneNotes(repo); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(NotesPath(repo))
	if strings.Count(string(b), "## ") != keepNotes {
		t.Errorf("prune with a leading headingless chunk kept wrong count:\n%s", b)
	}
}

// TestAppendNoteConcurrentWithPruneDoesNotCorrupt is the concrete test for
// the append-vs-truncate safety argument: AppendNote (O_APPEND, lockless)
// racing PruneNotes (truncating, mutexed) must never leave the file
// unreadable. Run under -race.
func TestAppendNoteConcurrentWithPruneDoesNotCorrupt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initGitRepo(t, "a.go")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			_ = AppendNote(repo, "explore", fmt.Sprintf("burst %d", i))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			_ = PruneNotes(repo)
		}
	}()
	wg.Wait()

	// The file must load without error and stay a plausible section log.
	if _, err := os.ReadFile(NotesPath(repo)); err != nil {
		t.Fatalf("notes file unreadable after concurrent append/prune: %v", err)
	}
	_ = LoadNotes(repo) // must not panic
}

func TestLoadNotesCapsAtHeadLines(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initGitRepo(t, "a.go")

	long := strings.Repeat("line\n", notesHeadLines*2)
	if err := AppendNote(repo, "explore", long); err != nil {
		t.Fatal(err)
	}
	got := LoadNotes(repo)
	if n := len(strings.Split(got, "\n")); n > notesHeadLines {
		t.Errorf("LoadNotes returned %d lines, cap is %d", n, notesHeadLines)
	}
}

func TestLoadNotesEmptyWhenAbsent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := LoadNotes(t.TempDir()); got != "" {
		t.Errorf("expected empty notes, got %q", got)
	}
}
