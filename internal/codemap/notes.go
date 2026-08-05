package codemap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/gitutil"
)

const (
	notesHeadLines = 30 // lines of notes actually injected
	keepNotes      = 5  // "## " sections retained; pruned lazily by the daemon
	maxNoteBytes   = 2 << 10
)

// NotesPath is the per-project exploration-notes file.
func NotesPath(cwd string) string {
	return filepath.Join(Dir(), gitutil.ProjectKey(cwd)+".notes.md")
}

// AppendNote is `deadeye notes-append`'s handler: resolves cwd's project
// via gitutil.ProjectKey (real Go, not prompt-replicated logic), then
// appends one "## <ts> <kind>\n<body>" section via O_APPEND -- safe to
// call from a short-lived CLI process with no daemon involvement and no
// lock, same single-atomic-append reasoning logstore.Append relies on.
// Deliberately does NOT take codemapMu: the CLI process and the daemon are
// different processes, so an in-process mutex couldn't order them anyway;
// append-vs-truncate safety comes from the append being one small write.
func AppendNote(cwd, kind, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	if len(body) > maxNoteBytes {
		body = body[:maxNoteBytes]
	}
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(NotesPath(cwd), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	section := fmt.Sprintf("## %s %s\n%s\n\n", time.Now().UTC().Format("2006-01-02T15:04Z"), kind, body)
	_, err = f.WriteString(section)
	return err
}

// PruneNotes truncates cwd's notes file to the newest keepNotes "## "
// sections. Daemon-only and mutex-protected -- this is the ONLY truncating
// writer of notes.md; AppendNote never truncates. Text before the first
// heading (a malformed append) is folded into the count as one leading
// section rather than lost. The atomic rename this writes through (see
// atomicWriteFile) means a concurrent reader never sees a torn file, and a
// concurrent AppendNote from the separate `deadeye notes-append` CLI
// process never sees a torn file either -- but codemapMu is in-process
// only, so it can't order this read-modify-write against that other
// process's append. deadeye: a PruneNotes that reads its snapshot just
// before an overlapping cross-process append writes back a version missing
// that one section. ceiling: loses at most one exploration note, on a
// microseconds-wide window, self-heals on the next append. upgrade: a
// cross-process lock (flock) if this ever needs to hold more than
// best-effort notes.
func PruneNotes(cwd string) error {
	codemapMu.Lock()
	defer codemapMu.Unlock()

	path := NotesPath(cwd)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil // no notes yet -- nothing to prune
	}
	sections := splitSections(string(b))
	if len(sections) <= keepNotes {
		return nil
	}
	kept := sections[len(sections)-keepNotes:]
	return atomicWriteFile(path, []byte(strings.Join(kept, "")), 0o600)
}

// splitSections splits notes content into "## "-headed chunks, each
// retaining its heading. Leading headingless text becomes its own chunk.
func splitSections(s string) []string {
	var sections []string
	var cur strings.Builder
	for _, line := range strings.SplitAfter(s, "\n") {
		if strings.HasPrefix(line, "## ") && cur.Len() > 0 {
			sections = append(sections, cur.String())
			cur.Reset()
		}
		cur.WriteString(line)
	}
	if strings.TrimSpace(cur.String()) != "" {
		sections = append(sections, cur.String())
	}
	return sections
}

// LoadNotes returns the newest notes for cwd, head-capped at
// notesHeadLines, or "".
func LoadNotes(cwd string) string {
	b, err := os.ReadFile(NotesPath(cwd))
	if err != nil {
		return ""
	}
	sections := splitSections(string(b))
	if len(sections) == 0 {
		return ""
	}
	// Newest sections are at the END of an append-only file -- take lines
	// from the tail, not the head.
	all := strings.Split(strings.TrimSpace(strings.Join(sections, "")), "\n")
	if len(all) > notesHeadLines {
		all = all[len(all)-notesHeadLines:]
	}
	return strings.Join(all, "\n")
}
