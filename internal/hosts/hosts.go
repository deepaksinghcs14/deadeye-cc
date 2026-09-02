// Package hosts encodes the per-host capability differences deadeye's
// deciders branch on. The full engine assumes Claude Code's hook surface;
// Codex and Gemini CLI are REDUCED hosts -- no Claude Agent-tool surface
// to route models or suggest workflows on, and persona/context injected via
// hookSpecificOutput.additionalContext rather than Claude's raw-stdout
// SessionStart. Keeping the host checks here (not scattered `host ==
// "codex"` string literals) means adding a reduced host is one place, and
// each call site reads its actual reason.
package hosts

// isClaude reports whether host is Claude Code (the empty/"claude" default).
// The two predicates below coincide on it today; they're kept separate so a
// future host with only one of the capabilities reads correctly.
func isClaude(host string) bool { return host == "" || host == "claude" }

// HasSubagentSurface reports whether host exposes Claude's Agent/subagent tool
// that deadeye's model routing, tier guidance, and workflow hint target.
// Codex has a SubagentStart lifecycle hook, but not this Agent tool contract.
func HasSubagentSurface(host string) bool { return isClaude(host) }

// UsesRawInjection reports whether host delivers SessionStart context as
// raw stdout (Claude Code). Codex and Gemini take it via
// hookSpecificOutput.additionalContext instead.
func UsesRawInjection(host string) bool { return isClaude(host) }
