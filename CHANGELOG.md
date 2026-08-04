# Changelog

## Unreleased

## 0.10.0

Everything from the second deep sweep: five reliability fixes, the truth
drift, and three product gaps.

Reliability:
- Daemon lock acquisition no longer has a two-winners race: a fresh empty
  lock is a daemon mid-acquisition, not a corpse.
- The statusline nudge's once-ever contract now holds under concurrent
  session starts (atomic O_EXCL claim instead of stat-then-write).
- Daemon requests are capped at 8 MB (`io.LimitReader`) -- one oversized
  payload can't balloon the shared daemon.
- Long `$HOME` no longer silently defeats the daemon: past the ~104-byte
  unix-socket path limit, the socket falls back to a deterministic
  uid-keyed path under the temp dir.
- `deadeye uninstall` refuses to remove state while the daemon is still
  alive, instead of reporting success and orphaning it.

Product:
- **`/deadeye-mute [off]`** -- session-scoped mute for advisories,
  plan-gate nags, and workflow hints. Silent rewrites keep working; a
  `plan_gate: hard` setting keeps enforcing.
- **`preprocess.disabled_rules` now also covers `grep-limit`,
  `read-advice`, and `repeat-command`** -- disable one advisory without
  killing all of preprocess.
- **`deadeye lessons [reset]`** -- inspect or clear the recorded routing
  outcomes that bias future decisions.
- The statusline shows `[DEADEYE:OFF]` / `[DEADEYE:CODER OFF]` when a
  kill switch is active -- silence is no longer ambiguous.
- Response-size observation is now gated by `mode.preprocess`, closing a
  gap in the "every axis independently switchable" guarantee.

Truth:
- `/deadeye-gain` no longer labels large Read/Grep/Glob/WebFetch
  responses as "MCP observed" -- two separately labeled streams -- and
  its Advisories caption now matches what's actually counted.
- README/site subagent-card numbers re-measured after 0.8.0's ruleset
  growth: 6,029 -> 887 bytes per spawn (85.3%).
- Stale comments pointing at the superseded SessionStart SystemMessage
  mechanism now point at raw stdout (verified.md §11); §5.1 carries a
  supersession banner; the schema names every disableable rule.

## 0.9.1

- **Fix: 0.9.0's Grep advisory and Read/Grep/Glob/WebFetch/WebSearch
  observation never fired** -- the hook matchers in hooks.json were
  never widened to send those tools' events to the daemon, so both
  features were dead on arrival (unit-tested, but the hook wiring wasn't
  live-verified). PreToolUse now matches Grep; PostToolUse matches the
  five observed tools. Found by a reliability sweep, verified live this
  time.

## 0.9.0

Batch 3: wider observation, new advisories.

- **PostToolUse now observes Read/Grep/Glob/WebFetch/WebSearch response
  sizes** (only responses over 8 KB -- outliers are the evidence, small
  responses would just be log noise). Observation-only, never touches
  context; this is the measured base future rules get justified from.
- **New Grep advisory**: content-mode Grep with no `head_limit` gets one
  per-session nudge toward `head_limit` or `files_with_matches`.
- **Bare-git advisories widened**: `git diff HEAD`/`--cached`/`--staged`
  and `git log -p`/`--stat`/`--oneline` (no `-n`) now get the same
  diff-cap/history-cap nudges as the exactly-bare forms.
- **Seven new unbounded-output advisories**: `terraform plan`,
  `kubectl get -o yaml|json`, `npm ls`, bare `find`, `tree` without
  `-L`, `du` without a depth cap, `pip list`/`brew list`. All advisory
  -- truncating these silently could hide the exact line needed.

## 0.8.0

Batch 2: the coder persona learns comment discipline.

- **New "Comments and docs" section in the ruleset.** The terseness ethos
  now explicitly governs the response, never the code's why-comments --
  the persona comments the constraint/tradeoff the code can't show,
  renames before annotating, deletes comments that restate the next
  line, and gives exported functions a one-line contract doc.
- **The `deadeye:` marker grammar is pinned**: `deadeye: <shortcut>.
  ceiling: <limit>. upgrade: <trigger>.` -- literal keywords, greppable,
  so `/deadeye-debt` parses the ledger reliably (freeform legacy markers
  still count, best-effort). `TODO` vs `deadeye:` is now spelled out.
- Commit/PR discipline: subjects say why (the diff shows what); bodies
  follow the skipped-X-add-when-Y shape.
- Three passages the ruleset said twice (read-before-cut, the off
  switch, shortest-diff) are now said once; net injection is ~6.0 KB
  (was ~5.3 KB), guarded by a new size-budget test.
- The subagent card carries the pinned grammar and a one-line comment
  rule too.

## 0.7.0

Batch 1 of the deep-sweep follow-up: correctness + the biggest token wins.

