package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/prreview"
)

// The on-demand review commands/skills, rendered into each non-Claude host's
// native surface: PR review (prCmd) and the working-diff/whole-repo
// self-review (reviewCmd). Claude Code gets both as shipped skills
// (skills/deadeye-pr/SKILL.md, skills/deadeye-review/SKILL.md) and needs no
// init. Every rendering is the canonical prreview.Body()/SelfBody() wrapped
// in a host-specific header -- so all hosts run the identical rubric.
// EXPERIMENTAL for every host here: these command surfaces are documented
// but unverified on a live install (verified.md §14).
//
// Placement is project-local for gemini/cursor/windsurf (their documented
// project command dirs, matching where those inits already write) and
// ~/.agents/skills for codex. Each is a NEW deadeye-owned file guarded by the
// never-clobber marker -- deadeye never edits a host's own config file.

// hostCmd is one review command's cross-host rendering recipe -- the axis
// that varies (deadeye-pr vs deadeye-review) factored out of the host axis
// (codex/gemini/cursor/windsurf) so writeCommand/installCommand/
// removeCommand stay one implementation each instead of two near-identical
// copies.
type hostCmd struct {
	name         string                     // "deadeye-pr" / "deadeye-review" -- file/skill name
	desc         string                     // one-line description in host frontmatter
	argHint      string                     // codex/cursor argument-hint value
	marker       string                     // never-clobber sentinel, unique per command
	body         func() string              // full rubric for codex/gemini/cursor
	windsurfBody func() string              // trimmed rubric for windsurf's 12000-char cap
	leadLine     func(host string) string   // host-specific lead-in before the body
	kind         string                     // printed noun, e.g. "PR-review"
	trigger      string                     // printed invocation hint, e.g. "/deadeye-pr -- experimental"
	legacyPaths  func(home string) []string // extra codex paths to clean up on uninstall, or nil
}

var prCmd = hostCmd{
	name:         "deadeye-pr",
	desc:         "deadeye PR review -- over-engineering, correctness, performance, security (experimental)",
	argHint:      "[<PR number or URL>] [--post]",
	marker:       prreview.Marker,
	body:         prreview.Body,
	windsurfBody: prreview.WindsurfBody,
	kind:         "PR-review",
	trigger:      "/deadeye-pr -- experimental",
	leadLine: func(host string) string {
		switch host {
		case "codex":
			return "Target PR: the PR number or URL in the user's prompt (if none, the current branch's PR).\n\n"
		case "gemini":
			return "Target PR: {{args}}  (a PR number or URL; if empty, the current branch's PR.)\n\n"
		case "cursor", "windsurf":
			return "Target PR: the PR number or URL in the user's message (if none, the current branch's PR).\n\n"
		}
		return ""
	},
	legacyPaths: legacyCodexPRCommandPaths,
}

var reviewCmd = hostCmd{
	name:         "deadeye-review",
	desc:         "deadeye self-review -- over-engineering, correctness, performance, security, working diff or --repo (experimental)",
	argHint:      "[--repo]",
	marker:       prreview.SelfMarker,
	body:         prreview.SelfBody,
	windsurfBody: prreview.SelfWindsurfBody,
	kind:         "self-review",
	trigger:      "/deadeye-review -- experimental",
	leadLine: func(host string) string {
		switch host {
		case "codex":
			return "Scope: the working diff by default. If the user's prompt includes --repo (or asks to review the whole repo), use whole-repo mode instead.\n\n"
		case "gemini":
			return "Scope: the working diff by default. Args: {{args}} -- if they include --repo (or the user asks to review the whole repo), use whole-repo mode instead.\n\n"
		case "cursor", "windsurf":
			return "Scope: the working diff by default. If the user's message includes --repo (or asks to review the whole repo), use whole-repo mode instead.\n\n"
		}
		return ""
	},
}

// commandPath returns cmd's command file for host, or ok=false for an
// unknown host. cwd is the project root (project-local hosts); home is the
// user's home dir (codex).
func commandPath(cmd hostCmd, host, cwd, home string) (string, bool) {
	switch host {
	case "codex":
		return filepath.Join(home, ".agents", "skills", cmd.name, "SKILL.md"), true
	case "gemini":
		return filepath.Join(cwd, ".gemini", "commands", cmd.name+".toml"), true
	case "cursor":
		return filepath.Join(cwd, ".cursor", "skills", cmd.name, "SKILL.md"), true
	case "windsurf":
		return filepath.Join(cwd, ".windsurf", "workflows", cmd.name+".md"), true
	}
	return "", false
}

