package preprocess

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestApplyGolden(t *testing.T) {
	cases := []struct {
		name         string
		cmd          string
		wantRule     string
		wantAdvisory bool
		wantApplied  bool
	}{
		{"go test", "go test ./...", "test-filter", false, true},
		{"go test with args", "go test -run TestFoo ./pkg", "test-filter", false, true},
		{"pytest", "pytest -x", "test-filter", false, true},
		{"go build", "go build ./...", "build-filter", false, true},
		{"tsc", "tsc --noEmit", "build-filter", false, true},
		{"eslint", "eslint .", "lint-filter", false, true},
		{"bare git diff", "git diff", "diff-cap", true, true},
		{"git diff with path", "git diff -- foo.go", "", false, false},
		{"unrelated command", "echo hello", "", false, false},
		{"looks similar but isn't", "go test-runner ./...", "", false, false},
		{"embedded, not prefix", "echo go test", "", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rule, newCmd, applied := Apply(c.cmd, nil)
			if applied != c.wantApplied {
				t.Fatalf("applied = %v, want %v (rule=%q newCmd=%q)", applied, c.wantApplied, rule.Name, newCmd)
			}
			if !applied {
				if newCmd != c.cmd {
					t.Errorf("unmatched command must pass through unchanged: got %q, want %q", newCmd, c.cmd)
				}
				return
			}
			if rule.Name != c.wantRule {
				t.Errorf("rule = %q, want %q", rule.Name, c.wantRule)
			}
			if rule.Advisory != c.wantAdvisory {
				t.Errorf("advisory = %v, want %v", rule.Advisory, c.wantAdvisory)
			}
			if rule.Advisory && newCmd != c.cmd {
				t.Errorf("advisory rule must not rewrite: got %q, want unchanged %q", newCmd, c.cmd)
			}
		})
	}
}

func TestDisabledRuleIsSkipped(t *testing.T) {
	_, newCmd, applied := Apply("go test ./...", map[string]bool{"test-filter": true})
	if applied {
		t.Fatal("disabled rule matched anyway")
	}
	if newCmd != "go test ./..." {
		t.Errorf("newCmd = %q, want unchanged", newCmd)
	}
}

func TestLogTailRespectsSizeThreshold(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.log")
	big := filepath.Join(dir, "big.log")
	if err := os.WriteFile(small, []byte("tiny"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(big, make([]byte, 300*1024), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, applied := Apply("cat "+small, nil); applied {
		t.Error("small log file should not be rewritten")
	}
	rule, newCmd, applied := Apply("cat "+big, nil)
	if !applied || rule.Name != "log-tail" {
		t.Fatalf("large log file should trigger log-tail, got rule=%q applied=%v", rule.Name, applied)
	}
	if newCmd == "cat "+big {
		t.Error("large log file command was not actually rewritten")
	}
}

// TestExitCodeSurvivesRewrite is the regression test for the bug caught
// while building this package: `set -o pipefail; cmd 2>&1 | grep ... | head`
// reports exit 1 on a PASSING command whenever grep finds nothing to match
// (grep's own "no match" exit code is 1, indistinguishable from pipefail's
// propagation of a real failure). captureThenFilter must not have this bug
// in either direction.
func TestExitCodeSurvivesRewrite(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}
	cases := []struct {
		name     string
		cmd      string
		wantExit int
	}{
		{"passing, filter finds nothing", `echo "all good"; exit 0`, 0},
		{"failing, filter finds a match", `echo "FAIL: boom"; exit 1`, 1},
		{"passing, filter text present but exit 0", `echo "FAIL: mentioned in passing output"; exit 0`, 0},
		{"failing, filter finds nothing", `echo "unrelated crash"; exit 2`, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wrapped := captureThenFilter(c.cmd, `grep -E "FAIL" -A 5 | head -n 120`)
			cmd := exec.Command("sh", "-c", wrapped)
			out, err := cmd.Output()
			gotExit := 0
			if err != nil {
				ee, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("unexpected error type: %v", err)
				}
				gotExit = ee.ExitCode()
			}
			if gotExit != c.wantExit {
				t.Errorf("exit code = %d, want %d (wrapped: %s)", gotExit, c.wantExit, wrapped)
			}
			if len(out) == 0 {
				t.Error("rewritten command produced zero output -- caught live: a silent exit made a real session distrust a passing test run and retry it five times")
			}
		})
	}
}
