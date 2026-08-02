// Package sessionmem implements PLAN.md §5.7: a compact per-project
// session summary (branch, recent commits, modified files, decisions
// logged) written at SessionEnd and injected at the start of the next
// session, to cut the re-orientation tax a fresh session pays rediscovering
// the project.
//
// ponytail: no native-resume-overlap guard. PLAN.md §5.7 calls for
// detecting whether Claude Code's own native resume-from-summary is
// active for the session and standing down if so (§10.10, unresolved --
// see docs/verified.md). Without a confirmed detection signal this ships
// unconditionally; worst case is a redundant paragraph of context, not a
// correctness problem. Add the guard once §10.10 is answered live.
package sessionmem

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// gitTimeout bounds every git subprocess here -- a stalled network mount or
// a held .git/index.lock must not hang SessionEnd forever (INV-5: fail
// open means timing out and treating it like "not a git repo", not hanging
// the daemon goroutine handling it).
const gitTimeout = 2 * time.Second

func Dir() string { return filepath.Join(meta.StateDir(), "sessions") }

const (
	freshnessGuard = 30 * time.Second // skip summaries this fresh when loading -- likely same-session artifacts
	headLines      = 25
)

// ProjectKey derives a filesystem-safe project identifier from cwd: the
// git repo root's basename if available, else cwd's own basename.
func ProjectKey(cwd string) string {
	base := cwd
	if root := gitOutput(cwd, "rev-parse", "--show-toplevel"); root != "" {
		base = root
	}
	name := filepath.Base(base)
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "project"
	}
	return sanitize(name)
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

func gitOutput(cwd string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Write creates a per-session summary file at SessionEnd. Skipped
// entirely when the session had no git activity and no logged decisions
// -- "skip when the session had no meaningful activity" per PLAN.md §5.7.
func Write(cwd, sessionID string, decisionCount int) error {
	branch := gitOutput(cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "" {
		return nil // not a git repo
	}
	commits := gitOutput(cwd, "log", "-5", "--oneline")
	status := gitOutput(cwd, "status", "--porcelain")
	if commits == "" && status == "" && decisionCount == 0 {
		return nil
	}

	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Session summary: %s\n\nbranch: %s\n\n", ProjectKey(cwd), branch)
	if commits != "" {
		b.WriteString("Recent commits:\n")
		for _, line := range strings.Split(commits, "\n") {
			fmt.Fprintf(&b, "  %s\n", line)
		}
		b.WriteString("\n")
	}
	if status != "" {
		b.WriteString("Modified/staged files:\n")
		for _, line := range strings.Split(status, "\n") {
			fmt.Fprintf(&b, "  %s\n", line)
		}
		b.WriteString("\n")
	}
	if decisionCount > 0 {
		fmt.Fprintf(&b, "deadeye logged %d decisions this session.\n", decisionCount)
	}

	path := filepath.Join(Dir(), fmt.Sprintf("%s_%d.md", ProjectKey(cwd), time.Now().UnixNano()))
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return err
	}
	pruneOldSummaries(ProjectKey(cwd))
	return nil
}

// keepSummaries caps how many past summaries a single project accumulates.
// Write creates one new file per session forever otherwise, and
// LoadRecent stats every matching file on every session start -- so an
// unbounded count means startup cost grows with how many sessions a
// project has EVER had, not just the handful that matter. 3, not 1:
// LoadRecent's own freshness guard skips the newest file for a short
// window after it's written, so keeping only 1 could leave nothing
// eligible to load immediately after a session ends.
const keepSummaries = 3

// pruneOldSummaries deletes all but the newest keepSummaries files for
// projectKey. Best-effort: a failure here just means one extra file
// survives to the next prune, not a correctness problem.
func pruneOldSummaries(projectKey string) {
	entries, err := os.ReadDir(Dir())
	if err != nil {
		return
	}
	prefix := projectKey + "_"
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			names = append(names, e.Name())
		}
	}
	if len(names) <= keepSummaries {
		return
	}
	// Filenames are "<project>_<unixnano>.md" -- the nanosecond suffix
	// sorts lexically the same as chronologically for any realistic time
	// range, so a plain string sort avoids a stat() per file just to rank
	// them by age.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, name := range names[keepSummaries:] {
		os.Remove(filepath.Join(Dir(), name))
	}
}

// LoadRecent returns the head (<=25 lines) of the most recent summary for
// cwd's project, skipping anything written within the freshness guard.
// Returns "" if there is none.
func LoadRecent(cwd string) string {
	entries, err := os.ReadDir(Dir())
	if err != nil {
		return ""
	}
	prefix := ProjectKey(cwd) + "_"
	var newestPath string
	var newestTime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < freshnessGuard {
			continue
		}
		if info.ModTime().After(newestTime) {
			newestTime = info.ModTime()
			newestPath = filepath.Join(Dir(), e.Name())
		}
	}
	if newestPath == "" {
		return ""
	}
	b, err := os.ReadFile(newestPath)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) > headLines {
		lines = lines[:headLines]
	}
	return strings.Join(lines, "\n")
}
