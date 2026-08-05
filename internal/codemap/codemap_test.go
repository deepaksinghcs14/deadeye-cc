package codemap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func initGitRepo(t *testing.T, files ...string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	for _, f := range files {
		path := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", ".")
	run("commit", "-q", "-m", "initial")
	return dir
}

func entryFor(entries []Entry, dir string) *Entry {
	for i := range entries {
		if entries[i].Dir == dir {
			return &entries[i]
		}
	}
	return nil
}

// TestGroupDescendsIntoPureContainers is the whole container heuristic in
// one assertion: internal/ holds no files of its own so its children get
// their own rows; hooks/ holds files directly so it does not descend.
func TestGroupDescendsIntoPureContainers(t *testing.T) {
	paths := []string{
		"cmd/deadeye/main.go",
		"internal/a/a.go",
		"internal/b/b.go",
		"hooks/x.sh",
		"hooks/y.sh",
		"README.md",
	}
	entries := Group(paths)
	for _, want := range []string{"cmd/deadeye/", "internal/a/", "internal/b/", "hooks/", "(root)"} {
		if entryFor(entries, want) == nil {
			t.Errorf("missing row %q in %+v", want, entries)
		}
	}
	for _, tooDeep := range []string{"internal/", "cmd/", "hooks/x/"} {
		if entryFor(entries, tooDeep) != nil {
			t.Errorf("unexpected row %q in %+v", tooDeep, entries)
		}
	}
}

func TestGroupCapsDirectories(t *testing.T) {
	var paths []string
	// 30 dirs of 1 file each, plus 5 dirs of 10 files -- the big ones must
	// survive the cap.
	for i := 0; i < 30; i++ {
		paths = append(paths, string(rune('a'+i%26))+strings.Repeat("x", i/26+1)+"/f.go")
	}
	for i := 0; i < 5; i++ {
		big := "big" + string(rune('0'+i))
		for j := 0; j < 10; j++ {
			paths = append(paths, big+"/f"+string(rune('0'+j))+".go")
		}
	}
	entries := Group(paths)
	if len(entries) != maxDirs {
		t.Fatalf("got %d rows, want the cap of %d", len(entries), maxDirs)
	}
	for i := 0; i < 5; i++ {
		if entryFor(entries, "big"+string(rune('0'+i))+"/") == nil {
			t.Errorf("high-count dir big%d/ did not survive the cap", i)
		}
	}
}

func TestGroupDominantExtension(t *testing.T) {
	entries := Group([]string{"a/x.go", "a/y.go", "a/z.md"})
	if e := entryFor(entries, "a/"); e == nil || e.Ext != ".go" {
		t.Errorf("majority extension not picked: %+v", entries)
	}
	// A true 50/50 split yields "" -- never a coin flip.
	entries = Group([]string{"b/x.go", "b/y.md"})
	if e := entryFor(entries, "b/"); e == nil || e.Ext != "" {
		t.Errorf("tie should yield no extension, got %+v", entries)
	}
}

func TestFingerprintStableAndSensitive(t *testing.T) {
	base := "a.go\nb.go\nc.go\n"
	if Fingerprint(base) != Fingerprint(base) {
		t.Error("same input produced different fingerprints")
	}
	if Fingerprint(base) == Fingerprint(base+"d.go\n") {
		t.Error("added path did not change the fingerprint")
	}
	// git's output is sorted -- a reorder means real drift, not noise.
	if Fingerprint(base) == Fingerprint("b.go\na.go\nc.go\n") {
		t.Error("reordered input did not change the fingerprint")
	}
}

func TestExtractDocComment(t *testing.T) {
	cases := []struct {
		name string
		head string
		want string
	}{
		{"package-is", "// Package secscan is coder mode's lens made deterministic.\npackage secscan\n", "coder mode's lens made deterministic"},
		{"command-is", "// Command deadeye is deadeye's single binary: hook client and daemon.\npackage main\n", "deadeye's single binary: hook client and daemon"},
		{"build-tag-before-doc", "//go:build linux\n\n// Package sys holds linux syscall shims.\npackage sys\n", "holds linux syscall shims"},
		{"no-doc", "package main\n\nfunc main() {}\n", ""},
		{"ordinary-comment-not-doc", "// this file does things\npackage main\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := leadingDocComment([]byte(c.head)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestRegenerateIsIdempotent: no work when nothing changed -- the whole
// invalidation contract in one test.
func TestRegenerateIsIdempotent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initGitRepo(t, "pkg/a.go", "top.go")

	if err := Regenerate(repo); err != nil {
		t.Fatal(err)
	}
	fi1, err := os.Stat(MapPath(repo))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := Regenerate(repo); err != nil {
		t.Fatal(err)
	}
	fi2, _ := os.Stat(MapPath(repo))
	if !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Error("Regenerate rewrote the map with nothing changed")
	}

	// Structural drift must regenerate.
	cmd := exec.Command("git", "add", ".")
	if err := os.WriteFile(filepath.Join(repo, "new.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	time.Sleep(10 * time.Millisecond)
	if err := Regenerate(repo); err != nil {
		t.Fatal(err)
	}
	fi3, _ := os.Stat(MapPath(repo))
	if fi1.ModTime().Equal(fi3.ModTime()) {
		t.Error("Regenerate did not rewrite after a tracked file was added")
	}
}

// TestLsFilesHandlesQuotableFilenames is the regression test for a bug
// where plain `git ls-files` (no -z) let core.quotepath's default C-quoting
// of non-ASCII paths (e.g. `"docs/\303\244.txt"`) leak a literal `"`
// character into the parsed path, corrupting Group's "/"-split bucketing
// into a phantom quoted directory. -z disables quoting entirely.
func TestLsFilesHandlesQuotableFilenames(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initGitRepo(t, "docs/ä.txt", "docs/plain.txt")

	paths, _, err := lsFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if strings.Contains(p, `"`) {
			t.Errorf("path %q retained a quote character -- -z quoting fix regressed", p)
		}
	}

	entries := Group(paths)
	if e := entryFor(entries, "docs/"); e == nil || e.Files != 2 {
		t.Errorf("expected docs/ with 2 files, got %+v", entries)
	}
	if e := entryFor(entries, `"docs/`); e != nil {
		t.Errorf("phantom quoted directory row present: %+v", e)
	}
}

// TestRegenerateFromSubdirectoryMapsWholeRepo is the regression test for
// the cwd-scoping bug: ls-files with no pathspec is scoped to cwd, so
// without resolving the repo root first, a session started in pkg/ would
// map only that subtree.
func TestRegenerateFromSubdirectoryMapsWholeRepo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initGitRepo(t, "pkg/a.go", "top.go")

	if err := Regenerate(filepath.Join(repo, "pkg")); err != nil {
		t.Fatal(err)
	}
	body := Load(filepath.Join(repo, "pkg"))
	if !strings.Contains(body, "(root)") {
		t.Errorf("map generated from a subdirectory is missing the repo root's files:\n%s", body)
	}
}

func TestRegenerateNoOpOutsideGitRepo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := Regenerate(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Dir()); !os.IsNotExist(err) {
		entries, _ := os.ReadDir(Dir())
		if len(entries) != 0 {
			t.Errorf("map dir should be empty for a non-git cwd, got %v", entries)
		}
	}
}

func TestTextEmptyWhenNothingExists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := Text(t.TempDir()); got != "" {
		t.Errorf("Text should be empty with no map/touch/notes, got %q", got)
	}
}
