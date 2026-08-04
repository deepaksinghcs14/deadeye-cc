package preprocess

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
		{"gradle", "gradle test", "test-filter", false, true},
		{"gradlew", "./gradlew test", "test-filter", false, true},
		{"dotnet", "dotnet test", "test-filter", false, true},
		{"rspec via bundler", "bundle exec rspec spec/", "test-filter", false, true},
		{"bare rspec", "rspec spec/models", "test-filter", false, true},
		{"phpunit", "phpunit tests/", "test-filter", false, true},
		{"vendored phpunit", "./vendor/bin/phpunit", "test-filter", false, true},
		{"go build", "go build ./...", "build-filter", false, true},
		{"tsc", "tsc --noEmit", "build-filter", false, true},
		{"docker build", "docker build -t app .", "build-filter", false, true},
		{"eslint", "eslint .", "lint-filter", false, true},
		{"npm install", "npm install", "install-filter", false, true},
		{"npm ci", "npm ci", "install-filter", false, true},
		{"pip install", "pip install -r requirements.txt", "install-filter", false, true},
		{"kubectl logs", "kubectl logs my-pod", "logs-tail", false, true},
		{"kubectl logs already bounded", "kubectl logs my-pod --tail=50", "", false, false},
		{"bare git diff", "git diff", "diff-cap", true, true},
		{"git diff with path", "git diff -- foo.go", "", false, false},
		{"bare git log", "git log", "history-cap", true, true},
		{"bare git show", "git show", "history-cap", true, true},
		{"scoped git log", "git log --oneline -20", "", false, false},
		{"git diff HEAD", "git diff HEAD", "diff-cap", true, true},
		{"git diff cached", "git diff --cached", "diff-cap", true, true},
		{"git log -p", "git log -p", "history-cap", true, true},
		{"git log oneline unbounded", "git log --oneline", "history-cap", true, true},
		{"terraform plan", "terraform plan", "tf-plan-cap", true, true},
		{"terraform plan targeted still fires", "terraform plan -target=module.db", "tf-plan-cap", true, true},
		{"terraform apply is not plan", "terraform apply", "", false, false},
		{"kubectl get yaml", "kubectl get pod my-pod -o yaml", "kubectl-yaml-cap", true, true},
		{"kubectl get name is bounded", "kubectl get pods -o name", "", false, false},
		{"npm ls bare", "npm ls", "npm-ls-cap", true, true},
		{"npm ls depth0 is bounded", "npm ls --depth=0", "", false, false},
		{"bare find", "find .", "find-cap", true, true},
		{"find with name is scoped", "find . -name '*.go'", "", false, false},
		{"tree unbounded", "tree", "tree-cap", true, true},
		{"tree with L is bounded", "tree -L 2", "", false, false},
		{"du unbounded", "du ~/projects", "du-cap", true, true},
		{"du -sh is bounded", "du -sh .", "", false, false},
		{"pip list", "pip list", "pkg-list-cap", true, true},
		{"brew list", "brew list", "pkg-list-cap", true, true},
		{"unrelated command", "echo hello", "", false, false},
		{"looks similar but isn't", "go test-runner ./...", "", false, false},
		{"embedded, not prefix", "echo go test", "", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rule, newCmd, applied := Apply("", c.cmd, nil)
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
	_, newCmd, applied := Apply("", "go test ./...", map[string]bool{"test-filter": true})
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

	if _, _, applied := Apply("", "cat "+small, nil); applied {
		t.Error("small log file should not be rewritten")
	}
	rule, newCmd, applied := Apply("", "cat "+big, nil)
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

// TestFilterSurvivesRealToolOutput runs the SHIPPED filter strings against
// output actually captured from real tool runs (internal/preprocess/testdata),
// not hand-written approximations -- hand-written fixtures are exactly how
// the original build-filter (which matched zero of Go's own compiler error
// text) and test-filter (after-context only, and matching none of a real
// mocha/pytest failure) shipped without anyone noticing.
func TestFilterSurvivesRealToolOutput(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}
	cases := []struct {
		name        string
		fixture     string
		filter      string
		exitCode    int
		mustContain []string // every one of these must survive filtering
	}{
		{
			name:        "go build: compiler errors have none of the original filter's tokens",
			fixture:     "go_build_fail.txt",
			filter:      buildFilter,
			exitCode:    1,
			mustContain: []string{"undefined: foo", "fixtures.example/goproj"},
		},
		{
			name:        "go test build failure: the cause line is BEFORE FAIL, an after-context-only filter drops it",
			fixture:     "go_test_buildfail.txt",
			filter:      testFilter,
			exitCode:    1,
			mustContain: []string{"undefined: helper", "build failed"},
		},
		{
			name:        "go test assertion failure",
			fixture:     "go_test_fail.txt",
			filter:      testFilter,
			exitCode:    1,
			mustContain: []string{"--- FAIL: TestAdd", "want 6"},
		},
		{
			name:        "mocha failure: the original filter matched none of this",
			fixture:     "mocha_fail.txt",
			filter:      testFilter,
			exitCode:    1,
			mustContain: []string{"1 failing", "AssertionError"},
		},
		{
			name:        "jest failure",
			fixture:     "jest_fail.txt",
			filter:      testFilter,
			exitCode:    1,
			mustContain: []string{"1 failed"},
		},
		{
			name:        "pytest failure",
			fixture:     "pytest_fail.txt",
			filter:      testFilter,
			exitCode:    1,
			mustContain: []string{"FAILED", "AssertionError"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, err := filepath.Abs(filepath.Join("testdata", c.fixture))
			if err != nil {
				t.Fatal(err)
			}
			cmd := "cat " + path + "; exit " + strconv.Itoa(c.exitCode)
			wrapped := captureThenFilter(cmd, c.filter)
			out, err := exec.Command("sh", "-c", wrapped).Output()
			gotExit := 0
			if err != nil {
				ee, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("unexpected error type: %v", err)
				}
				gotExit = ee.ExitCode()
			}
			if gotExit != c.exitCode {
				t.Errorf("exit code = %d, want %d", gotExit, c.exitCode)
			}
			for _, want := range c.mustContain {
				if !strings.Contains(string(out), want) {
					t.Errorf("filtered output missing %q\n--- got ---\n%s", want, out)
				}
			}
		})
	}
}

