package codemap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestMergeTouchedAccumulatesAcrossSessions is the key regression test for
// the whole Layer B1 design: counts must COMPOUND across sessions, not
// reset -- the difference between a "core files" view and a rolling log.
func TestMergeTouchedAccumulatesAcrossSessions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initGitRepo(t, "a.go", "b.go", "c.go")

	// Session 1 touches a and b; session 2 touches a and c.
	if err := MergeTouched(repo, []string{filepath.Join(repo, "a.go"), filepath.Join(repo, "b.go")}, 1000); err != nil {
		t.Fatal(err)
	}
	if err := MergeTouched(repo, []string{filepath.Join(repo, "a.go"), filepath.Join(repo, "c.go")}, 2000); err != nil {
		t.Fatal(err)
	}

	tf := LoadTouchFrequency(repo)
	if tf.Counts["a.go"] != 2 {
		t.Errorf("overlapping file a.go count = %d, want 2 (accumulation)", tf.Counts["a.go"])
	}
	if tf.Counts["b.go"] != 1 || tf.Counts["c.go"] != 1 {
		t.Errorf("session-unique files should sit at 1: %v", tf.Counts)
	}
}

func TestMergeTouchedRejectsPathsOutsideRepo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initGitRepo(t, "a.go")

	if err := MergeTouched(repo, []string{"/etc/passwd", filepath.Join(repo, "a.go")}, 1000); err != nil {
		t.Fatal(err)
	}
	tf := LoadTouchFrequency(repo)
	if len(tf.Counts) != 1 || tf.Counts["a.go"] != 1 {
		t.Errorf("outside-repo path leaked into the counter: %v", tf.Counts)
	}
}

func TestMergeTouchedCapsToTopN(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initGitRepo(t, "a.go")

	// One "core" file touched every session, plus a stream of one-offs.
	core := filepath.Join(repo, "a.go")
	for session := 0; session < touchFreqCap+8; session++ {
		oneOff := filepath.Join(repo, fmt.Sprintf("one%d.go", session))
		if err := os.WriteFile(oneOff, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := MergeTouched(repo, []string{core, oneOff}, int64(1000+session)); err != nil {
			t.Fatal(err)
		}
	}
	tf := LoadTouchFrequency(repo)
	if len(tf.Counts) > touchFreqCap {
		t.Errorf("counter grew past the cap: %d entries", len(tf.Counts))
	}
	if tf.Counts["a.go"] < touchFreqCap {
		t.Errorf("the highest-count file must survive every cap pass, got count %d", tf.Counts["a.go"])
	}
}

// TestMergeTouchedConcurrentCallsDoNotRace is the regression test for the
// concurrency bug review caught: the daemon handles each hook connection
// in its own goroutine, so two same-project SessionEnds can interleave.
// Run under -race (CI's `make check` does).
func TestMergeTouchedConcurrentCallsDoNotRace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initGitRepo(t, "a.go")
	target := filepath.Join(repo, "a.go")

	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := MergeTouched(repo, []string{target}, int64(1000+i)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()

	tf := LoadTouchFrequency(repo)
	if tf.Counts["a.go"] != n {
		t.Errorf("lost update: count = %d, want %d", tf.Counts["a.go"], n)
	}
	// The file must also still be valid JSON (no torn write).
	b, err := os.ReadFile(TouchPath(repo))
	if err != nil {
		t.Fatal(err)
	}
	var probe TouchFrequency
	if err := json.Unmarshal(b, &probe); err != nil {
		t.Errorf("touched.json corrupted by concurrent writes: %v", err)
	}
}

func TestLoadTouchFrequencyEmptyWhenAbsent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tf := LoadTouchFrequency(t.TempDir())
	if len(tf.Counts) != 0 {
		t.Errorf("expected an empty counter, got %v", tf.Counts)
	}
}
