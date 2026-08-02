// Package preprocess holds the deterministic Bash command rewrites that
// stop verbose tool output from entering context (PLAN.md §5.3). Rules run
// at PreToolUse, before the command executes, and rewrite via
// hookio.HookSpecificOutput.UpdatedInput -- never at PostToolUse, where the
// output has already landed.
package preprocess

import (
	"os"
	"regexp"
	"strings"
)

// Rule is one deterministic rewrite. Advisory rules never rewrite --
// TryRewrite returns the original command unchanged with ok=true, and the
// caller surfaces Note via additionalContext instead of updatedInput.
type Rule struct {
	Name       string
	Note       string // human-readable savings/behavior note, shown in the decision log and /deadeye-audit
	Advisory   bool
	TryRewrite func(cmd string) (rewritten string, matched bool)

	// EstBeforeBytes/EstAfterBytes are rough, static "typical case"
	// estimates for /deadeye-audit -- not measurements of this specific
	// invocation (PreToolUse runs before the command executes, so the real
	// output size isn't known yet). Real bytes_before/after can replace
	// these once Phase 1's PostToolUse tool_response is wired up (see
	// docs/verified.md's note on that field).
	EstBeforeBytes int
	EstAfterBytes  int
}

var (
	testFilterRe  = regexp.MustCompile(`^(npm test|npx jest|pytest|go test|cargo test|mvn test)(\s|$)`)
	buildFilterRe = regexp.MustCompile(`^(npm run build|go build|cargo build|tsc)(\s|$)`)
	lintFilterRe  = regexp.MustCompile(`^(eslint|golangci-lint|ruff)(\s|$)`)
	catLogRe      = regexp.MustCompile(`^cat\s+(\S+\.(?:log|out))\s*$`)
	bareGitDiffRe = regexp.MustCompile(`^git diff\s*$`)
)

// captureThenFilter wraps cmd so the ORIGINAL command's exit status survives
// the filtering pipeline, portable across bash/zsh/sh. `set -o pipefail`
// alone is not enough here: when the underlying command succeeds AND grep
// finds no matching lines (grep's own "no match" exit is 1), pipefail would
// report the whole pipeline as failed even though nothing failed --
// verified empirically. Capturing output first and exiting with the saved
// code afterward sidesteps that entirely.
//
// Second bug caught live (not in the original plan): a passing test/lint
// run with nothing for the filter to match produces zero output --
// indistinguishable, to the calling agent, from "didn't run" or "hung".
// Watching a real session: the model saw a silent-but-correct exit 0 from
// a filtered `go test`, didn't trust it, and retried the same test command
// five times with escalating workarounds (`echo $?`, redirecting to a log
// file, adding -v) before giving up and reporting success anyway. Every
// rewrite now guarantees at least one line of output either way.
func captureThenFilter(cmd, filter string) string {
	return `out=$(` + cmd + ` 2>&1); ec=$?; ` +
		`filtered=$(printf '%s\n' "$out" | ` + filter + `); ` +
		`if [ -n "$filtered" ]; then printf '%s\n' "$filtered"; ` +
		`else echo "deadeye: command exited $ec, no output survived filtering"; fi; ` +
		`exit $ec`
}

var Rules = []Rule{
	{
		Name:           "test-filter",
		Note:           "caps verbose test output to failure context only",
		EstBeforeBytes: 30000, // typical uncapped test-run dump
		EstAfterBytes:  9600,  // ~120 lines * ~80 bytes
		TryRewrite: func(cmd string) (string, bool) {
			if !testFilterRe.MatchString(cmd) {
				return cmd, false
			}
			return captureThenFilter(cmd, `grep -E "(FAIL|ERROR|error:|panic:|--- FAIL)" -A 5 | head -n 120`), true
		},
	},
	{
		Name:           "build-filter",
		Note:           "keeps only build errors, drops successful-build noise",
		EstBeforeBytes: 15000,
		EstAfterBytes:  9600,
		TryRewrite: func(cmd string) (string, bool) {
			if !buildFilterRe.MatchString(cmd) {
				return cmd, false
			}
			return captureThenFilter(cmd, `grep -E "error|Error|FAIL" -A 3 | head -n 120`), true
		},
	},
	{
		Name:           "log-tail",
		Note:           "tails large log files instead of dumping them whole",
		EstBeforeBytes: 500000, // rule only fires above the 200KB threshold; this is a typical case, not this file's real size
		EstAfterBytes:  16000,  // ~200 lines * ~80 bytes
		TryRewrite: func(cmd string) (string, bool) {
			m := catLogRe.FindStringSubmatch(strings.TrimSpace(cmd))
			if m == nil {
				return cmd, false
			}
			fi, err := os.Stat(m[1])
			if err != nil || fi.Size() <= 200*1024 {
				return cmd, false
			}
			return "tail -n 200 " + m[1], true
		},
	},
	{
		// Never rewritten silently -- a bare `git diff` in a large repo can
		// be huge, but truncating a diff can hide the exact lines the
		// caller needs. Suggest --stat first instead of guessing.
		Name:     "diff-cap",
		Note:     "suggests --stat first for an unscoped git diff",
		Advisory: true,
		TryRewrite: func(cmd string) (string, bool) {
			if !bareGitDiffRe.MatchString(strings.TrimSpace(cmd)) {
				return cmd, false
			}
			return cmd, true
		},
	},
	{
		Name:           "lint-filter",
		Note:           "caps linter output to a manageable head",
		EstBeforeBytes: 20000,
		EstAfterBytes:  12000,
		TryRewrite: func(cmd string) (string, bool) {
			if !lintFilterRe.MatchString(cmd) {
				return cmd, false
			}
			return captureThenFilter(cmd, `head -n 150`), true
		},
	},
}

// Apply runs the rule table in order against cmd, skipping disabled rule
// names. Returns the zero Rule and applied=false if nothing matched.
func Apply(cmd string, disabled map[string]bool) (rule Rule, newCmd string, applied bool) {
	for _, r := range Rules {
		if disabled[r.Name] {
			continue
		}
		if out, ok := r.TryRewrite(cmd); ok {
			return r, out, true
		}
	}
	return Rule{}, cmd, false
}
