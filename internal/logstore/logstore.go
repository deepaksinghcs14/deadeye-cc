// Package logstore is the decision log: an append-only JSONL file at
// ~/.deadeye/decisions.jsonl (PLAN.md §3.3, amended from SQLite to JSONL --
// see docs/verified.md). Every write is a single O_APPEND syscall under
// 4KB, which POSIX guarantees is atomic, so concurrent hook processes
// across a session need no cross-process locking; the in-process mutex
// below only protects the daemon's own goroutines from interleaving.
package logstore

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Record is one decision or preprocessing action. Not every field applies
// to every surface -- e.g. bytes_before_est/bytes_after are preprocess-only.
type Record struct {
	TS             string `json:"ts"`
	SessionID      string `json:"session_id,omitempty"`
	Surface        string `json:"surface"` // e.g. "PreToolUse/Bash"
	Action         string `json:"action"`  // e.g. "rewrite", "advise", "measured", "observed"
	Reason         string `json:"reason,omitempty"`
	BytesBeforeEst int    `json:"bytes_before_est,omitempty"`
	BytesAfter     int    `json:"bytes_after,omitempty"`
}

const maxSizeBytes = 10 * 1024 * 1024

type Store struct {
	mu   sync.Mutex
	path string
}

func Open(path string) *Store { return &Store{path: path} }

// Append writes one record. Errors are returned (the daemon may choose to
// ignore them -- logging failure must never block the underlying hook
// decision, per INV-5).
func (s *Store) Append(r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	if fi, err := os.Stat(s.path); err == nil && fi.Size() > maxSizeBytes {
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

// Scan reads every record in the log, in file order, for /deadeye-status
// and /deadeye-audit. A missing file is not an error -- it means no
// decisions have been logged yet.
func Scan(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var records []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue // skip malformed lines rather than fail the whole scan
		}
		records = append(records, r)
	}
	return records, sc.Err()
}
