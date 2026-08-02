// Package meta holds the plugin's identity and well-known filesystem paths.
// Keeping these in one place is what lets the display name stay a single
// constant, per the plan's naming discipline: nothing the user types
// day-to-day carries the deadeye-cc repo suffix.
package meta

import (
	"os"
	"path/filepath"
)

const (
	// Name is the display name and command-line identity: "deadeye". The
	// -cc suffix exists only in surfaces that must be globally unique
	// (repo, module, install path) and is never used here.
	Name = "deadeye"

	// Version is the binary version. Overridden at build time via
	// -ldflags "-X .../meta.Version=...", goreleaser-style.
	Version = "0.2.0-dev"
)

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

func SocketPath() string          { return filepath.Join(StateDir(), "deadeye.sock") }
func LockPath() string            { return filepath.Join(StateDir(), "deadeye.lock") }
func LogPath() string             { return filepath.Join(StateDir(), "decisions.jsonl") }
func OutcomesPath() string        { return filepath.Join(StateDir(), "outcomes.jsonl") }
func CapturesDir() string         { return filepath.Join(StateDir(), "captures") }
func ConfigPath() string          { return filepath.Join(StateDir(), "config.json") }
func CatalogOverridePath() string { return filepath.Join(StateDir(), "catalog.json") }
