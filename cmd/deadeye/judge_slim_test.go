//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeClaude puts a `claude` shell script first on PATH that appends its
// argv to a log and then runs body.
func fakeClaude(t *testing.T, body string) (argLog string) {
	t.Helper()
	dir := t.TempDir()
	argLog = filepath.Join(dir, "args.log")
	script := "#!/bin/sh\necho \"$*\" >> " + argLog + "\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	judgeSlimUnsupported.Store(false)
	t.Cleanup(func() { judgeSlimUnsupported.Store(false) })
	return argLog
}

func calls(t *testing.T, argLog string) []string {
	t.Helper()
	b, _ := os.ReadFile(argLog)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// A Claude Code that predates the slim flags must still give a verdict --
// otherwise every user on an older CLI silently loses the judge.
func TestJudgeFallsBackWhenSlimFlagsAreUnknown(t *testing.T) {
	argLog := fakeClaude(t, `case "$*" in *--strict-mcp-config*) echo "error: unknown option '--strict-mcp-config'" >&2; exit 1;; esac
echo 1`)

	tier, ok := judgeTierClaude("some task")
	if !ok || tier != 1 {
		t.Fatalf("got (%d, %v), want (1, true) via the plain call", tier, ok)
	}
	if got := calls(t, argLog); len(got) != 2 || strings.Contains(got[1], "--strict-mcp-config") {
		t.Fatalf("want a slim call then a plain one, got %q", got)
	}

	// Latched: the next call must not pay for the rejected attempt again.
	if _, ok := judgeTierClaude("another task"); !ok {
		t.Fatal("second call failed")
	}
	if got := calls(t, argLog); len(got) != 3 {
		t.Errorf("want 3 calls in total (no repeated slim attempt), got %d: %q", len(got), got)
	}
}

// Any other failure -- auth, network, a timeout -- would fail the plain call
// the same way, so it must not buy a second blocking call or latch slim off.
func TestJudgeDoesNotRetryOnOtherFailures(t *testing.T) {
	argLog := fakeClaude(t, `echo "Invalid API key" >&2; exit 1`)

	if _, ok := judgeTierClaude("some task"); ok {
		t.Fatal("a failed call reported a verdict")
	}
	if got := calls(t, argLog); len(got) != 1 {
		t.Errorf("want exactly 1 call, got %d: %q", len(got), got)
	}
	if judgeSlimUnsupported.Load() {
		t.Error("an unrelated failure latched the slim call off")
	}
}

// The slim call is only cheap with every one of these: dropping
// --strict-mcp-config alone made a cold call cost more than the plain one.
func TestJudgeSlimCallCarriesItsFlags(t *testing.T) {
	argLog := fakeClaude(t, `echo 0`)
	if tier, ok := judgeTierClaude("some task"); !ok || tier != 0 {
		t.Fatalf("got (%d, %v), want (0, true)", tier, ok)
	}
	got := calls(t, argLog)[0]
	for _, flag := range []string{"-p", "--model sonnet", "--system-prompt", "--tools", "--strict-mcp-config", "--disable-slash-commands", "--no-session-persistence"} {
		if !strings.Contains(got, flag) {
			t.Errorf("slim call is missing %q: %s", flag, got)
		}
	}
}
