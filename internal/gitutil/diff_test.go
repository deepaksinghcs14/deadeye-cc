package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAddedRanges(t *testing.T) {
	diff := `diff --git a/internal/a.go b/internal/a.go
index 1111111..2222222 100644
--- a/internal/a.go
+++ b/internal/a.go
@@ -10,0 +11,3 @@ func foo() {
+	one()
+	two()
+	three()
diff --git a/internal/b.go b/internal/b.go
--- a/internal/b.go
+++ b/internal/b.go
@@ -5 +5 @@
-	old()
+	new()
`
	got := AddedRanges(diff)
	want := map[string][]Range{
		"internal/a.go": {{Start: 11, End: 13}},
		"internal/b.go": {{Start: 5, End: 5}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AddedRanges:\n got %v\nwant %v", got, want)
	}
}

// A pure deletion leaves no new-side line for a later commit to touch, so
// it contributes no range -- otherwise `git log -L` would be handed a
// zero-length span.
func TestAddedRangesSkipsPureDeletions(t *testing.T) {
	diff := `--- a/x.go
+++ b/x.go
@@ -4,3 +3,0 @@
-	gone()
-	also()
-	gone()
`
	if got := AddedRanges(diff); len(got) != 0 {
		t.Errorf("got %v, want no ranges for a delete-only hunk", got)
	}
}

// A deleted file's new side is /dev/null: there is no path to attribute
// ranges to, and treating "/dev/null" as one would produce a bogus key.
func TestAddedRangesIgnoresDevNull(t *testing.T) {
	diff := `--- a/gone.go
+++ /dev/null
@@ -1,2 +0,0 @@
-	a()
-	b()
`
	if got := AddedRanges(diff); len(got) != 0 {
		t.Errorf("got %v, want no ranges when the new side is /dev/null", got)
	}
}

func TestParseHunkNewSide(t *testing.T) {
	cases := []struct {
		header string
		want   Range
		ok     bool
	}{
		{"@@ -1,4 +1,6 @@ func foo()", Range{1, 6}, true},
		{"@@ -1 +1 @@", Range{1, 1}, true},  // omitted count means 1
		{"@@ -4,3 +3,0 @@", Range{}, false}, // zero-length new side
		{"@@ nonsense @@", Range{}, false},
		{"not a hunk", Range{}, false},
	}
	for _, c := range cases {
		got, ok := parseHunkNewSide(c.header)
		if ok != c.ok || got != c.want {
			t.Errorf("parseHunkNewSide(%q) = %v,%v; want %v,%v", c.header, got, ok, c.want, c.ok)
		}
	}
}

func TestParseDiffPath(t *testing.T) {
	cases := map[string]string{
		"b/internal/x.go":            "internal/x.go",
		"a/internal/x.go":            "internal/x.go",
		"/dev/null":                  "",
		"b/dir/with space.go\t(old)": "dir/with space.go",
	}
	for in, want := range cases {
		if got := parseDiffPath(in); got != want {
			t.Errorf("parseDiffPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNumstatAndHeadOnRealRepo(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "first")

	if Head(dir) == "" {
		t.Error("Head returned empty in a repo with one commit")
	}
	if Root(dir) == "" {
		t.Error("Root returned empty inside a repo")
	}

	files, adds, dels := Numstat(dir, "HEAD")
	if files != 1 || adds != 3 || dels != 0 {
		t.Errorf("Numstat = %d files, +%d/-%d; want 1 file, +3/-0", files, adds, dels)
	}
}

// Outside a repo every read fails open to a zero value rather than
// erroring -- INV-5, the same posture as gitutil.Output.
func TestFailsOpenOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	if got := Root(dir); got != "" {
		t.Errorf("Root outside a repo = %q, want \"\"", got)
	}
	if got := Head(dir); got != "" {
		t.Errorf("Head outside a repo = %q, want \"\"", got)
	}
	if f, a, d := Numstat(dir, "HEAD"); f != 0 || a != 0 || d != 0 {
		t.Errorf("Numstat outside a repo = %d,%d,%d; want zeroes", f, a, d)
	}
}
