// Package preprocess holds the deterministic Bash command rewrites that
// stop verbose tool output from entering context (PLAN.md §5.3). Rules run
// at PreToolUse, before the command executes, and rewrite via
// hookio.HookSpecificOutput.UpdatedInput -- never at PostToolUse, where the
// output has already landed.
package preprocess

import (
	"os"
	"path/filepath"
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
	TryRewrite func(cwd, cmd string) (rewritten string, matched bool)

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
	testFilterRe    = regexp.MustCompile(`^(npm test|npx jest|pytest|go test|cargo test|mvn test|gradle test|\./gradlew test|dotnet test|bundle exec rspec|rspec|phpunit|\./vendor/bin/phpunit)(\s|$)`)
	buildFilterRe   = regexp.MustCompile(`^(npm run build|go build|cargo build|tsc|docker build)(\s|$)`)
	lintFilterRe    = regexp.MustCompile(`^(eslint|golangci-lint|ruff)(\s|$)`)
	installFilterRe = regexp.MustCompile(`^(npm install|npm ci|yarn install|pnpm install|pip install|pip3 install)(\s|$)`)
	kubectlLogsRe   = regexp.MustCompile(`^kubectl logs(\s|$)`)
	catLogRe        = regexp.MustCompile(`^cat\s+([\w./@+-]+\.(?:log|out))\s*$`)
	catLargeRe      = regexp.MustCompile(`^cat\s+([\w./@+-]+\.(?:json|txt|csv|xml|ya?ml))\s*$`)
	// Widened past the exactly-bare forms: `git diff HEAD` or `--cached`
	// and `git log -p`/`--stat`/`--oneline` are just as unbounded as their
	// bare parents. Anything with a pathspec or -n/--max-count stays quiet.
	bareGitDiffRe = regexp.MustCompile(`^git diff( --cached| --staged)?( HEAD[~^0-9]*)?\s*$`)
	bareGitHistRe = regexp.MustCompile(`^git (log|show)(( -p| --stat| --oneline))*\s*$`)
	tfPlanRe      = regexp.MustCompile(`^terraform plan(\s|$)`)
	kubectlYamlRe = regexp.MustCompile(`^kubectl get\b.* -o ?(yaml|json)\b`)
	npmLsRe       = regexp.MustCompile(`^npm ls\s*$`)
	bareFindRe    = regexp.MustCompile(`^find\s+\S+\s*$`) // a path and nothing else: no -name/-type/-maxdepth
	treeRe        = regexp.MustCompile(`^tree\b`)
	duRe          = regexp.MustCompile(`^du\b`)
	pkgListRe     = regexp.MustCompile(`^(pip3? list|brew list)\s*$`)

	// shellStructure flags a command as having shell control-flow structure
	// (a comment, a command separator/chain, redirection, a line
	// continuation) that captureThenFilter's textual `out=$(cmd 2>&1)`
	// wrapping does not understand. Verified live, both directions:
	// `go test ./... # skip flaky` -- the wrapper's own closing `2>&1); ec=$?;
	// ...` tail gets swallowed by the comment, and the shell reports
	// "unexpected EOF while looking for matching ')'" -- the command never
	// runs at all. `go test ./... && ./report.sh` -- the appended `2>&1`
	// binds to the LAST command in the chain, not the one that matched, so
	// the actual test runner's stderr escapes the capture entirely (neither
	// filtered nor visible). Rules only ever match a command by its leading
	// tokens, never its whole shape, so this guard runs once for all of
	// them: not rewriting is always safe, rewriting a command whose shape we
	// didn't check is not.
	shellStructure = regexp.MustCompile("[#;&|<>\\\\\n]")
)

func rewritable(cmd string) bool { return !shellStructure.MatchString(cmd) }

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

