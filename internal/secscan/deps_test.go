package secscan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractDeps(t *testing.T) {
	cases := []struct {
		name string
		path string
		body string
		want Dep
	}{
		{"npm", "package.json", `    "lodash": "^4.17.20",`, Dep{"npm", "lodash", "4.17.20"}},
		{"npm-non-dep-key-skipped", "package.json", `    "name": "my-app",`, Dep{}},
		{"go-mod", "go.mod", `	github.com/x/y v1.2.3`, Dep{"Go", "github.com/x/y", "1.2.3"}},
		{"go-mod-single-line-require", "go.mod", `require github.com/pkg/errors v0.9.1`, Dep{"Go", "github.com/pkg/errors", "0.9.1"}},
		{"go-mod-module-directive-excluded", "go.mod", `module github.com/x/y`, Dep{}},
		{"requirements-txt", "requirements.txt", `requests==2.25.0`, Dep{"PyPI", "requests", "2.25.0"}},
		{"pyproject", "pyproject.toml", `requests = "^2.25.0"`, Dep{"PyPI", "requests", "2.25.0"}},
		{"cargo", "Cargo.toml", `serde = "1.0.100"`, Dep{"crates.io", "serde", "1.0.100"}},
		{"gradle", "build.gradle", `implementation 'org.example:foo:1.0'`, Dep{"Maven", "org.example:foo", "1.0"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExtractDeps(c.path, c.body)
			if c.want == (Dep{}) {
				if len(got) != 0 {
					t.Errorf("ExtractDeps(%q, %q) = %v, want none", c.path, c.body, got)
				}
				return
			}
			if len(got) != 1 || got[0] != c.want {
				t.Errorf("ExtractDeps(%q, %q) = %v, want [%v]", c.path, c.body, got, c.want)
			}
		})
	}
}

func TestExtractDepsPomXML(t *testing.T) {
	body := `<dependency>
  <groupId>org.example</groupId>
  <artifactId>foo</artifactId>
  <version>1.0</version>
</dependency>`
	got := ExtractDeps("pom.xml", body)
	want := Dep{"Maven", "org.example:foo", "1.0"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %v, want [%v]", got, want)
	}
}

func TestIsManifest(t *testing.T) {
	for _, p := range []string{"package.json", "go.mod", "requirements.txt", "pyproject.toml", "Cargo.toml", "pom.xml", "build.gradle", "build.gradle.kts"} {
		if !IsManifest(p) {
			t.Errorf("IsManifest(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"main.go", "README.md", "package-lock.json"} {
		if IsManifest(p) {
			t.Errorf("IsManifest(%q) = true, want false", p)
		}
	}
}

func TestStripRange(t *testing.T) {
	cases := map[string]string{
		"^4.17.20":    "4.17.20",
		"~2.25.0":     "2.25.0",
		">=1.0.0":     "1.0.0",
		"1.0.0":       "1.0.0",
		">=1.0, <2.0": "1.0",
	}
	for in, want := range cases {
		if got := stripRange(in); got != want {
			t.Errorf("stripRange(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSupersededReplacement(t *testing.T) {
	if r := supersededReplacement("npm", "moment"); r == "" {
		t.Error("moment should have a bundled replacement")
	}
	if r := supersededReplacement("npm", "moment-not-real"); r != "" {
		t.Errorf("unexpected replacement for a nonexistent package: %q", r)
	}
}

func writeOSVCache(t *testing.T, dir string, fetchedUnix int64, entries map[string][]string) string {
	t.Helper()
	path := filepath.Join(dir, "osv-cache.json")
	b, err := json.Marshal(OSVCache{FetchedUnix: fetchedUnix, Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadOSVCacheMissing(t *testing.T) {
	c := LoadOSVCache(filepath.Join(t.TempDir(), "nope.json"), 1000)
	if len(c.Entries) != 0 {
		t.Errorf("missing cache should be empty, got %v", c)
	}
}

func TestLoadOSVCacheStale(t *testing.T) {
	dir := t.TempDir()
	path := writeOSVCache(t, dir, 1000, map[string][]string{"npm:lodash@4.17.20": {"vulnerable"}})
	// now is 25h past fetched -- past the 24h TTL
	c := LoadOSVCache(path, 1000+25*60*60)
	if len(c.Entries) != 0 {
		t.Errorf("stale cache should be discarded, got %v", c)
	}
}

func TestLoadOSVCacheMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "osv-cache.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := LoadOSVCache(path, 1000)
	if len(c.Entries) != 0 {
		t.Errorf("malformed cache should be empty, got %v", c)
	}
}

func TestLoadOSVCacheFresh(t *testing.T) {
	dir := t.TempDir()
	path := writeOSVCache(t, dir, 1000, map[string][]string{"npm:lodash@4.17.20": {"vulnerable, upgrade"}})
	c := LoadOSVCache(path, 1000+60)
	if got := c.lookup("npm", "lodash", "4.17.20"); len(got) != 1 {
		t.Errorf("fresh cache lookup = %v, want 1 entry", got)
	}
}

func TestScanDepsOSVTakesPriorityOverSuperseded(t *testing.T) {
	// moment has a bundled superseded-table entry AND, here, a cache hit --
	// the cache (specific, versioned) should win.
	cache := OSVCache{FetchedUnix: 1000, Entries: map[string][]string{
		"npm:moment@2.29.0": {"moment 2.29.0 has a ReDoS advisory, fixed in 2.29.4"},
	}}
	got := ScanDeps("package.json", `"moment": "2.29.0",`, cache, nil)
	// The advisory is package-prefixed (name+version -- <advisory>) and
	// marked Vuln (a confirmed OSV hit, escalatable to an ask).
	if len(got) != 1 || !strings.Contains(got[0].Advice, "ReDoS advisory") || !got[0].Vuln {
		t.Errorf("got %+v, want the package-named OSV advisory with Vuln=true", got)
	}
}

func TestScanDepsSupersededFallback(t *testing.T) {
	got := ScanDeps("package.json", `"moment": "2.29.0",`, OSVCache{}, nil)
	if len(got) != 1 {
		t.Fatalf("got %v, want 1 finding from the bundled table", got)
	}
}

func TestScanDepsCleanDepNoFinding(t *testing.T) {
	got := ScanDeps("package.json", `"react": "18.2.0",`, OSVCache{}, nil)
	if len(got) != 0 {
		t.Errorf("got %v, want no findings for a clean dependency", got)
	}
}

func TestScanDepsDisabled(t *testing.T) {
	got := ScanDeps("package.json", `"moment": "2.29.0",`, OSVCache{}, map[string]bool{"dep-check": true})
	if len(got) != 0 {
		t.Errorf("dep-check disabled but still fired: %v", got)
	}
}
