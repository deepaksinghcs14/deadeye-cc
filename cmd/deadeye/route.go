package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/gitutil"
	"github.com/deepaksinghcs14/deadeye-cc/internal/kernel"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
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
	files := scopedFiles(cwd)
	if len(files) == 0 {
		// Clean tree: fall back to the files the prompt itself names, so
		// committing your work doesn't blind filescope/gitchurn/
		// testpresence and force every decision to the ceiling. A prompt
		// naming nothing real still yields nothing here, and still gets
		// the unknown-evidence ceiling it should.
		ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
		defer cancel()
		files = signals.FilesNamedIn(ctx, repo, prompt)
	}
	return signals.Scope{Prompt: prompt, Files: files, Repo: repo}
}

// runRoute backs `deadeye route [--subagent-type=<type>] [task description]`
// / /deadeye-route: a dry run of the kernel against either the given
// description or the current working tree's modified/staged files,
// printing the full Decision and each provider's Evidence -- trust requires
// explainability (PLAN.md §6). subagentType mirrors the real Agent call's
// subagent_type (see decideAgentRouting) so the dry run can never show a
// different outcome than a real call would get once that field matters to
// a signal (signals.SubagentKind).
func runRoute(taskDescription, subagentType string) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Println("deadeye route:", err)
		return
	}
	scope := newScope(taskDescription, cwd)
	scope.SubagentType = subagentType
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	evidence := signals.AssessAll(ctx, scope, signals.Builtins())

	cfg := config.Load()
	cat := catalog.Load()
	// Same ADJUSTED threshold the live path uses (decideAgentRouting):
	// reading cfg.DownshiftThreshold raw here meant any shape carrying a
	// recorded escalation explained itself with a threshold the real call
	// would never use -- the dry run silently disagreed with the thing it
	// exists to explain.
	outcomes, _ := lessons.Scan(meta.OutcomesPath())
	shape := taskShapeKey(scope.Files, scope.Prompt, evidence)
	threshold := lessons.AdjustedDownshiftThreshold(cfg.DownshiftThreshold, outcomes, shape, time.Now())
	decision := kernel.Decide(evidence, cat, threshold)
	// Same judge path decideAgentRouting uses -- a real Agent call gets
	// this too when mode.routing_judge=on, so the dry-run must show it or
	// the explanation can silently diverge from the real decision.
	// wait=true: this is a CLI dry-run, not a hook -- it can afford to wait
	// for the verdict (the hook path returns the heuristic and judges async).
	decision = applyRoutingJudge(cfg, decision, cat, scope.Prompt)

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
	// Show the threshold the decision was actually gated on, and say so
	// when recorded escalations raised it above the configured base --
	// otherwise "confidence 0.80" next to a ceiling decision reads as a
	// contradiction with no way to see why.
	if threshold != cfg.DownshiftThreshold {
		fmt.Printf("  threshold:  %.2f %s\n", threshold,
			cDim(fmt.Sprintf("(base %.2f, raised by recorded escalations for shape %s)", cfg.DownshiftThreshold, shape)))
	} else {
		fmt.Printf("  threshold:  %.2f\n", threshold)
	}
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
