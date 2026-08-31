package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// pluginVersion reads the version from <pluginRoot>/.claude-plugin/plugin.json
// -- the version the marketplace installed. "" if it can't be read. Comparing
// it to the binary's own meta.Version is the only way to catch a stale binary:
// deadeye's daemon<->client handshake only compares the binary to itself.
func pluginVersion(pluginRoot string) string {
	if pluginRoot == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(pluginRoot, ".claude-plugin", "plugin.json"))
	if err != nil {
		return ""
	}
	var m struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	return m.Version
}

// skewNudge warns, once per plugin version, when the running binary is STRICTLY
// BEHIND the installed plugin -- the silent failure where a `deadeye` on PATH
// shadows the self-updating managed binary (~/.deadeye/bin/deadeye), so plugin
// updates never reach it. A binary AHEAD of the plugin is a dev checkout, not a
// problem, so it stays quiet.
func skewNudge(pluginRoot string, state *daemonState, sessionID string) string {
	pv := pluginVersion(pluginRoot)
	if pv == "" || !semverNewer(pv, meta.Version) {
		return ""
	}
	if b, err := os.ReadFile(meta.SkewWarnedPath()); err == nil && strings.TrimSpace(string(b)) == pv {
		return "" // already warned about this exact skew
	}
	_ = os.MkdirAll(meta.StateDir(), 0o700)
	_ = os.WriteFile(meta.SkewWarnedPath(), []byte(pv+"\n"), 0o600)
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: sessionID, Surface: "SessionStart", Action: "binary-skew"})
	return "deadeye: the running binary is " + meta.Version + " but the installed plugin is " + pv +
		". A `deadeye` on the user's PATH is almost certainly shadowing the managed, self-updating binary " +
		"(~/.deadeye/bin/deadeye), so plugin updates never reach it. Tell the user (once): run `which deadeye`, " +
		"then either update it (`go install github.com/deepaksinghcs14/deadeye-cc/cmd/deadeye@latest`) or remove " +
		"it (`rm \"$(which deadeye)\"`) so the managed binary takes over. This matters -- a stale binary runs old " +
		"logic against a newer plugin."
}
