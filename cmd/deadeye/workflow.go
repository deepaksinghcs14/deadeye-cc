package main

import (
	"strconv"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
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
// The version gate covers PLAN.md §5.5's "environment supports workflows
// (version >= 2.1.154)": clientVersion is parsed client-side from
// CLAUDE_CODE_EXECPATH (verified present in real hook environments) and
// rides proto.Request. Unknown or unparsable versions fail open -- a
// missing signal must never silence an advisory (INV-5). The other half,
// "session model supports xhigh", has no signal anywhere in the hook
// contract (effort.level reports the CURRENT effort, not the model's
// maximum) and fails open by construction.
func decideWorkflowHint(in hookio.Input, cfg config.Config, clientVersion string, state *daemonState) (suggestion string, fired bool) {
	if cfg.Mode.WorkflowHint != "on" {
		return "", false
	}
	if !versionAtLeast(clientVersion, 2, 1, 154) {
		return "", false
	}
	if in.Prompt == "" || isSyntheticPrompt(in.Prompt) || !looksFanOutShaped(in.Prompt) {
		return "", false
	}

	marker := truncatedMarker(in.Prompt)
	if !state.markSuggestedIfFirst(in.SessionID, "workflow:"+marker) {
		return "", false
	}

	state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "UserPromptSubmit", Action: "workflow-suggest", Reason: marker})
	return "deadeye: this looks like fan-out work across many independent units. Consider the `ultracode` keyword to run it as a multi-agent workflow -- it can spend significantly more tokens than a single-agent pass, so it's worth it for genuinely broad work, not routine tasks.", true
}

// versionAtLeast reports whether dotted version v is >= major.minor.patch.
// "" or anything unparsable returns true: fail open, never silence on a
// missing signal.
func versionAtLeast(v string, major, minor, patch int) bool {
	want := [3]int{major, minor, patch}
	parts := strings.SplitN(v, ".", 3)
	for i := 0; i < 3; i++ {
		n := 0
		if i < len(parts) {
			var err error
			if n, err = strconv.Atoi(parts[i]); err != nil {
				return true
			}
		}
		if n != want[i] {
			return n > want[i]
		}
	}
	return true
}
