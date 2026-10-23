package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoPersonalPathsInFixturesOrBenchmarks keeps real home directories and
// Claude Code scratch paths out of the public repo: fixtures and benchmark
// data are captured from real sessions, and a captured payload carries the
// author's home directory and session ids. Checks tracked files only.
// The markers are built by concatenation so this file does not match itself.
func TestNoPersonalPathsInFixturesOrBenchmarks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	markers := [][]byte{
		[]byte("/" + "Users" + "/"),
		[]byte("/private/tmp/" + "claude-"),
		[]byte("private-tmp-" + "claude-"), // the same path as a dashed project slug
		[]byte("claude-" + "501"),
	}
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" || !(strings.HasPrefix(rel, "benchmarks/") || strings.Contains(rel, "testdata/")) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue // deleted in the working tree but still listed
		}
		for _, m := range markers {
			if bytes.Contains(b, m) {
				t.Errorf("%s contains %q: use a neutral placeholder such as /home/user/proj", rel, m)
			}
		}
	}
}
