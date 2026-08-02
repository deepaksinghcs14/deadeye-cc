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

### Phase 1 — output preprocessing, session injection, explore skill

- Five exit-code-safe Bash rewrite rules (test/build/log/diff/lint-filter),
  each with a golden test, a must-not-rewrite test, and an exit-code
  regression test.
- Once-per-session `UserPromptSubmit` advisory injection (tier table,
  effort/workflow guidance, anti-waste rules) -- replaces the SessionStart
  mechanism PLAN.md §5.1 specified, which does not work in Claude Code
  v2.1.220 (see `docs/verified.md`).
- `explore` skill (forked, progressive-disclosure exploration).
- `/deadeye-audit` command, reporting only measured decision-log data.

### Phase 1.5 — cross-session memory

- Compact per-project summary written at `SessionEnd`, injected at the
  next session's first `UserPromptSubmit` with a 30s freshness guard.

### Phase 2 — policy kernel, signal providers, `/deadeye-route`

- `internal/kernel.Decide`: conservative-by-construction grid search with
  INV-1 property tests.
- Four built-in signal providers (promptshape, filescope, gitchurn,
  testpresence).
- `/deadeye-route` dry-run command; `/deadeye-status` surfaces
  `CLAUDE_EFFORT` pinning.

### Phase 3 — subagent model routing

- `PreToolUse/Agent`: advise (default) or enforce mode, rewriting
  `tool_input.model` to the kernel's decision. Never overrides an
  explicit caller request.
- Found and fixed live: `updatedInput` does not have merge semantics --
  `hookio.MergeToolInput` now merges client-side before every rewrite.

### Phase 4 — plan gate

- Soft layer (`UserPromptSubmit`): plan-first suggestion on
  vague/multi-file implementation-shaped prompts.
- Hard layer (`PreToolUse/Edit|Write`, opt-in): asks for permission when a
  plan is pending; clears itself after asking once (no hook surface
  reports back the user's answer).

### Phase 5 — workflow advisor

- Fan-out phrasing on an implementation/audit-shaped prompt gets a
  one-line `ultracode` suggestion, at most once per task. Recommend-only.

### Phase 6 — learning loop

- Escalation detection (the one outcome signal the current hook surfaces
  can actually observe): a caller requesting a higher-tier model than
  deadeye's last recommendation raises the downshift threshold for that
  task shape going forward.
- Found and fixed live: signal-provider confidence calibration made
  downshifting unreachable through the kernel for nearly all real usage
  (see `docs/verified.md` V11).
- `/deadeye-audit` reports escalation counts per task shape.

### Distribution

- GitHub Pages site (`docs/site/`) and release workflow
  (`.github/workflows/`).
