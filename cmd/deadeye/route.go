package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/gitutil"
	"github.com/deepaksinghcs14/deadeye-cc/internal/kernel"
	"github.com/deepaksinghcs14/deadeye-cc/internal/signals"
)

// gitTimeout aliases gitutil.Timeout for the non-git call sites that
// bound signals.AssessAll contexts -- one number, defined once.
const gitTimeout = gitutil.Timeout

// newScope builds a signals.Scope for prompt at cwd, shared by every call
// site that used to build one ad hoc. Repo is resolved to the git
// TOPLEVEL, not cwd itself -- scopedFiles' `git diff --name-only` always
// reports paths relative to the repo root, never to whatever subdirectory
// a session happens to be in. Verified live: from a subdirectory, `git log
// -- <repo-root-relative-path>` run with cmd.Dir=cwd matches nothing and
// exits 0 (git does not error on a non-matching pathspec) -- gitchurn was
// laundering that into "0 commits, confidence 0.82", the calmest possible
// reading, with high confidence, for a file that might have 40 commits.
func newScope(prompt, cwd string) signals.Scope {
	repo := gitutil.Output(cwd, "rev-parse", "--show-toplevel")
	if repo == "" {
		repo = cwd
	}
	return signals.Scope{Prompt: prompt, Files: scopedFiles(cwd), Repo: repo}
}

// runRoute backs `deadeye route [task description]` / /deadeye-route: a
// dry run of the kernel against either the given description or the
// current working tree's modified/staged files, printing the full
// Decision and each provider's Evidence -- trust requires explainability
// (PLAN.md §6).
func runRoute(taskDescription string) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Println("deadeye route:", err)
		return
	}
	scope := newScope(taskDescription, cwd)
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	evidence := signals.AssessAll(ctx, scope, signals.Builtins())

	cfg := config.Load()
	cat := catalog.Load()
	decision := kernel.Decide(evidence, cat, cfg.DownshiftThreshold)

	if taskDescription != "" {
		fmt.Printf("Task: %q\n", taskDescription)
	} else {
		fmt.Println("Task: (none given -- scoped to current working tree)")
	}
	fmt.Printf("Scope: %d file(s) in %s\n\n", len(scope.Files), scope.Repo)

	if len(evidence) == 0 {
		fmt.Println(cHead("Evidence:") + cDim(" none (every provider skipped -- degrading gracefully is correct per INV-1)"))
	} else {
		fmt.Println(cHead("Evidence"))
		for _, e := range evidence {
			fmt.Printf("  %-14s complexity=%.2f confidence=%.2f facts=%v\n", e.Provider, e.Complexity, e.Confidence, e.Facts)
		}
	}
	fmt.Println()

	fmt.Println(cHead("Decision"))
	fmt.Printf("  model:      %s\n", cValue(decision.Model))
	fmt.Printf("  effort:     %s\n", cValue(decision.Effort))
	fmt.Printf("  confidence: %.2f\n", decision.Confidence)
	fmt.Printf("  reason:     %s\n", cDim(decision.Reason))
	fmt.Printf("\nMode: routing=%s, effort=%s (advise = shown only; enforce = actually rewrites Agent tool calls)\n", cfg.Mode.Routing, cfg.Mode.Effort)
}

func scopedFiles(cwd string) []string {
	out := gitutil.Output(cwd, "diff", "--name-only", "HEAD")
	staged := gitutil.Output(cwd, "diff", "--cached", "--name-only")
	set := map[string]bool{}
	for _, f := range strings.Split(out, "\n") {
		if f != "" {
			set[f] = true
		}
	}
	for _, f := range strings.Split(staged, "\n") {
		if f != "" {
			set[f] = true
		}
	}
	files := make([]string, 0, len(set))
	for f := range set {
		files = append(files, f)
	}
	return files
}
