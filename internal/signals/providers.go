package signals

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PromptShape is the weakest signal (PLAN.md §3.1: "cap its weight"):
// word count, question density, and complexity/vagueness keyword
// matching. No syntax tree, no model call -- just cheap heuristics.
type PromptShape struct{}

func (PromptShape) Name() string { return "promptshape" }

var complexityWords = []string{
	"architecture", "redesign", "tradeoff", "trade-off", "refactor",
	"migrate", "migration", "rewrite", "scalability", "concurrency",
	"race condition", "across the codebase", "every file", "all files",
}

var vagueWords = []string{
	"somehow", "maybe", "not sure", "figure out", "look into", "investigate",
}

func (PromptShape) Assess(_ context.Context, s Scope) (Evidence, error) {
	prompt := strings.ToLower(strings.TrimSpace(s.Prompt))
	if prompt == "" {
		return Evidence{}, fmt.Errorf("promptshape: no prompt to assess")
	}

	score := 0.0
	for _, w := range complexityWords {
		if strings.Contains(prompt, w) {
			score += 0.15
		}
	}
	for _, w := range vagueWords {
		if strings.Contains(prompt, w) {
			score += 0.1
		}
	}
	score += 0.1 * float64(strings.Count(prompt, "?"))
	wordCount := len(strings.Fields(prompt))
	if wordCount > 60 {
		score += 0.15
	}
	if score > 1 {
		score = 1
	}

	return Evidence{
		Provider:   "promptshape",
		Complexity: score,
		Confidence: 0.35, // weakest signal, capped
		Facts:      map[string]any{"word_count": wordCount},
	}, nil
}

// FileScope reads task complexity off how many files are in scope --
// single-file vs multi-file is one of the cheapest, most reliable signals.
type FileScope struct{}

func (FileScope) Name() string { return "filescope" }

func (FileScope) Assess(_ context.Context, s Scope) (Evidence, error) {
	n := len(s.Files)
	if n == 0 {
		return Evidence{}, fmt.Errorf("filescope: no files in scope")
	}
	var complexity float64
	switch {
	case n == 1:
		complexity = 0.15
	case n <= 3:
		complexity = 0.4
	case n <= 8:
		complexity = 0.65
	default:
		complexity = 0.85
	}
	return Evidence{
		Provider:   "filescope",
		Complexity: complexity,
		Confidence: 0.6,
		Facts:      map[string]any{"file_count": n},
	}, nil
}

// GitChurn counts recent commits touching the files in scope -- a proxy
// for how volatile/risky the area currently is.
type GitChurn struct{}

func (GitChurn) Name() string { return "gitchurn" }

func (GitChurn) Assess(_ context.Context, s Scope) (Evidence, error) {
	if s.Repo == "" || len(s.Files) == 0 {
		return Evidence{}, fmt.Errorf("gitchurn: no repo/files to check")
	}
	args := append([]string{"log", "--since=30.days", "--oneline", "--"}, s.Files...)
	cmd := exec.Command("git", args...)
	cmd.Dir = s.Repo
	out, err := cmd.Output()
	if err != nil {
		return Evidence{}, err
	}
	trimmed := strings.TrimSpace(string(out))
	commits := 0
	if trimmed != "" {
		commits = len(strings.Split(trimmed, "\n"))
	}
	var complexity float64
	switch {
	case commits == 0:
		complexity = 0.1
	case commits <= 3:
		complexity = 0.3
	case commits <= 10:
		complexity = 0.55
	default:
		complexity = 0.8
	}
	return Evidence{
		Provider:   "gitchurn",
		Complexity: complexity,
		Confidence: 0.5,
		Facts:      map[string]any{"commits_last_30d": commits},
	}, nil
}

// TestPresence checks whether files in scope have an adjacent test file.
// Presence lowers the complexity contribution (a safety net exists);
// absence raises it.
type TestPresence struct{}

func (TestPresence) Name() string { return "testpresence" }

func (TestPresence) Assess(_ context.Context, s Scope) (Evidence, error) {
	if len(s.Files) == 0 {
		return Evidence{}, fmt.Errorf("testpresence: no files to check")
	}
	covered := 0
	for _, f := range s.Files {
		if hasAdjacentTest(f) {
			covered++
		}
	}
	ratio := float64(covered) / float64(len(s.Files))
	complexity := 0.6 - 0.5*ratio // 0.1 fully covered .. 0.6 uncovered
	return Evidence{
		Provider:   "testpresence",
		Complexity: complexity,
		Confidence: 0.55,
		Facts:      map[string]any{"files_with_adjacent_test": covered, "files_checked": len(s.Files)},
	}, nil
}

func hasAdjacentTest(path string) bool {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(filepath.Base(path), ext)
	candidates := []string{
		filepath.Join(dir, stem+"_test"+ext),
		filepath.Join(dir, stem+".test"+ext),
		filepath.Join(dir, "test_"+stem+ext),
		filepath.Join(dir, stem+"_spec"+ext),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return true
		}
	}
	return false
}
