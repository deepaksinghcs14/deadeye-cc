# Changelog

## Unreleased

## 0.22.2

**Go 1.27.0.** Toolchain bumped from 1.26.6 (`go.mod`; CI reads it via
`go-version-file`). CI's pinned `govulncheck` moved 1.1.4 → 1.7.0 — 1.1.4's
bundled `x/tools` SSA builder panics on go1.27 source; 1.7.0 scans this
(still dependency-free) module clean. No plugin behavior changed.

## 0.22.1

**Don't hand-roll what a library already gives you.** Two small rubric
edits, one idea:
- The over-engineering `stdlib:` tag (in `/deadeye-pr` and `/deadeye-review`)
  now flags reinventing what the standard library **or a dependency already
  in the project** ships — previously it only caught stdlib reinvention.
- Coder-mode ladder rung 5 reframed: use an installed library rather than
  hand-roll; a *new* dependency earns its place only when hand-rolling would
  be more code and more risk — never for what a few lines can do. Kept
  net-under-budget by tightening the ladder intro (the ruleset is injected
  every session, so it stays lean).

## 0.22.0

**PR review sharpened for precision (`/deadeye-pr`).** The rubric now makes
low-noise its contract — the axis every hosted AI reviewer loses on:
- **Proof-or-drop**: every finding must carry a `proof:` clause (the caller
  traced, the empty grep, the auditor line, the failing test). No proof, no
  print.
- **Reproduction as proof** for `inject`/`authz`/`logic`/`race`: a concrete
  input → sink, not a bare label.
- **Deterministic-tool fusion**: run the repo's own `go vet`/`tsc`/linter/
  touched tests and mark findings `(confirmed)` vs `likely` — a diff-only
  bot can't run your suite; the in-agent reviewer can.
- **Severity** (`block`/`warn`/`nit`) with a tallied verdict.
- **Inline per-line posting** on `--post` via `gh api .../pulls/<N>/reviews`
  (a `comments[]` array), instead of one wall-of-text comment.

**Tool surface consolidated (Breaking).** The command set had grown to
overlap; reconciled to a clearer, smaller surface:
- The three decision-log reports merged into one **`/deadeye-stats
  [savings|context]`** (default = the measured-impact scoreboard). Removes
  `/deadeye-audit`, `/deadeye-gain`, and `/deadeye-context`, and fixes their
  command/skill typing split. The `deadeye audit|gain|context` binary
  subcommands are unchanged — the skill fronts them.
- **`/deadeye-sweep` folded into `/deadeye-review --repo`** — one
  over-engineering entry point, scope as an argument. Removes
  `/deadeye-sweep`; its ranked-by-biggest-cut rubric is preserved as the
  repo branch.

Migration: `/deadeye-audit` → `/deadeye-stats savings`; `/deadeye-gain` →
`/deadeye-stats`; `/deadeye-context` → `/deadeye-stats context`;
`/deadeye-sweep` → `/deadeye-review --repo`. No engine, routing, or host
behavior changed.

## 0.21.0

