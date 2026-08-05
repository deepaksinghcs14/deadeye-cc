// Package codemap is the structural half of PLAN.md §5.7's re-orientation
// tax: sessionmem captures what recently CHANGED, this captures what the
// project IS. One stable skeleton file per project, regenerated only when
// the tracked-file list's fingerprint moves, so a session returning to an
// unchanged repo gets its bearings without burning a Glob sweep -- plus a
// touch-frequency counter (touch.go) and an exploration-notes log
// (notes.go) that enrich it across sessions.
package codemap

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/gitutil"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

const (
	// maxDirs caps the skeleton at the directories that actually carry the
	// project. A 4000-file monorepo must not turn a once-per-session
	// orientation paragraph into a directory dump -- cap explicitly, same
	// discipline as vuln.go's vulnFindingCap.
	maxDirs      = 20
	maxPurpose   = 90  // chars of a package doc line kept
	docScanBytes = 512 // bytes read from a .go file looking for its doc comment
	maxDocFiles  = 3   // .go files probed per directory before giving up
)

// codemapMu guards every truncating/read-modify-write file operation in
// this package (Regenerate, MergeTouched, PruneNotes). The daemon handles
// each hook connection in its own goroutine, so two sessions in the SAME
// project can hit SessionEnd concurrently -- same reasoning as
// logstore.Store's mutex. AppendNote deliberately does NOT take it: an
// O_APPEND write is process-safe on its own, and it must stay callable
// from the short-lived `deadeye notes-append` CLI process.
var codemapMu sync.Mutex

// Dir returns ~/.deadeye/map, creating nothing.
func Dir() string { return filepath.Join(meta.StateDir(), "map") }

// MapPath is the per-project skeleton file.
func MapPath(cwd string) string {
	return filepath.Join(Dir(), gitutil.ProjectKey(cwd)+".map.md")
}

// Entry is one directory row of the skeleton.
type Entry struct {
	Dir     string // repo-relative with trailing slash; "(root)" for top-level files
	Files   int
	Ext     string // dominant extension incl. dot; "" when no clear majority
	Purpose string // Go package/command doc one-liner; "" when absent
}

// Fingerprint hashes the raw tracked-file listing (git ls-files output is
// already sorted, so the bytes hash directly). First 12 hex chars.
func Fingerprint(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])[:12]
}

// groupKey buckets one repo-relative path into its skeleton row: top-level
// files go to "(root)", a file directly under a first-level directory goes
// to that directory, and a deeper file goes to its SECOND-level directory
// when the first level holds no files of its own (a pure container like
// internal/ or src/), else to the first level. containerDirs is the set of
// first-level dirs with zero direct files, precomputed by Group.
func groupKey(path string, containerDirs map[string]bool) string {
	parts := strings.Split(path, "/")
	switch {
	case len(parts) == 1:
		return "(root)"
	case len(parts) == 2 || !containerDirs[parts[0]]:
		return parts[0] + "/"
	default:
		return parts[0] + "/" + parts[1] + "/"
	}
}

// Group buckets repo-relative tracked paths into skeleton rows: one level
// for most directories, one level DEEPER for pure containers -- a
// directory holding no files of its own. No hardcoded name list: the
// zero-direct-files rule is language-agnostic and self-tuning. Pure
// function, no filesystem, no git -- the whole grouping rule is testable
// from a []string. Rows are sorted by file count desc then path asc, and
// capped at maxDirs.
func Group(paths []string) []Entry {
	// Pass 1: which first-level directories hold files of their own?
	directFiles := map[string]bool{}
	firstLevel := map[string]bool{}
	for _, p := range paths {
		if p == "" {
			continue
		}
		parts := strings.Split(p, "/")
		if len(parts) >= 2 {
			firstLevel[parts[0]] = true
		}
		if len(parts) == 2 {
			directFiles[parts[0]] = true
		}
	}
	containerDirs := map[string]bool{}
	for d := range firstLevel {
		if !directFiles[d] {
			containerDirs[d] = true
		}
	}

	// Pass 2: bucket every path, counting files and extensions per row.
	counts := map[string]int{}
	exts := map[string]map[string]int{}
	for _, p := range paths {
		if p == "" {
			continue
		}
		key := groupKey(p, containerDirs)
		counts[key]++
		if e := strings.ToLower(filepath.Ext(p)); e != "" {
			if exts[key] == nil {
				exts[key] = map[string]int{}
			}
			exts[key][e]++
		}
	}

	entries := make([]Entry, 0, len(counts))
	for dir, n := range counts {
		entries = append(entries, Entry{Dir: dir, Files: n, Ext: dominantExt(exts[dir], n)})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Files != entries[j].Files {
			return entries[i].Files > entries[j].Files
		}
		return entries[i].Dir < entries[j].Dir
	})
	if len(entries) > maxDirs {
		entries = entries[:maxDirs]
	}
	return entries
}

