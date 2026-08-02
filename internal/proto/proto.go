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
}
