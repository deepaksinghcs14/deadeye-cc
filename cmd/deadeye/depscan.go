package main

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
	"github.com/deepaksinghcs14/deadeye-cc/internal/secscan"
)

// decideDependencyFlag scans the project's EXISTING manifests once per
// session and returns a one-line summary if any current dependency carries
// a known advisory (or a superseded-package suggestion). This is the flag
// half of "vulnerable deps": deadeye can't hard-stop a library already in
// the tree at a hook -- that's CI's job (/deadeye-guard's native auditors)
// -- but it can surface it so it isn't invisible. Reuses ScanDeps on
// whole-file content; the local OSV cache answers inline (no network on
// this path) and cache-miss deps feed the background refresh for next
// session. Reads DECLARED manifest versions, not resolved lockfiles (no
// lockfile parser exists), so it's a floor, stated in the line's framing.
func decideDependencyFlag(cwd string, cfg config.Config, state *daemonState) (string, bool) {
	if cwd == "" || cfg.Coder.Disabled || cfg.Coder.Security == "off" || cfg.DisabledRuleSet()["dep-flag"] {
		return "", false
	}

	var cache secscan.OSVCache
	if cfg.Coder.SecurityOSVEnabled() {
		cache = secscan.LoadOSVCache(meta.OSVCachePath(), nowUnix())
	}
	disabled := cfg.DisabledRuleSet()

	seen := map[string]bool{} // dedupe package names across manifests
	var flagged []string
	var missingOSV []secscan.Dep
	for _, name := range secscan.ManifestFilenames() {
		path := filepath.Join(cwd, name)
		content, err := os.ReadFile(path)
		if err != nil {
			continue // manifest not present here
		}
		body := string(content)
		for _, f := range secscan.ScanDeps(path, body, cache, disabled) {
			pkg := depPackageName(f.Rule)
			if pkg == "" || seen[pkg] {
				continue
			}
			seen[pkg] = true
			flagged = append(flagged, pkg)
		}
		if cfg.Coder.SecurityOSVEnabled() {
			for _, d := range secscan.ExtractDeps(path, body) {
				if !cache.Known(d.Ecosystem, d.Name, d.Version) {
					missingOSV = append(missingOSV, d)
				}
			}
		}
	}

	if len(missingOSV) > 0 {
		triggerOSVRefresh(missingOSV)
	}
	if len(flagged) == 0 {
		return "", false
	}
	sort.Strings(flagged)

	n := strconv.Itoa(len(flagged))
	state.log(logstore.Record{
		TS: nowRFC3339(), Surface: "UserPromptSubmit", Action: "dep-flag",
		Reason: n + " advisories", // count only -- the package list is not logged
	})
	return "deadeye: " + n + " of this project's current dependencies have known advisories (" +
		strings.Join(flagged, ", ") + ") -- run /deadeye-guard for the full audit with your native auditor.", true
}

// depPackageName pulls the bare package name out of a "dep:<eco>:<name>"
// rule id.
func depPackageName(rule string) string {
	const prefix = "dep:"
	if !strings.HasPrefix(rule, prefix) {
		return ""
	}
	rest := rule[len(prefix):]
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		return rest[i+1:]
	}
	return ""
}