// dominantExt returns the extension carried by a strict majority of the
// row's files, or "" -- a true tie or a grab-bag directory gets no label
// rather than a coin flip.
func dominantExt(byExt map[string]int, total int) string {
	best, bestN, tie := "", 0, false
	for e, n := range byExt {
		switch {
		case n > bestN:
			best, bestN, tie = e, n, false
		case n == bestN:
			tie = true
		}
	}
	if tie || bestN*2 <= total {
		return ""
	}
	return best
}

// docCommentRe-free extraction: match the first "// Package X ..." or
// "// Command X ..." line in a file's head. Regexes are overkill for a
// two-prefix check on a line's start.
func leadingDocComment(head []byte) string {
	for _, line := range strings.Split(string(head), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "//") {
			// First non-comment, non-blank line ends the file header --
			// build tags and blank lines may precede the doc comment.
			if trimmed != "" && !strings.HasPrefix(trimmed, "//go:") {
				return ""
			}
			continue
		}
		text := strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))
		rest, ok := strings.CutPrefix(text, "Package ")
		if !ok {
			rest, ok = strings.CutPrefix(text, "Command ")
		}
		if !ok {
			continue
		}
		// Drop the package/command NAME token; keep the sentence after it.
		if _, after, found := strings.Cut(rest, " "); found {
			rest = after
		}
		rest = strings.TrimPrefix(rest, "is ")
		if i := strings.IndexAny(rest, ".;"); i > 0 {
			rest = rest[:i]
		}
		if len(rest) > maxPurpose {
			rest = rest[:maxPurpose]
		}
		return strings.TrimSpace(rest)
	}
	return ""
}

// EnrichGo fills Purpose from each directory's leading "// Package X ..."
// or "// Command X ..." doc comment (both shapes are live in this repo:
// cmd/deadeye/main.go uses Command, every internal package uses Package).
// Call only when go.mod sits at repoRoot -- the caller gates that.
func EnrichGo(repoRoot string, entries []Entry) []Entry {
	for i, e := range entries {
		if e.Ext != ".go" || e.Dir == "(root)" {
			continue
		}
		dirPath := filepath.Join(repoRoot, filepath.FromSlash(strings.TrimSuffix(e.Dir, "/")))
		files, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		probed := 0
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".go") || strings.HasSuffix(f.Name(), "_test.go") {
				continue
			}
			if probed++; probed > maxDocFiles {
				break
			}
			head := make([]byte, docScanBytes)
			fh, err := os.Open(filepath.Join(dirPath, f.Name()))
			if err != nil {
				continue
			}
			n, _ := fh.Read(head)
			fh.Close()
			if purpose := leadingDocComment(head[:n]); purpose != "" {
				entries[i].Purpose = purpose
				break
			}
		}
	}
	return entries
}

// Render produces the whole skeleton file body, header included.
func Render(projectKey, fingerprint string, totalFiles int, entries []Entry, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Codebase map: %s\n", projectKey)
	fmt.Fprintf(&b, "fingerprint: %s\n", fingerprint)
	fmt.Fprintf(&b, "generated: %s\n", now.UTC().Format("2006-01-02T15:04Z"))
	fmt.Fprintf(&b, "scope: %d git-tracked files (untracked/ignored files are not listed)\n\n", totalFiles)
	for _, e := range entries {
		fmt.Fprintf(&b, "%-22s %3d files  %-5s %s\n", e.Dir, e.Files, e.Ext, e.Purpose)
	}
	return b.String()
}