**PR review across four lenses, on every host (`/deadeye-pr`).** A new
on-demand review that runs over a GitHub PR's diff through four lenses —
over-engineering, correctness, performance, security — and prints tagged,
one-line-per-finding results locally; pass `--post` to publish them back to
the PR as a single review comment (with an explicit confirm, and secret
values redacted). It reuses the `/deadeye-review` and `/deadeye-guard` tag
rubrics verbatim and adds correctness + performance, so it stays recognizably
deadeye while going broader than either (this deliberately overlaps the
host's own deep reviewer).

One canonical rubric (`internal/prreview/ruleset.md`) is the single source of
truth — mirroring the coder pattern. Claude Code ships it as the `deadeye-pr`
skill (kept byte-identical by a sync canary). `deadeye init
codex|gemini|cursor|windsurf` renders the **same** rubric into each host's
native on-demand surface (a Codex prompt, a Gemini TOML command, a Cursor
skill, a Windsurf workflow), swapping only the PR-argument token.

The four non-Claude renderings are **experimental**: those command surfaces
are documented but unverified on a live install, so confirm they fire before
relying on them. deadeye writes only its own command file (guarded by a
never-clobber marker) and never edits the host's own config; the PR command
is project-local for gemini/cursor/windsurf and lands in `~/.codex/prompts`
for codex. `deadeye uninstall <host>` removes it. No Claude-path behavior
changed.

## 0.20.0

**Coder persona — sharpened for first-shot results.** Four edits to the
injected ruleset, all net-neutral against the injection budget (funded by
dropping a redundant grammar example):
- Rung 2 now says *match the file's idiom*, not just reuse its helpers —
  code that reads native lands in one review, not two.
- *Finish the shot*: a stub or half-wired path for the asked-for behavior is
  unfinished, not lean (added to the never-cut list).
- *Audit the premise, not just the implementation* — treat a constraint you
  documented yourself as unverified; re-derive it, don't re-read it.
- *A new credential is a rung-1 question* — prove the platform doesn't
  already grant the access uncredentialed; "it works" isn't proof.

The embedded ruleset and `skills/deadeye-coder/SKILL.md` stay byte-identical
(sync canary); no engine or host behavior changed.

## 0.19.0

**Gemini CLI — the coder persona and session guidance (experimental).**
Gemini CLI has a real hook system (external command, JSON stdin/stdout,
tool-input rewriting), so it's engine-capable. This release wires the
context-injection tier: the coder persona at session start, plus session
guidance / codemap / vulnerable-dependency flag / large-paste / soft plan
gate on each prompt. `deadeye init gemini` writes a self-contained
extension under `~/.deadeye/gemini-extension/` and prints the
`gemini extensions install` command — deadeye never edits Gemini's own
config.

Tool-level features (exfil guard, output trimming, model routing) are
deliberately **not** wired on Gemini yet: its tool names and argument
shapes differ from Claude's and aren't confirmed on a live install, so
wiring the exfil guard blind could make it silently not fire. It waits
for verification rather than risk false security. The output translator
and ask→deny/advise fallback that will carry those features are already
built and tested.

Under the hood: the scattered `host == "codex"` checks became two
intent-named predicates in `internal/hosts`, so Gemini inherits Codex's
reduced-host behavior for free — and the Claude Code path is provably
unchanged (SessionStart still raw-stdout, permission asks untranslated,
full tier table).

## 0.18.0

**Cursor and Windsurf — the coder persona, on two more editors.** Both
read an always-on rules file but have no hook contract, so they get the
lean-first coding discipline as a static file (`deadeye init cursor` →
`.cursor/rules/deadeye.md`, `deadeye init windsurf` →
`.windsurf/rules/deadeye.md`), level-filtered to your `coder.default_level`.
Persona only — the routing, security, preprocessing, and codemap engine
still needs a host with a live hook contract (Claude Code, Codex, and —
next — Gemini CLI). The file is marker-tagged: init never overwrites a
rules file you authored, and `deadeye uninstall cursor|windsurf` removes
only what deadeye wrote.

Also: config.json now tolerates a leading UTF-8 BOM (some Windows editors
add one), which previously made a BOM-saved config silently revert every
setting to default.

## 0.17.1

Bump the build toolchain to go1.26.6, clearing four Go standard-library
advisories present in go1.26.5 -- net/url (GO-2026-6218), crypto/tls
(GO-2026-6090), encoding/asn1 (GO-2026-5972), and net/http
(GO-2026-5026), all reachable through `deadeye update`'s HTTPS client.
Caught by 0.17.0's own new govulncheck CI job. No behavior change; the
released binaries are now built against the patched standard library.

## 0.17.0

**A security release: guard the exfiltration path, and stop coupling
security to a mood.** The emerging real attack on coding agents is
prompt-injection-driven secret egress -- malicious content telling the
agent to read a credential file and ship it out. deadeye sits at the exact
choke points, so it now watches them.

- **Exfiltration guard** (new top-level `security.exfil` axis,
  `off`/`advise`/`ask`, default **ask**): a Read of a sensitive credential
  path (ssh private keys, `~/.aws/credentials`, `.env`,
  `~/.claude/.credentials.json`, `~/.netrc`, `~/.kube/config`, gcloud,
  gh-hosts, and more -- plus your own `security.sensitive_paths` globs), or
  a Bash command that ships one out (a credential path piped to
  curl/nc/scp, an `env` dump piped to the network, a reader pulling a key
  into context), escalates to a permission prompt the model **cannot
  answer for itself** -- a prompt-injected instruction can't approve it.
  High precision: `~/.ssh/config`, `.pub` keys, `.env.example`, and `ssh
  -i key host` / `scp -i key` all stay silent. Independent of the coder
  persona; disabled only by `security.exfil: "off"` or `DEADEYE=off`.
- **Security decoupled from the coder persona level:** `stop coder` /
  `/deadeye-coder off` no longer silences the live Edit/Write security
  advisory -- disliking the persona's prose is not a reason to stop
  checking what's written. Still covered by `coder.security: "off"` and
  the env kill switches.
- **`coder.security: "ask"`** (new third value, opt-in): adding a
  dependency with a **confirmed** OSV advisory becomes a permission prompt
  naming the package, instead of a nudge a compromised agent can ignore.
- **Session-start dependency flag:** once per session, deadeye scans the
  project's existing manifests and flags any current dependency with a
  known advisory -- the vulnerable library already in your tree that no
  edit touches. A floor (declared versions, not resolved lockfiles);
  points at `/deadeye-guard` for the full native-auditor pass.
- **Provider-token fingerprints** in the secret-literal rule: GitHub,
  Slack, Anthropic, OpenAI, Stripe (live only), Google, GitLab, and signed
  JWTs -- bare tokens with near-zero natural occurrence, so pasting one as
  a literal is caught even with no `key =` context.
- **CI hardening:** every GitHub Action pinned by commit SHA, a
  least-privilege `permissions:` block, and a `govulncheck` job.

## 0.16.0

**Six new context-hygiene surfaces, all advisory.** The untouched token
drains, each with its own `preprocess.disabled_rules` switch and no new
mode axis -- nothing here rewrites or blocks:

- **`delegate-explore`** -- 12+ consecutive Read/Grep/Glob completions
  with no edit, command, or new prompt in between is survey work; one
  nudge toward an Explore subagent, whose reads land in a disposable
  context while the parent pays only for the summary.
- **`compact-timing`** -- deadeye counts every tool-response byte that
  arrives; past ~300KB the next Stop (a natural task boundary) carries a
  one-line suggestion to `/compact` by choice instead of letting
  auto-compact land mid-task. Once per accumulation cycle; a compact or
  clear restarts the count.
- **`bash-retry`** -- repeat-command's flag-escalation sibling: the same
  target re-run a third time with only option changes (`pytest x`,
  `pytest x -x`, `pytest x -x -vv`) and no edits in between. Hard
  precision bias: compound commands refused outright, `-run=TestFoo` vs
  `-run=TestBar` never collide.
- **`repeat-webfetch`** -- the same URL fetched twice in one session
  while the first response is still in context (fragment and trailing-
  slash variants fold; a different query string is a different fetch).
- **`mcp-oversize`** -- a single MCP response past 32KB gets a post-hoc
  nudge to narrow the next call; once per tool per session.
- **`large-paste`** -- a 20KB+ prompt is a paste, and it stays resident
  all session; one nudge toward file-plus-Grep. Synthetic prompts never
  fire it.

Plus **`/deadeye-context`**: a per-session ranked breakdown of context
bytes by source -- deadeye's own injections (real bytes at injection
time), observed arrivals (explicitly labeled a floor, since only >8KB
built-ins and MCP responses are ever logged), and kept-out savings with
the estimated/measured split intact. Also fixes `inject-subagent` log
rows missing their byte size; pre-0.16 rows render "size not recorded"
rather than counting as zero.