// testFilter and buildFilter are widened past their original shipped
// versions, which were verified live to lose real failure content:
//   - build-filter's original `error|Error|FAIL` matches NONE of Go's own
//     compiler error text (`./main.go:4:2: undefined: foo`) -- a real `go
//     build` failure survived filtering with ZERO diagnostics, just
//     "no output survived filtering". `file:line:col` / `file(line,col)`
//     covers go/tsc/clang; `-B 2` keeps the `# package/path` header Go
//     prints above the errors.
//   - test-filter's original `-A 5` (after-context only) drops the cause
//     when Go's test runner reports a build failure -- the real
//     `./foo_test.go:12:5: undefined: helper` lines print BEFORE `FAIL`, so
//     only `FAIL [build failed]` survived. `-B 3` fixes that. The original
//     pattern also matched none of a real mocha or pytest failure
//     (`1 failing`, `AssertionError`, `FAILED tests/x.py::test_y`) --
//     `AssertionError|[0-9]+ (failing|failed)|●|not ok|Traceback` covers
//     mocha/jest/tap/pytest; verified against real captured output from
//     each in internal/preprocess/testdata.
//   - install-filter's original pattern was case-sensitive for `WARN` and
//     `npm ERR!` -- npm 7+ (current: 11) renamed both markers to lowercase
//     `npm warn` / `npm error`, so a real `npm install` under npm 11 (both
//     a clean run with deprecation warnings and a hard failure, e.g. a
//     404) matched NOTHING and printed only "no output survived
//     filtering", silently eating the actual error. `-i` fixes it without
//     narrowing pip's already-matching `ERROR:`/`WARNING:`.
var (
	testFilter    = `grep -E "(FAIL|FAILED|Failed|ERROR|Error:|error:|panic:|--- FAIL|AssertionError|[0-9]+ (failing|failed|failures?)|Failures?:|✕|●|not ok|Traceback)" -B 3 -A 5 | head -n 120`
	buildFilter   = `grep -E "(error|Error|ERROR|FAIL|cannot |undefined|[^ ]+:[0-9]+:[0-9]+|[^ ]+\([0-9]+,[0-9]+\))" -B 2 -A 3 | head -n 120`
	installFilter = `grep -Ei "(err!|error:?|warn(ing)?)" -B 1 -A 3 | head -n 100`
)

// hasOptionContaining reports whether any whitespace-separated token in cmd
// both starts with "-" (an option, not a plain argument) and contains
// substr. The tree-cap/du-cap "already bounded" checks below used to
// substring-match the WHOLE command line, which fired on any plain path
// argument that happened to contain the flag's letter -- "du ./old-drafts"
// read "-drafts" as if -d had been passed, silently suppressing the
// advisory on an actually-unbounded command. Scoping the search to
// option-shaped tokens fixes that without parsing each tool's flag syntax.
func hasOptionContaining(cmd, substr string) bool {
	for _, f := range strings.Fields(cmd) {
		if strings.HasPrefix(f, "-") && strings.Contains(f, substr) {
			return true
		}
	}
	return false
}

// duBoundsOutput reports whether any whitespace-separated token in cmd is
// an actual du flag that bounds its output: a short-option cluster
// containing 's' or 'd' ("-s", "-sh", "-d1", ...), or the exact long
// option (optionally with a "=value" suffix) -- checked by exact name for
// "--", never by substring. hasOptionContaining's substring check is safe
// for short options (a single-dash token's letters really are flag
// characters) but not for long ones: an arbitrary --long-option name can
// innocently CONTAIN "s" or "d" as ordinary English-word letters
// (`--exclude=drafts` contains "d" from "exclude"/"drafts" without
// bounding anything), reproducing the exact false-suppression class
// hasOptionContaining was written to close, just shifted from a bare path
// argument into a long option's name or attached value.
func duBoundsOutput(cmd string) bool {
	for _, f := range strings.Fields(cmd) {
		if !strings.HasPrefix(f, "-") {
			continue
		}
		if !strings.HasPrefix(f, "--") {
			// deadeye: a single-dash misspelling of a long option (e.g.
			// "-exclude=x" for "--exclude=x") still reads as a short
			// cluster here, and an unrelated letter in it can still
			// suppress the advisory. ceiling: narrow -- needs a real
			// getopt-style parse of which single letters are actual du
			// short options to close, which this deliberately
			// lightweight, no-flag-syntax-parsing rule doesn't attempt.
			// upgrade: if a real false-suppression from this shape shows
			// up (as opposed to the already-fixed plain-argument and
			// long-option-substring cases above).
			if strings.Contains(f, "s") || strings.Contains(f, "d") {
				return true
			}
			continue
		}
		name, _, _ := strings.Cut(f, "=")
		if name == "--summarize" || name == "--max-depth" {
			return true
		}
	}
	return false
}

