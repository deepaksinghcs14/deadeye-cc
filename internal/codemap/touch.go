package codemap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/gitutil"
)

const touchFreqCap = 12 // ranked entries kept -- a "core files" view, not a full log

// TouchFrequency accumulates across every session this project has EVER
// had, not just the last few -- a file that keeps coming up across 30
// sessions stays visible; a file touched once months ago sinks toward the
// bottom on its own, rather than scrolling off a fixed-length log after 5
// sessions regardless of how central it actually is.
type TouchFrequency struct {
	Counts   map[string]int   `json:"counts"`
	LastSeen map[string]int64 `json:"last_seen"` // unix seconds; tie-breaker when capping
}

// TouchPath is the per-project counter file.
func TouchPath(cwd string) string {
	return filepath.Join(Dir(), gitutil.ProjectKey(cwd)+".touched.json")
}

// LoadTouchFrequency reads cwd's counter, or an empty one on any
// missing/malformed file -- fail open, a lost counter is a cold start,
// never an error.
func LoadTouchFrequency(cwd string) TouchFrequency {
	tf := TouchFrequency{Counts: map[string]int{}, LastSeen: map[string]int64{}}
	b, err := os.ReadFile(TouchPath(cwd))
	if err != nil {
		return tf
	}
	var loaded TouchFrequency
	if json.Unmarshal(b, &loaded) != nil || loaded.Counts == nil {
		return tf
	}
	if loaded.LastSeen == nil {
		loaded.LastSeen = map[string]int64{}
	}
	return loaded
}

// MergeTouched increments this session's touched paths into the persistent
// counts under codemapMu, then keeps only the top touchFreqCap entries by
// count (ties broken by most-recently-touched) -- bounded by RELEVANCE,
// not recency, so richness grows and stabilizes instead of resetting every
// few sessions. Paths outside the repo root are dropped, never persisted
// (a Read of /etc/passwd or another project's file must not leak into this
// project's map). nowUnix is a parameter for deterministic tests.
func MergeTouched(cwd string, sessionTouched []string, nowUnix int64) error {
	root := gitutil.Output(cwd, "rev-parse", "--show-toplevel")
	if root == "" || len(sessionTouched) == 0 {
		return nil
	}
	// Symlink-resolve the root once so containment checks compare real
	// paths: on macOS, git reports /private/var/... while callers may hold
	// the /var/... symlinked form of the same location -- comparing the two
	// raw strings wrongly classifies every in-repo file as outside it
	// (caught by this package's own tests before it could ship).
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}

	codemapMu.Lock()
	defer codemapMu.Unlock()

	tf := LoadTouchFrequency(cwd)
	merged := false
	for _, abs := range sessionTouched {
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			continue // outside the repo -- never persisted
		}
		// A control byte (e.g. a raw newline, legal in a filename on
		// macOS/Linux) would otherwise flow verbatim into renderTouch's
		// one-line-per-path output below, splitting a row across two lines
		// -- same corruption class sanitizeControlBytes closes for
		// lsFiles' git-tracked paths, needed again here because touched
		// paths come from real Read/Edit/Write calls, a separate source.
		rel = sanitizeControlBytes(filepath.ToSlash(rel))
		tf.Counts[rel]++
		tf.LastSeen[rel] = nowUnix
		merged = true
	}
	if !merged {
		return nil
	}

	// Cap by relevance: keep the top touchFreqCap by count, most-recent
	// wins ties. Everything else is forgotten -- it can re-earn its slot.
	if len(tf.Counts) > touchFreqCap {
		type kv struct {
			path  string
			count int
			seen  int64
		}
		ranked := make([]kv, 0, len(tf.Counts))
		for p, c := range tf.Counts {
			ranked = append(ranked, kv{p, c, tf.LastSeen[p]})
		}
		sort.Slice(ranked, func(i, j int) bool {
			if ranked[i].count != ranked[j].count {
				return ranked[i].count > ranked[j].count
			}
			return ranked[i].seen > ranked[j].seen
		})
		kept := TouchFrequency{Counts: map[string]int{}, LastSeen: map[string]int64{}}
		for _, r := range ranked[:touchFreqCap] {
			kept.Counts[r.path] = r.count
			kept.LastSeen[r.path] = r.seen
		}
		tf = kept
	}

	b, err := json.Marshal(tf)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	return atomicWriteFile(TouchPath(cwd), b, 0o600)
}

// renderTouch returns the ranked "core files" list for injection, or "".
func renderTouch(cwd string) string {
	tf := LoadTouchFrequency(cwd)
	if len(tf.Counts) == 0 {
		return ""
	}
	type kv struct {
		path  string
		count int
	}
	ranked := make([]kv, 0, len(tf.Counts))
	for p, c := range tf.Counts {
		ranked = append(ranked, kv{p, c})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].path < ranked[j].path
	})
	var b strings.Builder
	b.WriteString("Frequently touched files in this project (accumulated across sessions):\n")
	for _, r := range ranked {
		unit := "sessions"
		if r.count == 1 {
			unit = "session"
		}
		fmt.Fprintf(&b, "  %-40s %d %s\n", r.path, r.count, unit)
	}
	return strings.TrimRight(b.String(), "\n")
}