## 0.15.2

**Second sweep -- including a self-review of the first.** Three
fresh-angle passes over v0.15.1: an adversarial review of that release's
own fixes, a harder look at the kernel/catalog scoring math, and -- new
-- real runtime traffic thrown at a live daemon instead of code reading.
13 more bugs, each fixed with a regression test verified by live
revert-and-reconfirm.

- **kernel**: NaN evidence silently read as best-case (`>`/`<` against
  NaN are always false), downshifting garbage input to the cheapest
  model at a reported confidence of 1.0 -- the exact opposite of INV-1;
  a NaN downshift threshold disabled the confidence gate the same way.
  Two fall-through paths misreported Confidence:0/"no evidence" on real
  evidence, user-visible via `/deadeye-route`. All four now conservative
  and accurate.
- **hook/daemon**: a 7MB payload (under the daemon's own 8MB cap)
  reproducibly hit the client's fixed 200ms deadline and silently
  degraded to `{}` with a perfectly healthy daemon -- caught by actually
  running one, not reading code. The deadline now scales with payload
  size on BOTH sides of the socket, derived from one shared constant so
  they can't drift apart. Also: the stale-lock takeover path never wrote
  its pid, so v0.15.1's releaseLock protection could never fire on the
  one path it was written for.
- **codemap**: v0.15.1's `ls-files -z` fix disabled all of git's path
  quoting, so a filename with a literal control byte (legal!) corrupted
  the rendered map -- now sanitized, at both entry points (git-tracked
  paths and session-touched paths; the second was found only by
  reviewing the first fix).
- **preprocess**: `du --exclude=drafts` suppressed the du-cap advisory
  -- the "d" in an ordinary long-option name still read as a depth flag.
  Long options now match by exact name; short clusters keep the letter
  check.
- **secscan**: v0.15.1's jsShellRe fix over-narrowed -- `cp.exec`,
  `childProcess.exec`, and `require('child_process').exec(...)` all
  silently stopped matching (lost true positives on a security rule).
  Restored via an explicit alias list WITH a left-boundary guard, since
  the naive widening false-fired on `scp.exec`/`gcp.exec`.

The lesson worth keeping: every round of fixes got its own adversarial
review, and every round found real bugs IN the previous round's fixes
(13, then 8, then 5). Fixes are code too.

## 0.15.1

**A deep bug sweep, not a feature.** Every internal package and cmd/deadeye
reviewed independently, three times over, for reachable-input correctness
bugs -- 13 confirmed and fixed, all with regression tests, several verified
against real tool output (npm 11) or a live revert-and-reconfirm.

- **sessionmem**: a project key that's a valid prefix of another's (`app`
  vs `app_api`, both legal since `_` is allowed) could have its session
  summaries pruned and overridden by the other project's writes. Filename
  separator changed from `_` to `@`, which the key sanitizer always strips.
- **cmd/deadeye**: bare `deadeye hook` (no event) panicked instead of
  failing open. `deadeye update` silently no-op'd on Windows (wrong local
  filename) and used `0o755` instead of the `0o700` every other writer
  uses for `~/.deadeye`; `bootstrap.sh`'s fresh-install directory had the
  same gap.
