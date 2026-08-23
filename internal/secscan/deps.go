package secscan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Dep is one dependency line pulled from a manifest's added text.
type Dep struct {
	Ecosystem string // OSV ecosystem name: npm, Go, PyPI, crates.io, Maven
	Name      string
	Version   string
}

// manifestExtractors maps a manifest basename to the function that pulls
// Dep rows out of its ADDED lines -- never the whole manifest. One line,
// one dependency; a Gradle multi-line block or a pom.xml with its tags on
// separate un-adjacent lines is out of scope for this pass, deliberately:
// /deadeye-guard's native auditors read the whole file and don't share
// this limitation.
var manifestExtractors = map[string]func(string) []Dep{
	"package.json":     extractNPM,
	"go.mod":           extractGoMod,
	"requirements.txt": extractRequirementsTxt,
	"pyproject.toml":   extractPyprojectToml,
	"Cargo.toml":       extractCargoToml,
	"pom.xml":          extractPomXML,
	"build.gradle":     extractGradle,
	"build.gradle.kts": extractGradle,
}

// IsManifest reports whether path's basename is one ExtractDeps understands.
func IsManifest(path string) bool {
	_, ok := manifestExtractors[filepath.Base(path)]
	return ok
}

// ManifestFilenames returns the manifest basenames ExtractDeps understands
// -- for the session-start scan (cmd/deadeye/depscan.go) that looks for
// existing manifests to flag, rather than reacting to an edit.
func ManifestFilenames() []string {
	names := make([]string, 0, len(manifestExtractors))
	for n := range manifestExtractors {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ExtractDeps parses name+version pairs out of path's added text.
func ExtractDeps(path, added string) []Dep {
	if f, ok := manifestExtractors[filepath.Base(path)]; ok {
		return f(added)
	}
	return nil
}

// deadeye: range specifiers resolved to their base version, not the lockfile.
// ceiling: a ^-range that already resolves past the fix reads as vulnerable.
// upgrade: read the lockfile when false positives show.
var rangePrefixRe = regexp.MustCompile(`^[\^~<>=\s]+`)

func stripRange(v string) string {
	v = rangePrefixRe.ReplaceAllString(v, "")
	if i := strings.IndexAny(v, " ,"); i >= 0 {
		v = v[:i]
	}
	return v
}

// npmDepLineRe matches a JSON `"name": "version"` line; npmNonDepKeys
// excludes package.json's own top-level metadata fields, which have the
// same shape but aren't dependencies.
var npmDepLineRe = regexp.MustCompile(`^\s*"([^"]+)"\s*:\s*"([~^]?[0-9][^"]*)"\s*,?\s*$`)
var npmNonDepKeys = map[string]bool{
	"name": true, "version": true, "description": true, "main": true,
	"license": true, "private": true, "author": true, "homepage": true,
	"repository": true, "type": true, "module": true, "types": true,
	"packageManager": true,
}

func extractNPM(added string) []Dep {
	var out []Dep
	for _, line := range strings.Split(added, "\n") {
		m := npmDepLineRe.FindStringSubmatch(line)
		if m == nil || npmNonDepKeys[m[1]] {
			continue
		}
		out = append(out, Dep{Ecosystem: "npm", Name: m[1], Version: stripRange(m[2])})
	}
	return out
}

// goModDepRe requires a dotted module path (github.com/x/y, golang.org/x/y)
// followed by a v-prefixed version -- excludes the `module` and `go`
// directive lines, which don't have that shape. The optional leading
// "require " covers gofmt's single-dependency form (`require
// github.com/pkg/errors v0.9.1`, no parens) -- without it this only
// matched the indented lines inside a `require (...)` block, so a manifest
// with exactly one dependency (what `go get` leaves on a fresh module)
// bypassed dep scanning entirely.
var goModDepRe = regexp.MustCompile(`^\s*(?:require\s+)?([\w.\-]+(?:/[\w.\-]+)+)\s+v([0-9][\w.+-]*)`)

func extractGoMod(added string) []Dep {
	var out []Dep
	for _, line := range strings.Split(added, "\n") {
		m := goModDepRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out = append(out, Dep{Ecosystem: "Go", Name: m[1], Version: m[2]})
	}
	return out
}

var requirementsLineRe = regexp.MustCompile(`^\s*([A-Za-z0-9_.\-]+)\s*==\s*([0-9][\w.]*)`)

func extractRequirementsTxt(added string) []Dep {
	var out []Dep
	for _, line := range strings.Split(added, "\n") {
		m := requirementsLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out = append(out, Dep{Ecosystem: "PyPI", Name: m[1], Version: m[2]})
	}
	return out
}

// pyprojectDepLineRe matches poetry/pdm-style `name = "^1.2.3"` lines --
// same shape risk as package.json, so the same non-dep-key filter applies.
var pyprojectDepLineRe = regexp.MustCompile(`^\s*([A-Za-z0-9_.\-]+)\s*=\s*"([~^]?[0-9][\w.]*)"`)
var pyprojectNonDepKeys = map[string]bool{
	"name": true, "version": true, "description": true, "authors": true,
	"license": true, "readme": true, "homepage": true, "repository": true,
	"python": true,
}

func extractPyprojectToml(added string) []Dep {
	var out []Dep
	for _, line := range strings.Split(added, "\n") {
		m := pyprojectDepLineRe.FindStringSubmatch(line)
		if m == nil || pyprojectNonDepKeys[m[1]] {
			continue
		}
		out = append(out, Dep{Ecosystem: "PyPI", Name: m[1], Version: stripRange(m[2])})
	}
	return out
}

var cargoDepLineRe = regexp.MustCompile(`^\s*([A-Za-z0-9_\-]+)\s*=\s*"([~^]?[0-9][\w.]*)"`)
var cargoNonDepKeys = map[string]bool{
	"name": true, "version": true, "edition": true, "description": true,
	"license": true, "authors": true, "repository": true, "readme": true,
}

func extractCargoToml(added string) []Dep {
	var out []Dep
	for _, line := range strings.Split(added, "\n") {
		m := cargoDepLineRe.FindStringSubmatch(line)
		if m == nil || cargoNonDepKeys[m[1]] {
			continue
		}
		out = append(out, Dep{Ecosystem: "crates.io", Name: m[1], Version: stripRange(m[2])})
	}
	return out
}

// pomDepRe requires groupId, artifactId and version as consecutive tags
// (whitespace/newlines between) -- Maven's OSV package name is
// "groupId:artifactId".
var pomDepRe = regexp.MustCompile(`(?s)<groupId>([\w.\-]+)</groupId>\s*<artifactId>([\w.\-]+)</artifactId>\s*<version>([\w.\-]+)</version>`)

func extractPomXML(added string) []Dep {
	var out []Dep
	for _, m := range pomDepRe.FindAllStringSubmatch(added, -1) {
		out = append(out, Dep{Ecosystem: "Maven", Name: m[1] + ":" + m[2], Version: m[3]})
	}
	return out
}

// gradleDepRe matches the common single-line `implementation 'g:a:1.0'`
// (or api/compile/testImplementation, quotes either style) shape.
var gradleDepRe = regexp.MustCompile(`(?:implementation|api|compile|testImplementation)\s*\(?['"]([\w.\-]+):([\w.\-]+):([\w.+\-]+)['"]`)

func extractGradle(added string) []Dep {
	var out []Dep
	for _, m := range gradleDepRe.FindAllStringSubmatch(added, -1) {
		out = append(out, Dep{Ecosystem: "Maven", Name: m[1] + ":" + m[2], Version: m[3]})
	}
	return out
}

// Superseded is one entry in the bundled "the platform absorbed this"
// table -- rungs 3-4 of the ladder expressed as data, not a CVE feed. It
// stays useful with the network off, and it's the advice deadeye is
// uniquely positioned to give (an OSV feed only knows what's vulnerable,
// not what's simply redundant). Keep this list short and defensible: a
// long list rots, a dozen entries everyone agrees on does not.
type Superseded struct {
	Ecosystem   string
	Name        string
	Replacement string
}

var SupersededDeps = []Superseded{
	{"npm", "request", "fetch (built into Node 18+); request is unmaintained"},
	{"npm", "moment", "Temporal or date-fns; moment is legacy-only"},
	{"npm", "node-uuid", "crypto.randomUUID()"},
	{"npm", "left-pad", "String.prototype.padStart()"},
	{"PyPI", "pycrypto", "cryptography; pycrypto is abandoned and has known CVEs"},
}

func supersededReplacement(ecosystem, name string) string {
	for _, s := range SupersededDeps {
		if s.Ecosystem == ecosystem && strings.EqualFold(s.Name, name) {
			return s.Replacement
		}
	}
	return ""
}

// OSVCache mirrors ~/.deadeye/osv-cache.json's shape -- written by the
// daemon's background refresh, read-only here. Entries hold a short,
// pre-formatted advisory string ready to append to a finding, keyed by
// "ecosystem:name@version" -- the cache stores exactly what the hook needs
// to say, not the raw OSV response, so a stale schema on either side
// degrades to "no entry" rather than a parse error.
type OSVCache struct {
	FetchedUnix int64               `json:"fetched_unix"`
	Entries     map[string][]string `json:"entries"`
}

const osvCacheTTLSeconds = 24 * 60 * 60

// LoadOSVCache reads path and returns it, or a zero-value cache on any
// read/parse error or once past its TTL -- fail open, same as every other
// advisory surface (INV-5): a missing, stale, or malformed cache produces
// zero findings, never an error. nowUnix is a parameter rather than
// time.Now() so this stays deterministically testable.
func LoadOSVCache(path string, nowUnix int64) OSVCache {
	b, err := os.ReadFile(path)
	if err != nil {
		return OSVCache{}
	}
	var c OSVCache
	if json.Unmarshal(b, &c) != nil {
		return OSVCache{}
	}
	if nowUnix-c.FetchedUnix > osvCacheTTLSeconds {
		return OSVCache{}
	}
	return c
}

func (c OSVCache) lookup(eco, name, version string) []string {
	if c.Entries == nil {
		return nil
	}
	return c.Entries[eco+":"+name+"@"+version]
}

// Known reports whether eco/name/version has ever been checked against
// OSV, vulnerable or not -- distinct from lookup returning an empty slice,
// so a caller can skip re-querying a known-clean dependency within the
// cache's TTL instead of hitting the network on every edit.
func (c OSVCache) Known(eco, name, version string) bool {
	if c.Entries == nil {
		return false
	}
	_, ok := c.Entries[eco+":"+name+"@"+version]
	return ok
}

// ScanDeps checks path's added dependency lines against the OSV cache
// first (a specific, versioned advisory), then the bundled superseded
// table (a general "there's a better option") -- cache and network are
// consulted at every layer above this one; ScanDeps itself never touches
// either.
func ScanDeps(path, added string, cache OSVCache, disabled map[string]bool) []Finding {
	if disabled["dep-check"] {
		return nil
	}
	var out []Finding
	for _, d := range ExtractDeps(path, added) {
		name := "dep:" + d.Ecosystem + ":" + d.Name
		if disabled[name] {
			continue
		}
		if advisories := cache.lookup(d.Ecosystem, d.Name, d.Version); len(advisories) > 0 {
			// Name the package+version like the superseded branch does, so
			// both the advisory line and an ask-mode permission prompt say
			// WHAT is vulnerable, not just the advisory id.
			out = append(out, Finding{Rule: name, Advice: d.Name + " " + d.Version + " -- " + advisories[0], Vuln: true})
			continue
		}
		if repl := supersededReplacement(d.Ecosystem, d.Name); repl != "" {
			out = append(out, Finding{Rule: name, Advice: d.Name + " " + d.Version + " -- " + repl})
		}
	}
	return out
}