// headFingerprint reads the "fingerprint: " header line from an existing
// skeleton file, or "".
func headFingerprint(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for i := 0; sc.Scan() && i < 3; i++ {
		if fp, ok := strings.CutPrefix(sc.Text(), "fingerprint: "); ok {
			return fp
		}
	}
	return ""
}

// lsFilesTimeout bounds the one git call here that scales with repo size.
// More generous than gitutil.Timeout because a huge monorepo is exactly
// where ls-files is slowest AND the map most valuable -- but still
// bounded: a stalled mount fails open to "no map this session" (INV-5),
// never a hung daemon goroutine.
const lsFilesTimeout = 5 * time.Second

// lsFiles streams `git ls-files` at repoRoot, returning the path list, the
// raw-bytes fingerprint, and the count -- streamed through a scanner so a
// half-million-file monorepo never buffers tens of MB into the long-lived
// daemon (the accumulated []string of paths is bounded interest; the raw
// concatenated output is not retained).
func lsFiles(repoRoot string) (paths []string, fingerprint string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), lsFilesTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "ls-files")
	cmd.Dir = repoRoot
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", err
	}
	if err := cmd.Start(); err != nil {
		return nil, "", err
	}
	h := sha256.New()
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		h.Write([]byte(line))
		h.Write([]byte{'\n'})
		paths = append(paths, line)
	}
	if err := cmd.Wait(); err != nil {
		return nil, "", err
	}
	if err := sc.Err(); err != nil {
		return nil, "", err
	}
	return paths, hex.EncodeToString(h.Sum(nil))[:12], nil
}

// Regenerate rewrites cwd's skeleton when the tracked-file fingerprint
// moved. No-op returning nil when unchanged, when cwd is not a git repo,
// or when git fails -- INV-5, a missing map is never an error. The
// ls-files subprocess itself is bounded by the git binary being local and
// the daemon's SessionEnd path being off any hook's hot path.
func Regenerate(cwd string) error {
	// ls-files with no pathspec is scoped to CWD, not the repo: resolve the
	// root FIRST or a session started in a subdirectory silently maps only
	// that subtree, with cwd-relative paths. Same trap route.go's newScope
	// documents for git log pathspecs.
	root := gitutil.Output(cwd, "rev-parse", "--show-toplevel")
	if root == "" {
		return nil
	}
	paths, fp, err := lsFiles(root)
	if err != nil || len(paths) == 0 {
		return nil
	}

	codemapMu.Lock()
	defer codemapMu.Unlock()

	mapPath := MapPath(cwd)
	if headFingerprint(mapPath) == fp {
		return nil // structure unchanged -- same discipline as sessionmem's "skip when nothing changed"
	}
	entries := Group(paths)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		entries = EnrichGo(root, entries)
	}
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	body := Render(gitutil.ProjectKey(cwd), fp, len(paths), entries, time.Now())
	return os.WriteFile(mapPath, []byte(body), 0o600)
}

// Load returns the injectable skeleton body for cwd (header's fingerprint
// line stripped -- it's cache metadata, not orientation), or "".
func Load(cwd string) string {
	b, err := os.ReadFile(MapPath(cwd))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.HasPrefix(l, "fingerprint: ") || strings.HasPrefix(l, "generated: ") {
			continue
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// Text is the full injection paragraph for cwd: skeleton, then the ranked
// touch-frequency list, then recent exploration notes -- each section
// omitted when empty. "" when there is nothing at all to say.
func Text(cwd string) string {
	var parts []string
	if s := Load(cwd); s != "" {
		parts = append(parts, s)
	}
	if t := renderTouch(cwd); t != "" {
		parts = append(parts, t)
	}
	if n := LoadNotes(cwd); n != "" {
		parts = append(parts, "Recent exploration notes:\n"+n)
	}
	return strings.Join(parts, "\n\n")
}
