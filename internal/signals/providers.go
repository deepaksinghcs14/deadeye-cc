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

	// fuzzyMatch tracks whether a complexity/vague WORD fired -- that's
	// the genuinely uncertain part (translating scattered keyword hits
	// into a numeric magnitude is a guess). Question-mark count and word
	// count are objective, exactly countable facts, not guesses, so they
	// contribute to the complexity score without affecting confidence.
	score := 0.0
	fuzzyMatch := false
	for _, w := range complexityWords {
		if strings.Contains(prompt, w) {
			score += 0.15
			fuzzyMatch = true
		}
	}
	for _, w := range vagueWords {
		if strings.Contains(prompt, w) {
			score += 0.1
			fuzzyMatch = true
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

	// Confidence is about trust in THIS estimate, not in the provider
	// overall: no fuzzy keyword match is a fairly reliable negative result
	// (0.85); once one fires, confidence drops to 0.35, capped as the
	// plan's weakest signal. A high complexity reading still forces the
	// ceiling via kernel.Decide's free-upshift path regardless of
	// confidence, so the cap costs nothing there.
	//
	// This split matters and had two real bugs on the way to it: a flat
	// low confidence made downshifting UNREACHABLE for every real prompt
	// (any nonempty prompt participates, and kernel.Decide requires the
	// MINIMUM confidence across all evidence to clear downshift_threshold).
	// The first fix (confidence keyed to score>0) was still broken --
	// gating on the WHOLE score let one stray "?" (extremely common in
	// ordinary task descriptions) drop confidence to 0.35 with no genuine
	// complexity signal at all. Gating on fuzzyMatch specifically (word
	// hits only) fixes both: caught by reproducing Phase 6's escalation
	// detection live and getting a ceiling decision from a plain,
	// question-mark-terminated "what is 2+2?" delegation that should have
	// downshifted.
	confidence := 0.85
	if fuzzyMatch {
		confidence = 0.35
	}
	return Evidence{
		Provider:   "promptshape",
		Complexity: score,
		Confidence: confidence,
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
		Confidence: 0.85, // a file count is a fact, not a guess
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
		Confidence: 0.82, // a commit count is a fact, same footing as the other measured (non-heuristic) signals
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
		Confidence: 0.8, // file existence is a fact; ratio-to-complexity mapping is the only guess
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
