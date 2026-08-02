# Changelog

## Unreleased

## 0.3.1

- Fix: the daemon's lockfile was treated as stale after 60 seconds
  regardless of whether the daemon was actually still running -- a
  healthy, merely-idle daemon can legitimately go up to 30 minutes
  without writing anything, so its lock was always "stale" by that
  measure. A later spawn attempt would delete the live daemon's
  lockfile, notice the socket already answering, and exit -- deleting
  the lockfile it had just re-created and leaving the still-running
  daemon with no lockfile at all. Staleness now requires the recorded
  pid actually being gone AND nothing answering the socket.
- Fix: `deadeye uninstall` could never actually find the running
  daemon's pid, because of the bug above -- confirmed live, a real
  daemon's lockfile now survives well past the old 60s window, and
  `deadeye uninstall --purge` actually stops the process instead of a
  still-running daemon silently recreating the state dir the purge just
  removed.
- Fix: `deadeye uninstall` signaled whatever pid was in the lockfile
  without checking it was actually a deadeye process -- on Unix,
  `os.FindProcess` never errors regardless of whether that pid exists,
  so a crashed daemon's pid, later recycled by the OS for an unrelated
  process, would get signaled. Now gated on the daemon's socket actually
  answering.
- A session's in-memory dedup state (once-per-session injection, plan
  gate, workflow-suggested markers, routing history) is now evicted at
  SessionEnd, and per-project session summaries are capped at the 3
  most recent -- both previously grew for as long as the daemon or the
  machine stayed up.

## 0.3.0

Per-project config, and routing correctness. The most behaviorally
significant release yet -- several of these are fixes to things that
looked like they worked but didn't, verified live against the real
built binary in each case.

- Fix: `DEADEYE_PREPROCESS=off` / `DEADEYE_GATE=off` did nothing. The
  daemon checked its own (frozen, possibly days-old) environment instead
  of the current client's -- setting either after the daemon had started
  had no effect, while a fresh `deadeye status` process reported them as
  engaged. Kill switches are now read client-side and carried per-request;
  toggling one takes effect on the very next call, no restart needed.
- Fix: one daemon serves every project it's asked about, but config
  (including project-level `.deadeye.json`) was loaded once at daemon
  startup -- one project's config silently governed every other
  project's sessions too, for as long as the daemon stayed up (in
  practice, indefinitely). Config is now loaded fresh per request from
  the session's own directory.
- Fix: thin evidence (e.g. a clean working tree, where most signal
  providers have nothing to assess) was read as evidence of *simplicity*
  rather than *unknown* -- a keyword-free, architecture-sized prompt
  against a freshly-committed tree could downshift to the cheapest model
  tier. Skipped providers are now surfaced as explicit evidence of
  absence, which routes to the ceiling like any other low-confidence
  signal.
- Fix: the ceiling (what "we don't know" resolves to) picked the catalog's
  highest-priced model, not a reasonable default -- three read-only
  search calls with thin evidence were advised the most expensive model
  in the catalog this same session. Now caps at the opus-equivalent tier.
- Fix: `xhigh` effort was unreachable despite being documented and having
  a dedicated upshift path for genuinely very-high-complexity evidence.
- Fix: git-churn and test-presence signals resolved paths against the
  wrong directory when a session's cwd was a subdirectory of its repo --
  a file with real recent commit history could read as "0 commits,
  calmest possible reading, high confidence."
- Fix: the plan gate's implementation-verb detection matched substrings
  ("prefix" matched "fix", "address" matched "add", "changelog" matched
  "change"), firing on pure questions with zero implementation intent.
  Now matches whole words only.
- Fix: the soft plan gate could re-suggest the identical prompt on a
  later turn even after the user had already consented once -- it now
  shares the workflow advisor's once-per-task dedupe.
- Fix: a single escalation permanently raised a task shape's downshift
  threshold to a bar no evidence set could ever clear again, in every
  project, forever. Escalations now expire after 30 days, and a routing
  decision the caller overrode with an explicit model is no longer
  recorded as "applied" in the first place.
- Every git subprocess this plugin shells out to is now bounded by a 2s
  timeout -- a stalled mount or a held `.git/index.lock` no longer wedges
  a daemon goroutine or a CLI command indefinitely.

## 0.2.5

- Fix: build-filter's grep pattern matched none of Go's own compiler error
  text -- a real broken `go build` reached the agent with zero diagnostics.
  Widened to cover `file:line:col`/`file(line,col)` across go/tsc/clang, plus
  `-B 2` for Go's package-header line. Verified against real captured output
  (`internal/preprocess/testdata`), not hand-written approximations.
- Fix: test-filter used after-context only, dropping a Go test-build
  failure's cause (which prints *before* `FAIL`), and matched none of a real
  mocha or pytest failure. Widened with `-B 3` plus AssertionError/"N
  failing"/jest's bullet/tap's "not ok"/pytest's Traceback.
- Fix: a command containing `#`, `&&`, a pipe, a heredoc, or a trailing `\`
  that matched a rewrite rule got corrupted -- a trailing comment swallowed
  the wrapper's own closing syntax (the command never ran at all), and in a
  chain the appended `2>&1` bound to the last command, not the matched one
  (its stderr escaped the capture entirely). Any command with shell
  structure is now left unrewritten; not rewriting is always safe.
- Fix: `log-tail`'s size check resolved a relative path against the
  daemon's own directory rather than the session's, since one daemon
  serves every project. Could silently never fire, or fire against an
  unrelated file's size, depending on which project's hook happened to
  start the daemon.
- Fix: the self-bootstrapped binary always downloaded `releases/latest`
  while comparing its version against `plugin.json` -- when those
  disagreed, it re-downloaded the full binary every session, forever,
  without ever converging. Now pins to the plugin's own released version,
  falling back to latest once if that tag isn't found.
- Fix: a first-ever session's opening burst of tool calls could fire many
  concurrent bootstrap downloads racing to install the same file, with no
  atomicity guarantee on the install itself. The bootstrap now takes its
  own lock (with a stale-lock sweep) and installs via a same-filesystem
  atomic rename.
- The plan gate's hard-layer permission reason and the workflow-hint
  suggestion now carry a `deadeye:` tag like every other surfaced
  decision, instead of reading like an unattributed system message.
- CI now runs on every push and PR (previously only at release-tag time),
  and the release workflow asserts the pushed tag matches `plugin.json`'s
  version before building.

## 0.2.4

- Fix: the self-bootstrapped binary never updated after its first
  install -- `bootstrap.sh` exited immediately if anything already
  existed at `~/.deadeye/bin/deadeye`, so `claude plugin update` would
  bump `plugin.json`'s version and report success while the actual
  binary silently stayed on whatever version first bootstrapped it.
  `bootstrap.sh` now compares the installed binary's `version` output
  against `plugin.json`'s version and re-downloads on a mismatch;
  `deadeye-hook.sh` triggers that check once per session, on
  `SessionStart`, without adding latency to the hot path. Never touches
  a PATH-installed (user-managed) binary.

## 0.2.3

- Fix: `/deadeye-status`, `/deadeye-audit`, and `/deadeye-route` hardcoded
  `deadeye` as the command to run, assuming it resolved on PATH. It never
  does -- the self-bootstrap installs to `~/.deadeye/bin/deadeye` and
  nothing adds that to PATH; only the hook script's own internal
  resolution knew the fallback. All three commands now retry against
  `~/.deadeye/bin/deadeye` before reporting "not bootstrapped".

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