- **codemap**: `git ls-files` (no `-z`) let git's default filename quoting
  corrupt any non-ASCII tracked path into a phantom directory row; fixed
  with `-z` and NUL-delimited parsing. Regenerate/MergeTouched/PruneNotes
  now write through a shared atomic temp-file-plus-rename, closing a
  torn-read window against concurrent same-project sessions.
- **daemon**: a narrow crash-recovery race could let a losing daemon
  delete a live daemon's lockfile, breaking `deadeye uninstall`'s ability
  to signal it. `releaseLock` now only removes a lockfile that still names
  its own pid.
- **preprocess**: `install-filter`'s error/warning grep was case-sensitive
  for `WARN`/`npm ERR!` -- npm 7+ (current: 11) renamed both to lowercase,
  so a real npm 11 install (warnings or a hard failure) matched nothing
  and silently ate the output. `du-cap`/`tree-cap`'s "already bounded"
  checks matched the whole command line, so a path like `./old-drafts` or
  `./src-Legacy` was misread as an already-passed `-d`/`-s`/`-L` flag.
- **secscan**: the JS shell-injection rule fired on `RegExp.prototype`'s
  own `.exec()`; the TLS-off rule fired on any variable merely ending in
  "verify" (e.g. an unrelated `email_verify` flag); the go.mod dependency
  extractor never matched gofmt's single-line `require x v1.2.3` form (no
  parens), so a module with exactly one dependency skipped scanning
  entirely.