// TestFilterCollapsesGenuinePassToTheFallbackLine confirms a real passing
// `go test -v` run (all-PASS, no failure keyword anywhere) still collapses
// to the single fallback line rather than leaking the verbose PASS dump.
func TestFilterCollapsesGenuinePassToTheFallbackLine(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}
	path, err := filepath.Abs(filepath.Join("testdata", "go_test_pass.txt"))
	if err != nil {
		t.Fatal(err)
	}
	wrapped := captureThenFilter("cat "+path, testFilter)
	out, err := exec.Command("sh", "-c", wrapped).Output()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(out), "no output survived filtering") {
		t.Errorf("a genuinely passing run should collapse to the fallback line, got: %s", out)
	}
}

// TestFilterSurvivesTranscribedRunnerFormats covers the runners whose
// toolchains aren't installed on the dev machine: these snippets are
// TRANSCRIBED from each tool's documented output format rather than
// captured live (unlike testdata/*.txt, which are real runs). Weaker
// evidence, better than none -- if a real capture ever contradicts one of
// these, trust the capture.
func TestFilterSurvivesTranscribedRunnerFormats(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}
	cases := []struct {
		name        string
		output      string
		mustContain string
	}{
		{
			name:        "rspec failure summary",
			output:      "....F\n\nFailures:\n\n  1) Order applies tax before discount\n     Failure/Error: expect(total).to eq(108)\n\n5 examples, 1 failure\n",
			mustContain: "Failure/Error",
		},
		{
			name:        "gradle test failure",
			output:      "> Task :test FAILED\n\nOrderTest > appliesTax FAILED\n    org.opentest4j.AssertionFailedError at OrderTest.java:42\n\nBUILD FAILED in 12s\n",
			mustContain: "FAILED",
		},
		{
			name:        "dotnet test failure",
			output:      "Failed!  - Failed:     1, Passed:     4, Skipped:     0, Total:     5\n  Failed OrderTests.AppliesTax [12 ms]\n  Error Message:\n   Assert.Equal() Failure\n",
			mustContain: "Failed",
		},
		{
			name:        "phpunit failure",
			output:      "FAILURES!\nTests: 5, Assertions: 9, Failures: 1.\n\n1) OrderTest::testAppliesTax\nFailed asserting that 107 matches expected 108.\n",
			mustContain: "FAILURES!",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wrapped := captureThenFilter("printf '%s' "+shellQuote(c.output)+"; exit 1", testFilter)
			out, err := exec.Command("sh", "-c", wrapped).Output()
			if err != nil {
				ee, ok := err.(*exec.ExitError)
				if !ok || ee.ExitCode() != 1 {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if !strings.Contains(string(out), c.mustContain) {
				t.Errorf("filtered output missing %q:\n%s", c.mustContain, out)
			}
		})
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// TestInstallFilterKeepsErrors: npm's failure marker is "npm ERR!", pip's
// is "ERROR:"; progress spam has neither and must collapse to the
// fallback line.
func TestInstallFilterKeepsErrors(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}
	npmFail := "npm ERR! code ERESOLVE\nnpm ERR! ERESOLVE unable to resolve dependency tree\n"
	wrapped := captureThenFilter("printf '%s' "+shellQuote(npmFail)+"; exit 1", `grep -E "(ERR!|ERROR:|error:|WARN|warning)" -B 1 -A 3 | head -n 100`)
	out, _ := exec.Command("sh", "-c", wrapped).Output()
	if !strings.Contains(string(out), "ERESOLVE") {
		t.Errorf("npm error detail lost: %s", out)
	}

	progressOnly := "added 1204 packages in 32s\n147 packages are looking for funding\n"
	wrapped = captureThenFilter("printf '%s' "+shellQuote(progressOnly), `grep -E "(ERR!|ERROR:|error:|WARN|warning)" -B 1 -A 3 | head -n 100`)
	out, err := exec.Command("sh", "-c", wrapped).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "no output survived filtering") {
		t.Errorf("progress spam should collapse to the fallback line, got: %s", out)
	}
}