func legacyCodexPRCommandPaths(home string) []string {
	paths := []string{filepath.Join(home, ".codex", "prompts", "deadeye-pr.md")}
	if codexHome := os.Getenv("CODEX_HOME"); codexHome != "" {
		path := filepath.Join(codexHome, "prompts", "deadeye-pr.md")
		if path != paths[0] {
			paths = append(paths, path)
		}
	}
	return paths
}

// renderCommand wraps cmd's canonical rubric in host's command-file format,
// substituting the argument token in that host's syntax.
func renderCommand(cmd hostCmd, host string) string {
	switch host {
	case "codex":
		return "---\nname: " + cmd.name + "\ndescription: " + cmd.desc + "\nlicense: MIT\nargument-hint: \"" + cmd.argHint + "\"\n---\n\n" +
			cmd.leadLine(host) + cmd.body()
	case "gemini":
		// TOML literal string ('''): no escape processing, so backticks,
		// backslashes, and quotes in the rubric pass through verbatim. The
		// rubric contains no ''' sequence (a size/marker test guards its shape).
		return "description = \"" + cmd.desc + "\"\nprompt = '''\n" +
			cmd.leadLine(host) + cmd.body() + "\n'''\n"
	case "cursor":
		return "---\nname: " + cmd.name + "\ndescription: " + cmd.desc + "\ndisable-model-invocation: true\n---\n\n" +
			cmd.leadLine(host) + cmd.body()
	case "windsurf":
		// Windsurf workflows cap at 12000 chars; use the trimmed rubric.
		return "---\ndescription: " + cmd.desc + "\n---\n\n" +
			cmd.leadLine(host) + cmd.windsurfBody()
	}
	return ""
}

// writeCommand installs host's command file for cmd. It refuses to
// overwrite a file that isn't deadeye's (no marker), returns the path it
// wrote, and a non-nil error only on a real filesystem failure or a
// foreign-file collision -- callers warn but do not abort the wider init.
func writeCommand(cmd hostCmd, host string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	path, ok := commandPath(cmd, host, cwd, home)
	if !ok {
		return "", fmt.Errorf("no %s command surface for %s", cmd.kind, host)
	}
	if b, err := os.ReadFile(path); err == nil && !strings.Contains(string(b), cmd.marker) {
		return path, fmt.Errorf("%s exists and isn't deadeye's -- not overwriting", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return path, err
	}
	if err := os.WriteFile(path, []byte(renderCommand(cmd, host)), 0o644); err != nil {
		return path, err
	}
	return path, nil
}

// installCommand writes host's command file for cmd and reports the
// result. A failure is a warning, not a fatal -- the wider `init <host>`
// (hooks, persona) has already succeeded and shouldn't be undone by this.
func installCommand(cmd hostCmd, host string) {
	if path, err := writeCommand(cmd, host); err != nil {
		fmt.Println(cWarn(cmd.kind+" command skipped: ") + err.Error())
	} else {
		kind := cmd.kind + " command"
		trigger := "  (" + cmd.trigger + ")"
		if host == "codex" {
			kind = cmd.kind + " skill"
			trigger = "  ($" + cmd.name + " -- experimental)"
		}
		fmt.Println(cGood("Wrote") + " " + kind + " " + cValue(path) + cDim(trigger))
	}
}

func removeDeadeyeCommandFile(cmd hostCmd, path string) bool {
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), cmd.marker) {
		return false
	}
	return os.Remove(path) == nil
}

// removeCommand deletes host's command file for cmd if it still carries
// deadeye's marker (never a user's own file). For cursor it removes the
// whole per-skill directory it created.
func removeCommand(cmd hostCmd, host string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	path, ok := commandPath(cmd, host, cwd, home)
	if !ok {
		return
	}
	removed := removeDeadeyeCommandFile(cmd, path)
	if host == "cursor" {
		if removed {
			os.RemoveAll(filepath.Dir(path)) // the per-skill dir
		}
		return
	}
	if host == "codex" {
		if removed {
			os.Remove(filepath.Dir(path)) // only succeeds when the skill dir is empty
		}
		if cmd.legacyPaths != nil {
			for _, legacy := range cmd.legacyPaths(home) {
				removeDeadeyeCommandFile(cmd, legacy)
			}
		}
	}
}
