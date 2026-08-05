// Package sessionmem implements PLAN.md §5.7: a compact per-project
// session summary (branch, recent commits, modified files, decisions
// logged) written at SessionEnd and injected at the start of the next
// session, to cut the re-orientation tax a fresh session pays rediscovering
// the project. Its structural sibling is internal/codemap: this captures
// what recently CHANGED, codemap captures what the project IS.
//
// Native-restore guard (PLAN.md §5.7/§10.10): SessionStart's source field
// distinguishes resume/compact -- sessions whose context Claude Code
// itself just restored -- from startup/clear. The daemon marks those
// sessions and decideUserPromptSubmit skips LoadRecent for them, so this
// summary never repeats what the transcript replay or compaction summary
// already carries.
package sessionmem

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/gitutil"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

func Dir() string { return filepath.Join(meta.StateDir(), "sessions") }

const (
	freshnessGuard = 30 * time.Second // skip summaries this fresh when loading -- likely same-session artifacts
	headLines      = 25
)

// Write creates a per-session summary file at SessionEnd. Skipped
// entirely when the session had no git activity and no logged decisions
// -- "skip when the session had no meaningful activity" per PLAN.md §5.7.
func Write(cwd, sessionID string, decisionCount int) error {
	branch := gitutil.Output(cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "" {
		return nil // not a git repo
	}
	commits := gitutil.Output(cwd, "log", "-5", "--oneline")
	status := gitutil.Output(cwd, "status", "--porcelain")
	if commits == "" && status == "" && decisionCount == 0 {
		return nil
	}

	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Session summary: %s\n\nbranch: %s\n\n", gitutil.ProjectKey(cwd), branch)
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

	path := filepath.Join(Dir(), fmt.Sprintf("%s_%d.md", gitutil.ProjectKey(cwd), time.Now().UnixNano()))
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return err
	}
	pruneOldSummaries(gitutil.ProjectKey(cwd))
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
	prefix := gitutil.ProjectKey(cwd) + "_"
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
