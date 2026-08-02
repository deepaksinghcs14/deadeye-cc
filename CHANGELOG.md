# Changelog

## Unreleased

### Phase 0 — skeleton & plumbing

- Plugin manifest and marketplace metadata (`.claude-plugin/`).
- Hook registration for `SessionStart`, `UserPromptSubmit`, `PreToolUse`
  (`Bash|Edit|Write|Agent`), `PostToolUse`, `SubagentStart`, `Stop`,
  `SessionEnd` -- all no-ops for now.
- `deadeye` binary: hook client, daemon (unix socket, idle-exits after
  30min), `status`, `capture`, `uninstall [--purge]`.
- Fail-open by construction (INV-5): every hook path recovers from panics
  and prints `{}`; `DEADEYE=off` short-circuits before any daemon dial.
- Decision log at `~/.deadeye/decisions.jsonl` (append-only JSONL, not
  SQLite -- see `docs/verified.md` for why).
- Compiled-in model catalog (`internal/catalog`), regenerable via
  `scripts/gen-catalog.go`, overridable by `~/.deadeye/catalog.json`.
- `/deadeye-status` slash command.
- `docs/verified.md` -- §10 findings recorded against Claude Code v2.1.220.
