package gitutil

import (
	"strconv"
	"strings"
)

// Range is an inclusive, 1-based line range in a file.
type Range struct {
	Start int
	End   int
}

// Root resolves the repository toplevel for cwd, or "" outside a repo.
//
// Every diff-scoped git read in this package has to run from the toplevel:
// paths that come out of `git diff --name-only` are root-relative, so
// passing one as a pathspec from a subdirectory silently matches nothing
// and looks identical to "this file has no history" (the same trap
// documented at internal/signals/providers.go's git call sites).
func Root(cwd string) string {
	return Output(cwd, "rev-parse", "--show-toplevel")
}

// Head resolves HEAD's full sha in cwd, or "" outside a repo / in a repo
// with no commits yet.
func Head(cwd string) string {
	return Output(cwd, "rev-parse", "HEAD")
}

// AddedRanges parses a unified diff and returns, per file path, the
// NEW-side line ranges that diff added or changed.
//
// These are the lines a commit is responsible for -- the input to asking
// "did anything later come back and fix the lines this change
// introduced". Pure deletions (a hunk with a zero-length new side)
// produce no range: there is no resulting line for a later commit to
// touch.
func AddedRanges(unified string) map[string][]Range {
	out := map[string][]Range{}
	path := ""
	for _, line := range strings.Split(unified, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			path = parseDiffPath(strings.TrimPrefix(line, "+++ "))
		case strings.HasPrefix(line, "@@ "):
			if path == "" {
				continue
			}
			if r, ok := parseHunkNewSide(line); ok {
				out[path] = append(out[path], r)
			}
		}
	}
	return out
}

// parseDiffPath turns a unified-diff header path ("b/internal/x.go", or
// "/dev/null" for a deleted file) into a repo-relative path, or "" when
// there is no real file on that side.
func parseDiffPath(s string) string {
	// git appends a tab plus metadata for paths with spaces; take the
	// first field only when a tab is present, never splitting on spaces
	// (a path may legitimately contain one).
	if i := strings.IndexByte(s, '\t'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if s == "/dev/null" || s == "" {
		return ""
	}
	// Strip git's a/ b/ prefixes. A real path whose first segment is
	// literally "b" survives, because git always emits the prefix.
	if strings.HasPrefix(s, "b/") || strings.HasPrefix(s, "a/") {
		s = s[2:]
	}
	return s
}

// parseHunkNewSide pulls the new-side range out of a hunk header:
// "@@ -12,4 +12,6 @@ func foo()" -> {12, 17}. A count is optional and
// means 1 when omitted ("@@ -1 +1 @@"); a zero count means the hunk only
// removed lines, so there is no new-side range at all.
func parseHunkNewSide(header string) (Range, bool) {
	i := strings.IndexByte(header, '+')
	if i < 0 {
		return Range{}, false
	}
	spec := header[i+1:]
	if j := strings.IndexAny(spec, " \t"); j >= 0 {
		spec = spec[:j]
	}
	start, count := spec, "1"
	if c := strings.IndexByte(spec, ','); c >= 0 {
		start, count = spec[:c], spec[c+1:]
	}
	s, err := strconv.Atoi(start)
	if err != nil {
		return Range{}, false
	}
	n, err := strconv.Atoi(count)
	if err != nil || n <= 0 {
		return Range{}, false
	}
	return Range{Start: s, End: s + n - 1}, true
}

// Numstat returns the file/insertion/deletion counts for one revision.
// Fails open to zeroes, like every other read here -- a commit that can't
// be measured is skipped by callers, never reported as a zero-line commit.
func Numstat(root, rev string) (files, adds, dels int) {
	out := Output(root, "show", "--numstat", "--format=", rev)
	if out == "" {
		return 0, 0, 0
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		// A binary file's counts are "-", not a number: it still counts
		// as a touched file, but contributes no lines.
		files++
		if n, err := strconv.Atoi(f[0]); err == nil {
			adds += n
		}
		if n, err := strconv.Atoi(f[1]); err == nil {
			dels += n
		}
	}
	return files, adds, dels
}
