package main

import (
	"regexp"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
)

// muteRe accepts /deadeye-mute (and the /deadeye:deadeye-mute plugin
// form), with an optional "off" argument to unmute.
var muteRe = regexp.MustCompile(`^[/@$](?:deadeye:)?deadeye-mute(?:\s+(off))?\s*$`)

// muteTracker gives preprocess advisories, the soft plan gate, and the
// workflow hint a session-scoped off switch -- the same in-band control
// coder mode already has. Muting silences NAGS only: silent output
// rewrites keep saving tokens, the hard plan gate (an explicit enforce
// choice) keeps enforcing, and the coder persona is governed by its own
// /deadeye-coder command. Returns the confirmation text, or "".
func muteTracker(in hookio.Input, state *daemonState) string {
	m := muteRe.FindStringSubmatch(strings.TrimSpace(in.Prompt))
	if m == nil {
		return ""
	}
	if m[1] == "off" {
		state.setMuted(in.SessionID, false)
		state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "UserPromptSubmit", Action: "unmute"})
		return "DEADEYE UNMUTED — advisories, plan-gate nags, and workflow hints are back on."
	}
	state.setMuted(in.SessionID, true)
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "UserPromptSubmit", Action: "mute"})
	return "DEADEYE MUTED for this session — advisories, plan-gate nags, and workflow hints are off. Silent output rewrites stay on. Restore with /deadeye-mute off."
}
