// Package receipts records WHERE deadeye acted -- the anchor none of its
// own self-measurement could work without.
//
// The decision log (internal/logstore) is deliberately thin: no repo, no
// cwd, no prompt, no commit. That keeps it cheap on the hook path, but it
// also means deadeye cannot check any of its own claims after the fact --
// "the reviewer is precise", "coder mode makes code leaner" were both
// unfalsifiable, because nothing tied a review or a coder session to a
// repository and a commit. A receipt is that tie: a review ran in repo R
// with HEAD at commit C over paths P, or a coder session started in repo R
// at commit C at level L.
//
// Commit is always the BASE -- HEAD at the time deadeye acted, before the
// work it was acting on has landed. `deadeye misses` scans forward from
// there; `deadeye adherence` reads the commits after it.
//
// This is the third hand-rolled append-only JSONL store in the codebase
// (internal/logstore, internal/lessons, here). internal/lessons already
// wrote down why it didn't generalize logstore -- a type parameter
// threaded through every decisions.jsonl call site for one small extra
// file -- and the same trade holds here. Consolidating all three behind
// one generic store is a reasonable later cleanup; doing it inside this
// bundle would mean touching the live routing feedback loop for no gain
// to the features being shipped.
package receipts

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Kind values. Deliberately few: a receipt marks a deadeye pass that has
// something later-checkable about it, not every action it takes.
const (
	// KindReview is one completed review pass -- /deadeye-review,
	// /deadeye-pr, or /deadeye-guard. Scope says which shape it was.
	KindReview = "review"
	// KindCoder is a coder-mode session start or level switch.
	KindCoder = "coder"
)

// Receipt is one "deadeye acted here" record.
type Receipt struct {
	TS        string `json:"ts"`
	SessionID string `json:"session_id,omitempty"`
	// Repo is gitutil.ProjectKey(cwd) -- the same repo key the outcomes
	// store uses, so the two can be joined.
	Repo string `json:"repo"`
	// Commit is HEAD at the moment deadeye acted: the BASE the reviewed
	// work lands on top of, not the commit containing it. Empty when the
	// cwd isn't a git repo (fail-open, like every gitutil read).
	Commit string `json:"commit"`
	Kind   string `json:"kind"`
	// Level is the coder level, for KindCoder only.
	Level string `json:"level,omitempty"`
	// Scope is which review shape ran, for KindReview only: "diff",
	// "staged", "repo", "pr", or "guard".
	Scope string `json:"scope,omitempty"`
	// Paths are the files the reviewed diff touched, repo-root-relative.
	// Empty for a whole-repo pass, which has no single diff.
	Paths []string `json:"paths,omitempty"`
}

// Store is an append-only JSONL log of receipts.
type Store struct {
	mu   sync.Mutex
	path string
}

func Open(path string) *Store { return &Store{path: path} }

func (s *Store) Append(r Receipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	// Same 10MB rotate-to-.1 backstop as logstore and lessons. Receipts
	// are per-review and per-coder-session, so this is a far-off ceiling,
	// but an unbounded file in the state dir is the thing being avoided.
	if fi, err := os.Stat(s.path); err == nil && fi.Size() > 10<<20 {
		_ = os.Rename(s.path, s.path+".1")
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(b)
	return err
}

// Scan reads every receipt in the log. A missing file is not an error --
// it means deadeye hasn't recorded a pass yet, which is the normal state
// until the first review or coder session after this ships.
func Scan(path string) ([]Receipt, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Receipt
	for _, line := range bytes.Split(b, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var r Receipt
		if json.Unmarshal(line, &r) != nil {
			continue // skip malformed lines rather than fail the whole scan
		}
		out = append(out, r)
	}
	return out, nil
}

// ForRepo filters receipts to one repo key and kind. Both reports are
// repo-scoped by default -- the same scoping `deadeye lessons priority`
// already uses, since accuracy in one codebase says nothing about
// accuracy in another.
func ForRepo(rs []Receipt, repo, kind string) []Receipt {
	var out []Receipt
	for _, r := range rs {
		if r.Repo == repo && r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}
