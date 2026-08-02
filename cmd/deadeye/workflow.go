package main

import (
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
)

// fanOutPhrases mark a task shaped like PLAN.md §5.5's examples: a
// repo-wide audit, an N-file migration, cross-checked research -- many
// independent units, not a single targeted change.
var fanOutPhrases = []string{
	"every file", "all files", "across the codebase", "each package",
	"entire codebase", "repo-wide", "every package", "each file",
	"whole codebase", "audit the codebase", "across every",
}

func looksFanOutShaped(prompt string) bool {
	p := strings.ToLower(prompt)
	for _, phrase := range fanOutPhrases {
		if strings.Contains(p, phrase) {
			return true
		}
	}
	return false
}

// decideWorkflowHint is PLAN.md §5.5: recommend-only (INV-2 -- a plugin
// must never trigger a workflow itself, only ever suggest one), rule-based
// (no learning axis here, "too few, too expensive samples"), at most once
// per task.
//
// ponytail: no version/model-capability gate. PLAN.md §5.5 also wants this
// gated on "environment supports workflows (version >= 2.1.154, session
// model supports xhigh)" -- neither signal is available from hook input
// (no Claude Code version field observed in any captured payload, and
// effort.level reports the CURRENT effort, not the model's maximum
// supported level). Shipping the phrase-based trigger alone; add the
// capability gate if/when a real signal for it turns up.
func decideWorkflowHint(in hookio.Input, state *daemonState) (suggestion string, fired bool) {
	if state.cfg.Mode.WorkflowHint != "on" {
		return "", false
	}
	if in.Prompt == "" || isSyntheticPrompt(in.Prompt) || !looksFanOutShaped(in.Prompt) {
		return "", false
	}

	marker := in.Prompt
	if len(marker) > 60 {
		marker = marker[:60]
	}
	if !state.markWorkflowSuggestedIfFirst(in.SessionID, marker) {
		return "", false
	}

	state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "UserPromptSubmit", Action: "workflow-suggest", Reason: marker})
	return "deadeye: this looks like fan-out work across many independent units. Consider the `ultracode` keyword to run it as a multi-agent workflow -- it can spend significantly more tokens than a single-agent pass, so it's worth it for genuinely broad work, not routine tasks.", true
}