- **Subagents now get a condensed persona card (~0.7 KB) instead of the
  full ~5.3 KB ruleset** -- the ladder, the `deadeye:` marker convention,
  the output shape, and the never-cut list travel; the worked examples
  and long-session prose don't. The cut is logged per spawn
  (`coder-subagent` bytes_after), so `/deadeye-gain` shows it.
- **The Agent-routing path's git calls are now bounded by the same 2s
  timeout as every other git call site** -- a stalled network mount or a
  held `.git/index.lock` fails open into "unknown" evidence instead of
  hanging the hook.
- **Noop records are no longer written to the decision log.** They were
  ~95% of all rows and carried no reporting value; logged totals in
  `/deadeye-audit` and `deadeye status` will drop accordingly, and the
  session-memory "meaningful activity" check stops being inflated by them.
- Dead code removed (unused effort helpers, an unused exported wrapper,
  hand-rolled map-key sorts replaced with stdlib `slices.Sorted`); the
  subagent matcher regex is compiled once, not per spawn.

## 0.6.1

- Configs seeded before coder mode existed (pre-0.5.0) now gain the
  `coder` block with its defaults spelled out on the next daemon start,
  so the off switch (`"default_level": "off"`) is visible in the file
  users open to tweak. The setting always applied; only its visibility
  was missing. A config that already has any `coder` key -- or is
  malformed -- is left untouched.

## 0.6.0

All four `deadeye:` debt markers paid down:

- **Per-session statusline badges.** The statusline script now reads the
  session_id Claude Code pipes to it on stdin and picks that session's own
  mode file, so two concurrent sessions at different coder levels each
  show their own badge instead of last-writer-wins. The global file
  remains as a fallback; stale per-session files are swept after 48h.
- **`CLAUDE_CONFIG_DIR` honored.** The statusline nudge now checks
  settings.json under the client's real config dir (carried over the
  wire -- the daemon still never reads env), not a hardcoded `~/.claude`.
- **Workflow hint gated on client version.** The Claude Code version is
  parsed from `CLAUDE_CODE_EXECPATH` in the hook environment (verified
  live) and rides the request; clients older than 2.1.154 -- which lack
  the Workflow tool -- no longer get the ultracode hint. Unknown versions
  fail open.
- **Session memory stands down on native restore.** When SessionStart
  reports source `resume` or `compact`, Claude Code itself restored the
  session's context, so the next-prompt injection skips the session-memory
  paragraph instead of repeating what's already there (PLAN.md §5.7).

## 0.5.3

- CLI colors now honor the standard `FORCE_COLOR` convention, not just a
  TTY check -- Claude Code sets `FORCE_COLOR=3` in its Bash sessions and
  renders ANSI in the output pane, so `/deadeye-status`, `/deadeye-gain`,
  and friends are colored inside Claude Code too, not only in a raw
  terminal. `NO_COLOR` still wins; plain pipes stay plain.

## 0.5.2

- CLI output (`status`, `gain`, `audit`, `route`) is now colored when
  stdout is a terminal: brass section headers, green for on/up/measured,
  amber for estimates and cautions, red for OFF/down, dim annotations.
  Piped output (and NO_COLOR) stays plain bytes, so scripts and tests
  are unaffected. Injected suggestions stay uncolored by design -- they
  are model-context text, not terminal output.
- Every skill and command description is now a single line, so the
  command picker shows them in full instead of truncating.

## 0.5.1

- Fix: the first session after a daemon exit (boot, 30-minute idle
  timeout, `deadeye uninstall`) silently ran without the coder persona --
  SessionStart hit the dead socket, failed open, and the injection was
  lost for that whole session. Caught live: a real fresh session
  answered "Unknown" to its own coder level; the next one answered
  correctly. SessionStart now waits briefly (up to 2s, well inside its
  5s hook timeout) for the daemon it just spawned and retries once.
  Hot-path events (PreToolUse etc.) still fail open immediately --
  a regression test pins both sides of that boundary.

## 0.5.0

Coder mode: a lean-first coding persona, built Go-native into the
daemon. (Portions adapted from an MIT-licensed work -- notice in
THIRD-PARTY.md.)

- **On by default at level `marksman`.** The persona ruleset is injected
  at every session start -- including after compaction -- and travels
  into subagents. Three intensity levels: `spotter` (names the leaner
  alternative, builds what's asked), `marksman` (the lean-first ladder
  enforced -- default), `sniper` (maximum minimalism). lite/full/ultra
  are accepted as hidden input aliases.
