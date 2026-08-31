package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

const updateCheckTTLSeconds = 24 * 60 * 60
const releaseLatestURL = "https://github.com/deepaksinghcs14/deadeye-cc/releases/latest"

// updateRefreshEnabled gates the background network refresh. Only runDaemon()
// sets it true, so unit tests -- which call the decision functions directly,
// never through the daemon -- stay fully offline.
var updateRefreshEnabled bool

// updateCache is ~/.deadeye/update-check.json: the last background lookup and
// the version we've already asked about, so the ask fires at most once per new
// release, not every session.
type updateCache struct {
	CheckedUnix int64  `json:"checked_unix"`
	Latest      string `json:"latest"`
	Asked       string `json:"asked"`
}

func loadUpdateCache() updateCache {
	var c updateCache
	if b, err := os.ReadFile(meta.UpdateCheckPath()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func saveUpdateCache(c updateCache) {
	_ = os.MkdirAll(meta.StateDir(), 0o700)
	b, _ := json.Marshal(c)
	_ = os.WriteFile(meta.UpdateCheckPath(), append(b, '\n'), 0o600)
}

// updateNudge asks the AGENT to offer an update when a newer release is out.
// The version comes from a cache refreshed in the BACKGROUND (never on the hook
// response path, same discipline as the OSV cache) -- so the first session that
// sees a new release only kicks the refresh; the ask lands the session after,
// paying zero latency. Off when mode.update_check is "off"; asks at most once
// per version (declining doesn't nag, but a still-newer release asks again).
func updateNudge(cfg config.Config, state *daemonState, sessionID string) string {
	if cfg.Mode.UpdateCheck == "off" {
		return ""
	}
	c := loadUpdateCache()
	if updateRefreshEnabled && nowUnix()-c.CheckedUnix > updateCheckTTLSeconds {
		go refreshUpdateCache() // informs the NEXT session, never this one
	}
	if c.Latest == "" || !semverNewer(c.Latest, meta.Version) || c.Asked == c.Latest {
		return ""
	}
	c.Asked = c.Latest
	saveUpdateCache(c)
	state.log(logstore.Record{TS: nowRFC3339(), SessionID: sessionID, Surface: "SessionStart", Action: "update-available"})
	return "deadeye: a newer version (" + c.Latest + ") is out -- this install is on " + meta.Version +
		". ASK the user whether they'd like to update. If yes, have them run `/plugin update` then " +
		"`/reload-plugins` (the managed binary re-syncs itself automatically afterward). Ask once, " +
		"as a brief question; if they decline, drop it for this session."
}

// refreshUpdateCache fetches the latest release tag and caches it. CheckedUnix
// is stamped regardless of outcome so a flaky network doesn't refetch every
// session -- it just tries again after the TTL.
func refreshUpdateCache() {
	c := loadUpdateCache()
	c.CheckedUnix = nowUnix()
	if v := fetchLatestVersion(); v != "" {
		c.Latest = v
	}
	saveUpdateCache(c)
}

// fetchLatestVersion reads the version from the releases/latest redirect
// Location (.../releases/tag/v0.26.0 -> "0.26.0"). No API, no JSON, no body --
// just the redirect header, with a short timeout.
func fetchLatestVersion() string {
	client := &http.Client{
		Timeout:       3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(releaseLatestURL)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if i := strings.LastIndex(loc, "/tag/"); i >= 0 {
		return strings.TrimPrefix(loc[i+len("/tag/"):], "v")
	}
	return ""
}

// semverNewer reports whether a is a strictly newer x.y.z than b. Malformed
// input returns false -- never ask on garbage.
func semverNewer(a, b string) bool {
	pa, oka := parseSemver(a)
	pb, okb := parseSemver(b)
	if !oka || !okb {
		return false
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parseSemver(s string) ([3]int, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i] // drop -dev / prerelease / build suffix
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}
