package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/gitutil"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
	"github.com/deepaksinghcs14/deadeye-cc/internal/receipts"
)

// reviewScopes are the review shapes a receipt can carry. Anything else is
// rejected rather than recorded: a typo'd scope silently polluting the
// accuracy denominator is worse than an error the skill's operator sees.
var reviewScopes = map[string]bool{
	"diff": true, "staged": true, "repo": true, "pr": true, "guard": true,
}

// runReceipt backs `deadeye receipt review --scope <s> [--paths a,b,c]`,
// called by /deadeye-review, /deadeye-pr and /deadeye-guard when a pass
// completes. It records WHERE the review happened -- repo key and the
// base commit its findings were judged against -- so `deadeye misses` can
// later scan forward from that base and ask whether anything came back to
// fix the lines this review passed.
//
// Best-effort by contract, same as the `deadeye lessons record` calls that
// already live in those rubrics: outside a git repo, or with an
// unwritable state dir, it prints one line and exits 0. A review must
// never appear to fail because its bookkeeping did.
func runReceipt(args []string) {
	if len(args) == 0 || args[0] != "review" {
		fmt.Fprintln(os.Stderr, "usage: deadeye receipt review --scope <diff|staged|repo|pr|guard> [--paths a,b,c]")
		os.Exit(2)
	}
	scope, rest := extractFlag(args[1:], "--scope=")
	if scope == "" {
		scope, rest = flagValue(rest, "--scope")
	}
	pathsRaw, rest := extractFlag(rest, "--paths=")
	if pathsRaw == "" {
		pathsRaw, _ = flagValue(rest, "--paths")
	}
	if !reviewScopes[scope] {
		fmt.Fprintln(os.Stderr, "usage: deadeye receipt review --scope <diff|staged|repo|pr|guard> [--paths a,b,c]")
		os.Exit(2)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Println("receipt skipped: no working directory")
		return
	}
	root := gitutil.Root(cwd)
	if root == "" {
		fmt.Println("receipt skipped: not a git repository")
		return
	}
	head := gitutil.Head(root)
	if head == "" {
		fmt.Println("receipt skipped: repository has no commits yet")
		return
	}

	paths := splitPaths(pathsRaw)
	if len(paths) == 0 {
		paths = derivePaths(root, scope)
	}

	r := receipts.Receipt{
		TS:        nowRFC3339(),
		SessionID: os.Getenv("CLAUDE_SESSION_ID"),
		Repo:      gitutil.ProjectKey(cwd),
		Commit:    head,
		Kind:      receipts.KindReview,
		Scope:     scope,
		Paths:     paths,
	}
	if err := receipts.Open(meta.ReceiptsPath()).Append(r); err != nil {
		fmt.Println("receipt skipped:", err)
		return
	}
	fmt.Printf("receipt recorded: %s review at %s (%d path(s))\n", scope, head[:min(8, len(head))], len(paths))
}

// derivePaths asks git which files the reviewed diff touched, when the
// caller didn't say. Always run from the repo root: `git diff
// --name-only` prints root-relative paths, and every later read of them
// has to resolve from the same place.
func derivePaths(root, scope string) []string {
	var out string
	switch scope {
	case "staged":
		out = gitutil.Output(root, "diff", "--cached", "--name-only")
	case "diff", "guard":
		out = gitutil.Output(root, "diff", "--name-only")
	default:
		// "repo" has no single diff, and "pr" lives on a ref this process
		// has no reliable handle on -- the caller passes --paths or the
		// receipt carries none.
		return nil
	}
	return splitPaths(strings.ReplaceAll(out, "\n", ","))
}

func splitPaths(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, gitutil.SanitizeControlBytes(p))
		}
	}
	return out
}

// flagValue pulls a "--flag value" pair (space-separated, unlike
// extractFlag's "--flag=value"), returning the value and args with both
// tokens removed. Both spellings are accepted because these commands are
// typed by a model following a rubric, not by a person reading a usage
// string.
func flagValue(args []string, name string) (string, []string) {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			rest := append(append([]string{}, args[:i]...), args[i+2:]...)
			return args[i+1], rest
		}
	}
	return "", args
}

// writeCoderReceipt records that coder mode was active in this repo at
// this commit, so `deadeye adherence` can later measure the commits that
// landed while it was on.
//
// This is the only reliable answer to "was coder mode active here?" after
// the fact. The mode files under ~/.deadeye are display-only and shared
// mutable state -- lessons.go already documents why a gate reading them
// was removed as "unreliable in both directions" -- and the decision log
// carries no repo or commit at all.
//
// Best-effort and silent: a failed receipt must never affect the hook
// response, which is on the critical path of every session start.
func writeCoderReceipt(cwd, sessionID, level string) {
	if cwd == "" {
		return
	}
	root := gitutil.Root(cwd)
	if root == "" {
		return
	}
	head := gitutil.Head(root)
	if head == "" {
		return
	}
	_ = receipts.Open(meta.ReceiptsPath()).Append(receipts.Receipt{
		TS:        nowRFC3339(),
		SessionID: sessionID,
		Repo:      gitutil.ProjectKey(cwd),
		Commit:    head,
		Kind:      receipts.KindCoder,
		Level:     level,
	})
}
