package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/kernel"
	"github.com/deepaksinghcs14/deadeye-cc/internal/signals"
)

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
	repo := gitOut(cwd, "rev-parse", "--show-toplevel")
	if repo == "" {
		repo = cwd
	}
	files := scopedFiles(cwd)

	scope := signals.Scope{Prompt: taskDescription, Files: files, Repo: repo}
	evidence := signals.AssessAll(context.Background(), scope, signals.Builtins())

	cfg := config.Load()
	cat := catalog.Load()
	decision := kernel.Decide(evidence, cat, cfg.DownshiftThreshold)

	if taskDescription != "" {
		fmt.Printf("Task: %q\n", taskDescription)
	} else {
		fmt.Println("Task: (none given -- scoped to current working tree)")
	}
	fmt.Printf("Scope: %d file(s) in %s\n\n", len(files), repo)

	if len(evidence) == 0 {
		fmt.Println("Evidence: none (every provider skipped -- see docs/verified.md/PLAN.md §3.1 on degrading gracefully)")
	} else {
		fmt.Println("Evidence:")
		for _, e := range evidence {
			fmt.Printf("  %-14s complexity=%.2f confidence=%.2f facts=%v\n", e.Provider, e.Complexity, e.Confidence, e.Facts)
		}
	}
	fmt.Println()

	fmt.Println("Decision:")
	fmt.Printf("  model:      %s\n", decision.Model)
	fmt.Printf("  effort:     %s\n", decision.Effort)
	fmt.Printf("  confidence: %.2f\n", decision.Confidence)
	fmt.Printf("  reason:     %s\n", decision.Reason)
	fmt.Printf("\nMode: routing=%s, effort=%s (advise = shown only; enforce = actually rewrites Agent tool calls)\n", cfg.Mode.Routing, cfg.Mode.Effort)
}

func scopedFiles(cwd string) []string {
	out := gitOut(cwd, "diff", "--name-only", "HEAD")
	staged := gitOut(cwd, "diff", "--cached", "--name-only")
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

func gitOut(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