// TestCatLargeAdvisesOnBigStructuredFiles: .json/.csv/etc get an advisory
// (never a rewrite -- truncating structured data can cut mid-record),
// with the same 200KB threshold and cwd resolution as log-tail.
func TestCatLargeAdvisesOnBigStructuredFiles(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "dump.json")
	small := filepath.Join(dir, "small.json")
	if err := os.WriteFile(big, make([]byte, 300*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(small, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	rule, newCmd, applied := Apply(dir, "cat dump.json", nil)
	if !applied || rule.Name != "cat-large" || !rule.Advisory {
		t.Fatalf("large json: rule=%q advisory=%v applied=%v, want cat-large advisory", rule.Name, rule.Advisory, applied)
	}
	if newCmd != "cat dump.json" {
		t.Errorf("advisory must not rewrite: got %q", newCmd)
	}
	if _, _, applied := Apply(dir, "cat small.json", nil); applied {
		t.Error("small structured file should not trigger anything")
	}
}

// TestApplySkipsCommandsWithShellStructure is the regression test for A3:
// captureThenFilter's textual `out=$(cmd 2>&1)` wrapping only understands a
// single simple command. Every one of these matches a rule's prefix and
// would have been wrapped before the rewritable() guard existed.
func TestApplySkipsCommandsWithShellStructure(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
	}{
		{"trailing comment", "go test ./... # skip the flaky ones"},
		{"chained with &&", "go test ./... && ./report.sh"},
		{"chained with ||", "go build ./... || echo failed"},
		{"piped", "go test ./... | tee out.txt"},
		{"redirected", "go build ./... > log 2>&1"},
		{"semicolon chained", "go build ./...; echo done"},
		{"heredoc", "npm test <<EOF\nfoo\nEOF"},
		{"trailing backslash continuation", "go test ./... \\"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, newCmd, applied := Apply("", c.cmd, nil)
			if applied {
				t.Fatalf("command with shell structure must not be rewritten: %q -> %q", c.cmd, newCmd)
			}
			if newCmd != c.cmd {
				t.Errorf("unrewritten command must pass through byte-identical, got %q", newCmd)
			}
		})
	}
}

