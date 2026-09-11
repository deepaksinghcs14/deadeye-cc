// Package gitutil is the one bounded git-subprocess helper every deadeye
// call site shares. The same ten lines existed byte-identically twice
// (sessionmem's gitOutput, route.go's gitOut) before a third caller
// (codemap) made the copy a liability.
package gitutil

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Timeout bounds every git subprocess this plugin shells out to. A hung
// git (stalled network mount, held .git/index.lock) must not wedge a
// daemon goroutine -- fail open per INV-5 means timing out and returning
// "", not hanging.
const Timeout = 2 * time.Second

// Output runs git in cwd bounded by Timeout, returning trimmed stdout, or
// "" on any error, non-zero exit, or timeout.
func Output(cwd string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	return OutputCtx(ctx, cwd, args...)
}

// OutputCtx is Output under a caller-supplied deadline -- for call sites
// that must bound a SEQUENCE of git calls, not each one independently.
func OutputCtx(ctx context.Context, cwd string, args ...string) string {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ProjectKey derives a filesystem-safe project identifier from cwd: the
// git repo root's basename if available, else cwd's own basename.
func ProjectKey(cwd string) string {
	base := cwd
	if root := Output(cwd, "rev-parse", "--show-toplevel"); root != "" {
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

// SanitizeControlBytes replaces any control byte (<0x20) in git-derived text
// with "?". Two independent callers need this for two different reasons, both
// preserved here since either could regress independently: codemap's git
// listing uses `-z` to fix quote-character corruption, which as a side
// effect disables ALL of git's path quoting, so a tracked path with a
// literal control byte (a raw newline is legal in a filename on
// macOS/Linux) now flows through unescaped -- and codemap's one-row-per-line
// render has no other defense against a newline splitting one row into two.
// sessionmem's commit summary is one-item-per-line the same way, and a
// commit subject is arbitrary text where a raw newline or ANSI escape is
// just as legal, letting a single commit forge extra lines that read as
// deadeye's own guidance rather than repo data.
func SanitizeControlBytes(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 }) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 {
			b.WriteByte('?')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