Two additional narrow, self-healing races (a cross-process notes-append
race, the daemon lock's own brief two-winners window) are documented in
place with the project's own `deadeye: ... ceiling: ... upgrade:` marker
rather than papered over.

## 0.15.0

**Codebase map -- the re-orientation tax, actually killed.** PLAN.md §5.7
named the goal ("a fresh session burns 15-40 tool calls rediscovering the
project") but the shipped session memory only carried git activity --
branch, commits, dirty files -- never what the codebase IS. Meanwhile the
two signals that did capture real structure were thrown away every
session: the explore skill's findings died with its fork, and the
which-files-did-this-session-read map died at SessionEnd. All three now
persist, per project, under `~/.deadeye/map/`:

- **Structural skeleton** (`<project>.map.md`) -- directory rows with file
  counts and Go package doc-comment purposes, built from a streamed
  `git ls-files` at the repo root (resolved first: ls-files is cwd-scoped,
  and a session started in a subdirectory would otherwise silently map
  only that subtree). Regenerated at SessionEnd only when the tracked-file
  fingerprint moved.
- **Touch-frequency counter** (`<project>.touched.json`) -- a
  `path -> sessions` count merged at every SessionEnd, capped at the top
  12 by relevance, not recency: accumulated knowledge of which files
  actually matter, not a rolling window that forgets. Read-tracking now
  runs whenever preprocess OR codemap is on, so turning off output
  trimming no longer silently starves the map.
- **Exploration notes** (`<project>.notes.md`) -- the explore skill caches
  its summary via a new `deadeye notes-append` subcommand (real Go does
  the path math; the write is a single O_APPEND, safe beside the daemon's
  own pruning). Newest 5 entries kept.

Injected once per session at the first prompt, after the guidance block;
suppressed on resume/compact (that context already carries its own
exploration). New `mode.codemap` switch (`off`/`on`, default `on`) gates
writes and injection both. Every truncating write in the new package is
mutex-protected -- the daemon serves concurrent sessions, and two
same-project SessionEnds must not tear a counter file.

Also: the byte-identical `gitOutput`/`gitOut` helpers (sessionmem,
route.go) consolidated into `internal/gitutil` along with `ProjectKey` --
a third caller made the copy a liability.

## 0.14.0

**Security joins coder mode -- part of coding, not a post-check.** The
persona's "never cut security" carve-out was purely negative: it told the
model not to remove safety it happened to find, never to look for
exposures in what it was about to write. Rung 5 of the ladder
(reach for an installed dependency) also had no idea whether that
dependency was vulnerable or abandoned. Both close now.

- **`## Check your backstop`** -- a new ruleset section scaled across
  spotter/marksman/sniper: name the trust boundary before crossing it,
  take the safe form (usually the shorter one), never hand-roll crypto or
  a sanitizer, and use the existing `deadeye: ... ceiling: ... upgrade:`
  marker for a knowingly-shipped exposure. Injection budgets raised
  deliberately alongside it: 6.5KB -> 8KB per level,
  `coder.injection_budget_tokens` 1600 -> 2000.
- **Live Edit/Write advisory** -- a deterministic pass (`internal/secscan`)
  over the ADDED text only, never the target file: SQL/shell/eval
  injection shapes, hardcoded secrets, weak crypto, and disabled TLS
  verification, across Go, JavaScript/TypeScript, Python, Java, and Rust.
  Advisory only, deduped once per session per finding, capped at 2 per
  edit, gated on coder mode being active. New `coder.security` config
  key (`off`/`advise`, default `advise`).
- **Dependency check** -- editing a manifest (`go.mod`, `package.json`,
  `requirements.txt`/`pyproject.toml`, `Cargo.toml`, `pom.xml`/
  `build.gradle`) checks the dependency itself: a bundled table flags
  packages the platform has since absorbed (`request` -> `fetch`,
  `moment` -> `Temporal`/`date-fns`), and an optional OSV.dev lookup
  catches known vulnerabilities. The network call never sits in the hook
  path -- a background daemon goroutine refreshes
  `~/.deadeye/osv-cache.json` (package name + version only, nothing else
  about the codebase) so a cache miss informs the *next* edit, never
  blocks the current one. New `coder.security_osv` config key (default
  `true`; `false` keeps the check fully offline, the bundled table still
  works).
- **`/deadeye-guard`** -- the on-demand deep pass: diff-scoped, verifies
  a missing sanitizer/authz check against the surrounding code before
  reporting it (same discipline as `/deadeye-review`), and runs
  `govulncheck`/`npm audit`/`pip-audit`/`cargo audit` when installed.
  `/deadeye-review`'s scope note now points security findings here
  instead of at Claude Code's `/security-review`.
- Subagent card's measured cut, re-measured after both ruleset changes:
  7,666 bytes per spawn -> 1,073 -- an 86.0% cut (was 85.3%).

## 0.13.0

- **`deadeye update`** -- the one-command updater for hosts without the
  plugin bootstrap (Codex-only installs): hash-compares the local
  binary against the latest release's checksums.txt (no update needed
  -> no download), sha256-verifies what it fetches, swaps it in with an
  atomic rename, and reports the version the new binary actually
  prints. Warns when a PATH binary shadows the managed copy. The daemon
  version handshake makes the swap take effect on the next hook call.

## 0.12.0

**Experimental Codex CLI support.** Codex's hooks system (v0.114+,
experimental) speaks nearly the same contract as Claude Code's, and a
live verification spike (docs/verified.md §12) confirmed the two
load-bearing behaviors on real Codex runs: SessionStart
`additionalContext` reaches the model, and PreToolUse `updatedInput`
really rewrites the executed command.

