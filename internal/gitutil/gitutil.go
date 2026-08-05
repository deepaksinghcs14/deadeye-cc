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