// TestCaptureThenFilterBreaksOnShellStructure documents, concretely, why
// rewritable() exists rather than trusting the comment above it: it uses
// echo stand-ins (not real go test/npm test) so this stays fast and
// hermetic -- Apply's own guard is what actually protects real commands
// from ever reaching this path.
func TestCaptureThenFilterBreaksOnShellStructure(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}

	// A trailing comment swallows the wrapper's own closing tail --
	// verified live: the shell reports "unexpected EOF while looking for
	// matching ')'" and the command never runs at all.
	wrapped := captureThenFilter(`echo hi # trailing comment`, `grep -E "x" -A 1`)
	if _, err := exec.Command("sh", "-c", wrapped).CombinedOutput(); err == nil {
		t.Error("expected the comment-corrupted wrapper to fail to parse")
	}

	// The appended 2>&1 binds to the LAST command in a chain, not the one
	// that matched -- its stderr never reaches $out, so it's neither
	// filtered nor visible.
	wrapped = captureThenFilter(`sh -c 'echo ERRSTREAM 1>&2; exit 0' && echo second`, `grep -E "ERRSTREAM"`)
	out, err := exec.Command("sh", "-c", wrapped).Output()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(string(out), "ERRSTREAM") {
		t.Error("expected ERRSTREAM to escape the capture, proving 2>&1 bound to the wrong command in the chain")
	}
}

// TestLogTailResolvesRelativePathAgainstGivenCwd is the regression test for
// A4: the daemon serves every project from whichever directory happened to
// spawn it, so os.Stat on a relative path must resolve against the
// session's cwd (passed in), never the daemon's own.
func TestLogTailResolvesRelativePathAgainstGivenCwd(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "server.log")
	if err := os.WriteFile(big, make([]byte, 300*1024), 0o644); err != nil {
		t.Fatal(err)
	}

	rule, newCmd, applied := Apply(dir, "cat server.log", nil)
	if !applied || rule.Name != "log-tail" {
		t.Fatalf("a large relative log path should resolve against the given cwd, got rule=%q applied=%v", rule.Name, applied)
	}
	if newCmd != "tail -n 200 server.log" {
		t.Errorf("newCmd = %q, want the original relative path preserved (the session's own shell has the right cwd; only the daemon's stat needed it)", newCmd)
	}

	if _, _, applied := Apply(t.TempDir(), "cat server.log", nil); applied {
		t.Error("a relative log path must not resolve against an unrelated cwd")
	}
}

// TestCatLogRegexRejectsShellMetacharacters is defense in depth for A4's
// injection finding: even though Apply's shellStructure guard already
// rejects any command containing ';', catLogRe itself should not admit a
// filename with shell metacharacters if ever called directly.
func TestCatLogRegexRejectsShellMetacharacters(t *testing.T) {
	if catLogRe.MatchString("cat a.log;rm -rf /tmp") {
		t.Error("catLogRe must not match a filename containing shell metacharacters")
	}
}