- `deadeye init codex` registers the hooks: shows the exact
  `~/.codex/hooks.json` changes, writes only after an explicit yes,
  preserves any existing entries. `deadeye uninstall codex` removes
  exactly ours. Codex's own hook-trust prompt and the
  `[features] hooks = true` flag are surfaced, never bypassed.
- Works on Codex: Bash output trimming + all advisories, the coder
  persona (compaction survival via PostCompact; level switching by
  typing the /deadeye-coder command as prompt text), the plan gate
  (`apply_patch` joins the Edit/Write path), /deadeye-mute, decision
  log + gain/audit.
- Not on Codex (deliberate): model routing (no subagent surface),
  ultracode workflow hints, the statusline badge, self-bootstrap.
  Codex sessions get a host-tailored guidance injection without the
  Claude-only tier table.
- `deadeye status` gains a Hosts block showing Claude and Codex
  registration state.

## 0.11.0

Third deep sweep: competitive hardening against Claude Code's built-ins,
plus the daemon lifecycle fixes.

- **Version handshake: the daemon now retires itself when a newer binary
  answers a hook.** Before this, an updated plugin's new features were
  silently inert until the old daemon idled out (30 min, reset on every
  connection -- for an active user, days). The current request is
  answered first; the next hook call respawns the new binary.
- **Panics leave a trace**: `~/.deadeye/panics.log` plus a `panic` row
  in the decision log (visible in `/deadeye-audit`). Fail-open stays;
  "deadeye stopped advising" is now diagnosable.
- **`outcomes.jsonl` rotates at 10MB** like the decision log -- it was
  the one file with no ceiling.
- **`/deadeye-review` and `/deadeye-sweep` overhauled** to out-execute
  naive review prompts: pinned cheap scoping (`git ls-files` +
  grep-first for sweep; exact diff commands for review), a
  verify-before-report step (grep implementers/callers before asserting
  yagni/delete), finding caps with ranked truncation, explicit
  empty-diff/non-git behavior, and cuts that emit the pinned `deadeye:`
  marker grammar into the debt ledger.
- README/site: new "How this fits with Claude Code's built-ins" section
  (review layering, plan-gate triggering, content-based vs size-based
  trimming, routing, persona). Windows story corrected: hooks and
  daemon work (PowerShell scripts ship); self-bootstrap and the
  statusline badge remain unix-only.
- Smaller: debt-ledger grep covers block/HTML comment styles; gain
  skill matches the shared retry voice; `DisabledRuleSet` computed once
  per Bash request; captures/ documented as never-pruned.

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
daemon.

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
