// Package proto is the wire protocol between the deadeye CLI (one process
// per hook call) and the long-lived daemon over the unix socket. This is
// our own protocol, not Claude Code's hook contract -- see internal/hookio
// for that.
package proto

import "encoding/json"

// Request is written by the client, then the connection is half-closed
// (CloseWrite) to signal end of message; the daemon reads to EOF, decides,
// and writes the raw hookio.Output JSON bytes back before closing.
type Request struct {
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`

	// Off carries the client's env-derived kill switches (see
	// config.OffSwitches) across the wire. The daemon is a long-lived
	// process that may have been spawned minutes or days earlier from a
	// different shell -- it must never read os.Getenv for these itself,
	// only ever trust what the current client's real environment reports.
	Off []string `json:"off,omitempty"`

	// PluginRoot is the client's CLAUDE_PLUGIN_ROOT, same doctrine as Off:
	// the env belongs to the client process, never the daemon. Used only
	// by the statusline nudge to compose the suggested command path.
	PluginRoot string `json:"plugin_root,omitempty"`

	// ConfigDir is the client's CLAUDE_CONFIG_DIR (usually empty --
	// ~/.claude). Same doctrine as Off. Used only by the statusline nudge
	// to find the user's settings.json.
	ConfigDir string `json:"config_dir,omitempty"`

	// ClientVersion is the Claude Code version serving this hook, parsed
	// client-side from CLAUDE_CODE_EXECPATH (.../versions/2.1.220 --
	// verified present in real hook environments). "" when the layout is
	// unrecognized; consumers must fail open on "".
	ClientVersion string `json:"client_version,omitempty"`
}