- Switch with `/deadeye-coder spotter|marksman|sniper|off`; report with
  bare `/deadeye-coder`; persist a default with `/deadeye-coder default
  <level>` (writes deadeye's own `~/.deadeye/config.json`, never
  Claude's settings). Say exactly `normal mode` or `stop coder` to turn
  it off mid-session -- whole-message match only, so mentioning those
  words inside a task never kills the mode.
- New skills: `/deadeye-review` (diff over-engineering review),
  `/deadeye-sweep` (repo-wide audit), `/deadeye-debt` (ledger of
  `deadeye:` shortcut markers), `/deadeye-gain` (measured-impact
  scoreboard -- real bytes labeled as measured, estimates labeled as
  estimates, never an invented per-repo %), `/deadeye-help` (reference
  card). New CLI: `deadeye gain`.
- Optional statusline badge (`hooks/deadeye-statusline.sh`) shows the
  live level; deadeye offers the setup once and never edits your
  settings itself. Config: `coder.default_level`,
  `coder.subagent_matcher`, `coder.injection_budget_tokens`. Kill
  switch: `DEADEYE_CODER=off`.
- Under the hood: SessionStart raw-stdout injection (re-verified live --
  supersedes the earlier "SessionStart cannot inject" finding, which had
  only ever tested JSON fields), level filtering from one embedded
  canonical ruleset with a canary test against the skill file, and
  per-session level state in the daemon so a mid-session switch survives
  compaction re-injection.
- If you run another lean-coding persona plugin, uninstall it before
  enabling coder mode -- two personas double-inject every session.

## 0.4.1

- Fix: the duplicate-Read / large-Read advisories and the subagent
  brevity note shipped in 0.4.0 without an off switch. They now sit
  under `mode.preprocess` (and its `DEADEYE_PREPROCESS=off` kill
  switch), same family as the Bash-output rules -- every surface must
  be switchable off without touching the others.
- The schema now names every disable-able rule in
  `preprocess.disabled_rules`' description, and `mode.preprocess`'s
  description covers the advisory surfaces it gates.
- README/site: a third real measurement joins the before/after -- an
  actual `npm install express mocha`, 553 bytes of progress spam
  filtered to 55 (90.1%), with errors and warnings still passed through.

## 0.4.0

Nine new token-minimization surfaces, all advise-first per the project's
standing rule: enforcement only after advisory precision is proven.

- New rewrite rules: `gradle test`/`./gradlew test`, `dotnet test`,
  `rspec`/`bundle exec rspec`, `phpunit` (test-filter); `docker build`
  (build-filter); `npm install`/`npm ci`/`yarn install`/`pnpm install`/
  `pip install` (new install-filter -- keeps errors and warnings, drops
  progress spam); `kubectl logs` without `--tail` (new logs-tail --
  wraps with a 200-line tail). The test filter also learned rspec's
  `N failures`/`Failure/Error:` and dotnet's mixed-case `Failed` formats.
- Duplicate-Read detection: re-reading a file that hasn't changed since
  it was last read this session draws a one-line advisory. An edit to
  the file (mtime change) resets it -- re-reading changed files is
  legitimate.
- Large-file Read advisory: a whole-file `Read` of anything over 200KB
  suggests Grep or an offset/limit read; a bounded read of the same file
  stays quiet.
- `cat` of a large structured file (`.json`/`.csv`/`.xml`/`.yaml`/`.txt`
  over 200KB) draws an advisory -- never a rewrite, since truncating
  structured data can cut mid-record.
- Bare `git log` / `git show` draw the same style of advisory as the
  existing `git diff` one: suggest `--oneline -20` or a path scope.
- Consecutive-repeat detection: the same Bash command run twice with no
  Edit/Write in between (the retry-loop pathology) draws an advisory.
  An intervening edit clears it -- re-running after a fix is legitimate.
- Subagent brevity guidance: one line injected at SubagentStart asking
  for terse, structured results, since subagent output lands in the
  parent's context whole. (Whether this surface delivers injected
  context is unverified -- harmless no-op if not, and the decision log
  records it either way for correlation.)
- Real measurement: a rewritten command's actual output size is now
  logged at PostToolUse (`measured`, attributed to the rule), alongside
  the pre-run estimates -- ground truth for tuning every rule.
- MCP observation: every `mcp__*` tool response's size is logged
  (`observed`) to build the evidence base for which MCP tools deserve a
  rule -- their inputs can't be rewritten safely, so measurement first.

## 0.3.2

- The first daemon start (effectively install time -- the bootstrap
  spawns it on the first hook call) now seeds `~/.deadeye/config.json`
  with every default spelled out, plus a `$schema` pointer for editor
  validation and autocomplete. Users tweak an existing file instead of
  authoring one from scratch. An existing config is never touched.
- Fix: three documented config knobs did nothing. `posture` and
  `plan_gate.radius_trigger` are deleted outright -- no code path ever
  read either, so setting them changed nothing but the status printout.
  `mode.effort: "off"` is now actually wired: it suppresses the effort
  half of the Agent-routing recommendation and the effort-guidance line
  in the session injection, instead of being silently ignored. The
  schema's `mode.effort` enum also drops `"enforce"`, which was never
  possible -- the Agent tool has no effort parameter to enforce.

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
