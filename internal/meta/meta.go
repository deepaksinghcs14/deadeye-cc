// Package meta holds the plugin's identity and well-known filesystem paths.
// Keeping these in one place is what lets the display name stay a single
// constant, per the plan's naming discipline: nothing the user types
// day-to-day carries the deadeye-cc repo suffix.
package meta

import (
	"fmt"
	"os"
	"path/filepath"
)

// Name is the display name and command-line identity: "deadeye". The -cc
// suffix exists only in surfaces that must be globally unique (repo,
// module, install path) and is never used here.
const Name = "deadeye"

// Version is the binary version. Overridden at build time via
// -ldflags "-X .../meta.Version=...", goreleaser-style -- deliberately a
// var, not a const: `go build -ldflags -X` can only patch a package-level
// string variable. It silently no-ops on a const, which is exactly what
// happened to the v0.1.0/v0.2.0 releases -- `deadeye version` reported the
// compiled-in dev string instead of the real tag on both, caught only by
// checking the actual downloaded release binary's output, not by reading
// this file.
var Version = "0.24.0-dev"

// StateDir returns ~/.deadeye, creating no directories itself.
func StateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fail open to a relative path rather than erroring; every caller
		// treats a missing state dir as "nothing persisted yet", not fatal.
		home = "."
	}
	return filepath.Join(home, ".deadeye")
}

// SocketPath is normally $HOME/.deadeye/deadeye.sock. Unix sockaddr paths
// cap around 104 bytes (macOS; 108 Linux) and net.Listen fails with
// "invalid argument" past it -- a long corporate $HOME would otherwise
// silently defeat the daemon and re-spawn a doomed process on every hook
// call. The fallback is deterministic (uid-keyed under TempDir), so every
// client and daemon on the machine resolves the same path.
func SocketPath() string {
	p := filepath.Join(StateDir(), "deadeye.sock")
	if len(p) <= 100 {
		return p
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("deadeye-%d.sock", os.Getuid()))
}
func CoderModePath() string { return filepath.Join(StateDir(), "coder-mode") }

// CoderModePathFor is the per-session statusline mirror -- the statusline
// script picks it via the session_id in its stdin JSON, falling back to
// the global CoderModePath. The id is sanitized so a hostile payload
// can't traverse out of the state dir.
func CoderModePathFor(sessionID string) string {
	safe := make([]rune, 0, len(sessionID))
	for _, r := range sessionID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			safe = append(safe, r)
		default:
			safe = append(safe, '_')
		}
	}
	return filepath.Join(StateDir(), "coder-mode."+string(safe))
}
func StatuslineNudgedPath() string { return filepath.Join(StateDir(), "statusline-nudged") }
func LockPath() string             { return filepath.Join(StateDir(), "deadeye.lock") }
func LogPath() string              { return filepath.Join(StateDir(), "decisions.jsonl") }
func OutcomesPath() string         { return filepath.Join(StateDir(), "outcomes.jsonl") }
func CapturesDir() string          { return filepath.Join(StateDir(), "captures") }
func ConfigPath() string           { return filepath.Join(StateDir(), "config.json") }
func CatalogOverridePath() string  { return filepath.Join(StateDir(), "catalog.json") }
func OSVCachePath() string         { return filepath.Join(StateDir(), "osv-cache.json") }