var Rules = []Rule{
	{
		Name:           "test-filter",
		Note:           "caps verbose test output to failure context only",
		EstBeforeBytes: 30000, // typical uncapped test-run dump
		EstAfterBytes:  9600,  // ~120 lines * ~80 bytes
		TryRewrite: func(cwd, cmd string) (string, bool) {
			if !testFilterRe.MatchString(cmd) {
				return cmd, false
			}
			return captureThenFilter(cmd, testFilter), true
		},
	},
	{
		Name:           "build-filter",
		Note:           "keeps only build errors, drops successful-build noise",
		EstBeforeBytes: 15000,
		EstAfterBytes:  9600,
		TryRewrite: func(cwd, cmd string) (string, bool) {
			if !buildFilterRe.MatchString(cmd) {
				return cmd, false
			}
			return captureThenFilter(cmd, buildFilter), true
		},
	},
	{
		Name:           "log-tail",
		Note:           "tails large log files instead of dumping them whole",
		EstBeforeBytes: 500000, // rule only fires above the 200KB threshold; this is a typical case, not this file's real size
		EstAfterBytes:  16000,  // ~200 lines * ~80 bytes
		TryRewrite: func(cwd, cmd string) (string, bool) {
			m := catLogRe.FindStringSubmatch(strings.TrimSpace(cmd))
			if m == nil {
				return cmd, false
			}
			// The stat must resolve against the SESSION's cwd, not this
			// process's -- verified live: the daemon serves every project
			// from whichever directory happened to spawn it, so a relative
			// path here previously resolved against an unrelated project
			// and the rule silently never fired (or fired on the wrong
			// file's size). The rewrite itself still emits the original
			// relative path -- the command runs in the session's shell,
			// which does have the right cwd.
			path := m[1]
			statPath := path
			if !filepath.IsAbs(statPath) && cwd != "" {
				statPath = filepath.Join(cwd, statPath)
			}
			fi, err := os.Stat(statPath)
			if err != nil || fi.Size() <= 200*1024 {
				return cmd, false
			}
			return "tail -n 200 " + path, true
		},
	},
	{
		// Never rewritten silently -- a bare `git diff` in a large repo can
		// be huge, but truncating a diff can hide the exact lines the
		// caller needs. Suggest --stat first instead of guessing.
		Name:     "diff-cap",
		Note:     "suggests --stat first for an unscoped git diff",
		Advisory: true,
		TryRewrite: func(cwd, cmd string) (string, bool) {
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
		TryRewrite: func(cwd, cmd string) (string, bool) {
			if !lintFilterRe.MatchString(cmd) {
				return cmd, false
			}
			return captureThenFilter(cmd, `head -n 150`), true
		},
	},
	{
		// Package installs spam progress lines that carry nothing an agent
		// can act on; only errors and deprecation warnings matter. npm 7+'s
		// markers are lowercase ("npm error", "npm warn"); pip's are
		// "ERROR:"/"WARNING:" -- -i covers both without two patterns.
		Name:           "install-filter",
		Note:           "keeps only errors/warnings from package-install output",
		EstBeforeBytes: 20000,
		EstAfterBytes:  2000,
		TryRewrite: func(cwd, cmd string) (string, bool) {
			if !installFilterRe.MatchString(cmd) {
				return cmd, false
			}
			return captureThenFilter(cmd, installFilter), true
		},
	},
	{
		// Pod logs are unbounded app output -- the recent tail is what a
		// debugging agent almost always wants. kubectl's own --tail flag
		// would change the command's argv; wrapping keeps it untouched.
		Name:           "logs-tail",
		Note:           "tails pod logs instead of dumping them whole",
		EstBeforeBytes: 100000,
		EstAfterBytes:  16000,
		TryRewrite: func(cwd, cmd string) (string, bool) {
			if !kubectlLogsRe.MatchString(cmd) {
				return cmd, false
			}
			if strings.Contains(cmd, "--tail") {
				return cmd, false // caller already bounded it
			}
			return captureThenFilter(cmd, `tail -n 200`), true
		},
	},
	{
		// Advisory only -- unlike .log/.out, truncating a structured file
		// (JSON/CSV/YAML) can cut it mid-record, so suggest a targeted tool
		// rather than rewriting. Same 200KB threshold as log-tail.
		Name:     "cat-large",
		Note:     "large structured file -- consider jq, head, or grep instead of cat-ing it whole",
		Advisory: true,
		TryRewrite: func(cwd, cmd string) (string, bool) {
			m := catLargeRe.FindStringSubmatch(strings.TrimSpace(cmd))
			if m == nil {
				return cmd, false
			}
			statPath := m[1]
			if !filepath.IsAbs(statPath) && cwd != "" {
				statPath = filepath.Join(cwd, statPath)
			}
			fi, err := os.Stat(statPath)
			if err != nil || fi.Size() <= 200*1024 {
				return cmd, false
			}
			return cmd, true
		},
	},
	{
		// Advisory, same reasoning as diff-cap: a bare `git log`/`git show`
		// in a long-lived repo dumps unbounded history, but truncating it
		// silently could hide the commit the caller was after.
		Name:     "history-cap",
		Note:     "unbounded git history dump -- consider --oneline -20, or scoping to a path",
		Advisory: true,
		TryRewrite: func(cwd, cmd string) (string, bool) {
			if !bareGitHistRe.MatchString(strings.TrimSpace(cmd)) {
				return cmd, false
			}
			return cmd, true
		},
	},
	// The advisories below share diff-cap's reasoning: each command's
	// output is unbounded (often 10-100KB+), but truncating it silently
	// could hide the exact line the caller needs -- so suggest the
	// bounded form, never rewrite.
	{
		Name:     "tf-plan-cap",
		Note:     "terraform plan dumps the full plan -- consider -target to scope it, or -out=plan.bin then inspect with terraform show",
		Advisory: true,
		TryRewrite: func(cwd, cmd string) (string, bool) {
			return cmd, tfPlanRe.MatchString(strings.TrimSpace(cmd))
		},
	},
	{
		Name:     "kubectl-yaml-cap",
		Note:     "kubectl get -o yaml/json is unbounded -- consider -o name, or -o jsonpath for just the field you need",
		Advisory: true,
		TryRewrite: func(cwd, cmd string) (string, bool) {
			return cmd, kubectlYamlRe.MatchString(strings.TrimSpace(cmd))
		},
	},
	{
		Name:     "npm-ls-cap",
		Note:     "npm ls prints the whole dependency tree -- consider --depth=0",
		Advisory: true,
		TryRewrite: func(cwd, cmd string) (string, bool) {
			return cmd, npmLsRe.MatchString(strings.TrimSpace(cmd))
		},
	},
	{
		Name:     "find-cap",
		Note:     "unscoped find -- consider -maxdepth or -name to bound it",
		Advisory: true,
		TryRewrite: func(cwd, cmd string) (string, bool) {
			return cmd, bareFindRe.MatchString(strings.TrimSpace(cmd))
		},
	},
	{
		Name:     "tree-cap",
		Note:     "tree without -L dumps the whole hierarchy -- consider tree -L 2",
		Advisory: true,
		TryRewrite: func(cwd, cmd string) (string, bool) {
			c := strings.TrimSpace(cmd)
			return cmd, treeRe.MatchString(c) && !hasOptionContaining(c, "L")
		},
	},
	{
		Name:     "du-cap",
		Note:     "du without a depth cap lists every subdirectory -- consider du -d1 or du -sh",
		Advisory: true,
		TryRewrite: func(cwd, cmd string) (string, bool) {
			c := strings.TrimSpace(cmd)
			return cmd, duRe.MatchString(c) && !duBoundsOutput(c)
		},
	},
	{
		Name:     "pkg-list-cap",
		Note:     "full package list -- pipe through grep for the package you care about",
		Advisory: true,
		TryRewrite: func(cwd, cmd string) (string, bool) {
			return cmd, pkgListRe.MatchString(strings.TrimSpace(cmd))
		},
	},
}

// Apply runs the rule table in order against cmd, skipping disabled rule
// names. Returns the zero Rule and applied=false if nothing matched.
// cwd is the session's working directory (needed by log-tail's stat; see
// its comment) -- pass "" when unknown, which just disables that one rule's
// relative-path handling rather than misattributing it to an unrelated cwd.
func Apply(cwd, cmd string, disabled map[string]bool) (rule Rule, newCmd string, applied bool) {
	if !rewritable(cmd) {
		return Rule{}, cmd, false
	}
	for _, r := range Rules {
		if disabled[r.Name] {
			continue
		}
		if out, ok := r.TryRewrite(cwd, cmd); ok {
			return r, out, true
		}
	}
	return Rule{}, cmd, false
}
