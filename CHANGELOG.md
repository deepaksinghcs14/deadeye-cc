# Changelog

## Unreleased

## 0.2.2

- Fix: the plan gate and workflow-hint advisor could fire on a synthetic
  `UserPromptSubmit` -- a background subagent's task-completion
  notification is delivered through a genuine `UserPromptSubmit` hook
  event carrying raw `<task-notification>...` text, which the keyword
  heuristics matched as if a user had typed an implementation request.
  Caught by running a real multi-step task with subagent delegation, not
  a synthetic test. Both advisors now skip any prompt that isn't
  user-typed.
- README/site: published a real end-to-end task's `/deadeye-audit` log
  (add a method + test, verified by `go build`/`go test`, delegated to a
  subagent) alongside the existing measured before/after numbers.

## 0.2.1

- Fix: `Version` was declared as a `const`, so goreleaser's
  `-ldflags -X .../meta.Version=...` silently never overrode it -- every
  v0.1.0 and v0.2.0 release binary reported its compiled-in dev version
  string via `deadeye version` instead of the real tag. `-ldflags -X` can
  only patch a package-level string variable; it fails silently on a
  const. Fixed by making `Version` a `var`; added a regression test that
  builds a throwaway binary with the same ldflags and asserts the
  override actually lands.

## 0.2.0

- Per-session savings summary: a single terse line via `Stop`'s
  `additionalContext` when new preprocessing savings have accrued since
  the last turn (`deadeye: ~N bytes kept out of context this session (M
  rewrites).`), shown at most once per change so a stale total never
  repeats.
- Site: logo mark added to the nav.
- README/site: the before/after now cites two separate real measurements
  (485→99 bytes / 79.6% on a small suite with a real failure, 10,301→55
  bytes / 99.5% on this repo's own full passing suite) instead of one
  number, with a badge stating the range rather than a blended average.

## 0.1.0

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
