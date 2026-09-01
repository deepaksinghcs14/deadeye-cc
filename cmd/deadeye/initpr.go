package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/prreview"
)

// The /deadeye-pr on-demand review command, rendered into each non-Claude
// host's native command surface. Claude Code gets it as a shipped skill
// (skills/deadeye-pr/SKILL.md) and needs no init. Every rendering is the
// canonical prreview.Body() wrapped in a host-specific header that passes
// the PR argument in that host's syntax -- so all hosts run the identical
// rubric. EXPERIMENTAL for every host here: these command surfaces are
// documented but unverified on a live install (verified.md §14).
//
// Placement is project-local for gemini/cursor/windsurf (their documented
// project command dirs, matching where those inits already write) and
// ~/.codex/prompts for codex (Codex's only prompt location, where init
// codex already writes). Each is a NEW deadeye-owned file guarded by the
// never-clobber marker -- deadeye never edits a host's own config file.

// prCommandPath returns the on-demand PR-review command file for host, or
// ok=false for an unknown host. cwd is the project root (project-local
// hosts); home is the user's home dir (codex).
func prCommandPath(host, cwd, home string) (string, bool) {
	switch host {
	case "codex":
		return filepath.Join(home, ".codex", "prompts", "deadeye-pr.md"), true
	case "gemini":
		return filepath.Join(cwd, ".gemini", "commands", "deadeye-pr.toml"), true
	case "cursor":
		return filepath.Join(cwd, ".cursor", "skills", "deadeye-pr", "SKILL.md"), true
	case "windsurf":
		return filepath.Join(cwd, ".windsurf", "workflows", "deadeye-pr.md"), true
	}
	return "", false
}

// renderPRCommand wraps the canonical rubric in host's command-file format,
// substituting the PR-argument token in that host's syntax.
func renderPRCommand(host string) string {
	const desc = "deadeye PR review -- over-engineering, correctness, performance, security (experimental)"
	body := prreview.Body()
	switch host {
	case "codex":
		return "---\ndescription: " + desc + "\nargument-hint: \"[<PR number or URL>] [--post]\"\n---\n\n" +
			"Target PR: $ARGUMENTS  (a PR number or URL; if empty, the current branch's PR.)\n\n" + body
	case "gemini":
		// TOML literal string ('''): no escape processing, so backticks,
		// backslashes, and quotes in the rubric pass through verbatim. The
		// rubric contains no ''' sequence (a size/marker test guards its shape).
		return "description = \"" + desc + "\"\nprompt = '''\n" +
			"Target PR: {{args}}  (a PR number or URL; if empty, the current branch's PR.)\n\n" + body + "\n'''\n"
	case "cursor":
		return "---\nname: deadeye-pr\ndescription: " + desc + "\ndisable-model-invocation: true\n---\n\n" +
			"Target PR: the PR number or URL in the user's message (if none, the current branch's PR).\n\n" + body
	case "windsurf":
		// Windsurf workflows cap at 12000 chars; use the trimmed rubric.
		return "---\ndescription: " + desc + "\n---\n\n" +
			"Target PR: the PR number or URL in the user's message (if none, the current branch's PR).\n\n" + prreview.WindsurfBody()
	}
	return ""
}

// writePRCommand installs host's PR-review command file. It refuses to
// overwrite a file that isn't deadeye's (no marker), returns the path it
// wrote, and a non-nil error only on a real filesystem failure or a
// foreign-file collision -- callers warn but do not abort the wider init.
func writePRCommand(host string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	path, ok := prCommandPath(host, cwd, home)
	if !ok {
		return "", fmt.Errorf("no PR-review command surface for %s", host)
	}
	if b, err := os.ReadFile(path); err == nil && !strings.Contains(string(b), prreview.Marker) {
		return path, fmt.Errorf("%s exists and isn't deadeye's -- not overwriting", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return path, err
	}
	if err := os.WriteFile(path, []byte(renderPRCommand(host)), 0o644); err != nil {
		return path, err
	}
	return path, nil
}

// installPRCommand writes host's PR-review command and reports the result.
// A failure is a warning, not a fatal -- the wider `init <host>` (hooks,
// persona) has already succeeded and shouldn't be undone by this.
func installPRCommand(host string) {
	if path, err := writePRCommand(host); err != nil {
		fmt.Println(cWarn("PR-review command skipped: ") + err.Error())
	} else {
		fmt.Println(cGood("Wrote") + " PR-review command " + cValue(path) + cDim("  (/deadeye-pr -- experimental)"))
	}
}

// removePRCommand deletes host's PR-review command file if it still carries
// deadeye's marker (never a user's own file). For cursor it removes the
// whole per-skill directory it created.
func removePRCommand(host string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	path, ok := prCommandPath(host, cwd, home)
	if !ok {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), prreview.Marker) {
		return
	}
	if host == "cursor" {
		os.RemoveAll(filepath.Dir(path)) // the deadeye-pr/ skill dir
		return
	}
	os.Remove(path)
}
