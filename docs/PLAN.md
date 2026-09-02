# deadeye — Build Plan

> **How to use this document.** This is the complete specification for a Claude
> Code plugin, written to be executed by Claude Code. Do NOT attempt the whole
> plan in one pass. Work phase by phase (§9). Before writing any code that
> depends on a hook contract, tool schema, or API shape, run the matching
> verification in §10 and record the result in `docs/verified.md` — the plan is
> amended by findings, not the reverse. §2 invariants are non-negotiable and
> should be encoded as tests.
>
> **Amendment log (Phase 0, 2026-08-02):** Phase 0 shipped. Nine findings from
> `docs/verified.md` amend this document in place below, each marked
> `[AMENDED 2026-08-02, see docs/verified.md §X]`. Two are load-bearing:
> `internal/hookio`'s response shape had two real bugs the live Claude Code
> validator caught (`hookEventName` is required, not optional; `SessionStart`
> does not accept `hookSpecificOutput` at all), and §5.1's entire SessionStart
> injection mechanism does not work as designed and needs a redesign before
> Phase 2 starts. See `docs/verified.md` for full evidence on every amendment.
>
> **Assets already produced** (in this repo, do not regenerate): `README.md`,
> `assets/logo.svg`, `assets/logo-dark.svg`, `docs/site/index.html`,
> `.github/workflows/pages.yml`.
>
> `[AMENDED 2026-08-02]` **This list was wrong at the start of Phase 0.** The
> repo held only `LICENSE`, a 2-line `README.md`, and `.gitignore` — none of
> the assets above existed. Phase 0 did not build them (they ship with Phase
> 1); this note exists so Phase 1 doesn't assume they're already there either.

> **Name: `deadeye`.** Rationale: the marksman who never wastes a
> shot. The metaphor IS the value proposition — not "the cheap one," but the one
> whose every token lands. Pairs with `greybeard` as a second competency in the
> same frontier register: one remembers what depends on what, the other never
> wastes a shot.
>
> **Repo is `deadeye-cc`; the product is `deadeye`.** The `-cc` suffix
> disambiguates the GitHub/module namespace (`d3c0d3er/deadeye` is a small
> dormant credential-phishing tool — not a functional collision, but the wrong
> neighbour for a plugin that rewrites tool calls). The suffix appears ONLY in
> paths that must be globally unique:
>
> | Surface | Value |
> |---|---|
> | GitHub repo | `deepaksinghcs14/deadeye-cc` |
> | Go module | `github.com/deepaksinghcs14/deadeye-cc` |
> | Pages site | `deepaksinghcs14.github.io/deadeye-cc/` |
> | Marketplace add | `/plugin marketplace add deepaksinghcs14/deadeye-cc` |
> | Plugin install | `/plugin install deadeye@deadeye-cc` |
> | Binary | `deadeye` |
> | Release assets | `deadeye_<os>_<arch>` |
> | Commands | `/deadeye-status`, `/deadeye-route`, `/deadeye-audit`, `/deadeye-config` |
> | State dir | `~/.deadeye/` |
> | Env vars | `DEADEYE=off`, `DEADEYE_PREPROCESS=off`, `DEADEYE_GATE=off` |
>
> Nothing the user types day-to-day carries the suffix. Set `name` in
> `.claude-plugin/plugin.json` to `deadeye` explicitly, and confirm the
> marketplace identifier the plugin system derives from the repo — if it does not
> resolve to `deadeye-cc`, correct the install line in README and site (§10.8).
>
> `[AMENDED 2026-08-02, see docs/verified.md V1/V2]` **"Plugin install"
> row above is wrong.** The marketplace identifier is NOT derived from the
> repo name — `.claude-plugin/marketplace.json`'s `name` field is required
> and authoritative (confirmed both live and by reading the Claude Code
> binary's own manifest schema and install-log strings). With
> `marketplace.json.name: "deadeye"` (as this repo now has it), the correct
> line is:
>
> **`/plugin install deadeye@deadeye`** — not `deadeye@deadeye-cc`.
>
> Correct this everywhere it appears: README, the Pages site (§10.8), and
> anywhere else the install snippet is quoted.
>
> Required regardless of naming: repo description and topics must place this
> unambiguously in agent tooling — "Claude Code plugin — fits model, effort, and
> context to each task"; topics `claude-code`, `claude-code-plugin`,
> `token-optimization`, `model-routing`, `claude-hooks`. README's first line
> states the metaphor plainly: *he doesn't waste shots.*
>
> Keep the display name in a single constant regardless.

## 1. Thesis

A Claude Code plugin that makes every session cheaper without making it dumber, by
managing the dials Claude cannot manage for itself:

1. **What enters context** (tool-output preprocessing) — largest, safest savings.
2. **How hard the model thinks** (effort level) — primary reasoning dial.
3. **Which model does which task** (model routing for subagents) — secondary dial.
4. **Whether to plan before implementing** (plan gate, user-consented).
5. **Whether a task deserves a workflow** (recommend-only, never auto-enable).

The plugin is a **deterministic policy kernel** wrapped around a probabilistic agent:
signals in, decisions out, every decision logged, every rule testable without an LLM
in the loop. It **learns** which decisions worked from observed outcomes, and it
**stays current on new models automatically** by deriving tiers from the live model
catalog instead of hardcoding names.

---

## 2. Hard invariants — violate none of these

These are enforcement-layer rules. Every feature must be checked against them before
merge. Encode them as unit tests where possible.

- **INV-1: Unknown routes up.** Missing evidence about a task must NEVER produce a
  cheaper model or lower effort. Downshifting requires positive evidence above a
  confidence threshold. Upshifting is always free.
- **INV-2: Never resolve toward the unbounded side.** Workflows/orchestration can
  spend unbounded tokens and auto-approve edits — therefore the plugin only ever
  RECOMMENDS workflows, never triggers them. (This is INV-1 generalized: on cost
  axes the risk is under-powering, on orchestration the risk is over-spawning.)
- **INV-3: Never write to `~/.claude/settings.json` or any user settings file.**
  The main-session model cannot be changed reliably mid-session; do not try.
  Main-session guidance is advisory only (`systemMessage` / injected context).
- **INV-4: SessionStart injection must be byte-stable within a session.** Compute
  once at session start; never vary per turn. Per-turn variance in injected context
  risks prompt-cache misses that cost more than the plugin saves. Budget: the total
  injected preamble ≤ 400 tokens.
- **INV-5: Fail open.** Any error in any hook passes the prompt/tool-call through
  unmodified. A broken policy layer must never block the user's work. Wrap every
  hook entry point in a top-level recover that emits `{}`.
- **INV-6: Every automated decision is visible and reversible.** Log every decision
  with its reason. Every enforcement surface has (a) a per-invocation bypass,
  (b) a mode setting (`off | advise | enforce`), and (c) a global env kill switch
  `DEADEYE=off`.
- **INV-7: Consent is sticky.** Once the user declines a gate for a task, do not
  re-ask within that task. Record the decline; it is also a training signal.
- **INV-8: Hooks must be fast.** PreToolUse hooks sit in the critical path of every
  matched tool call. Hook process budget: p95 < 50ms. Achieve this via a
  long-lived daemon + thin client over a unix socket (see §5.3), not per-call
  cold starts of the full binary.
- **INV-9: "No error" is not success.** In the learning loop, weight strong negative
  signals (revert, escalation, test failure, user correction) heavily; weight
  "completed without visible error" near zero. Otherwise the policy learns to route
  everything down.

`[AMENDED 2026-08-02, see docs/verified.md V8]` On INV-8: measured a bare Go
binary (no daemon) at p95 16.5ms over 30 cold starts on the build machine —
already 3x under budget without the daemon. The daemon was still built,
per an explicit scope decision for Phase 0, and its own live in-process
benchmark comes in far lower (p95 ≈ 400-600µs). Both numbers are recorded
so the daemon's cost/benefit stays visible rather than assumed.

---

## 3. Architecture overview

Language: **Go** (single static binary, same toolchain and release pipeline as
greybeard: goreleaser, GitHub Releases, sha256-verified self-bootstrap on first use).

```
deadeye/
├── .claude-plugin/           # plugin manifest for the marketplace
│   └── plugin.json
├── hooks/
│   └── hooks.json            # hook registrations (bundled, auto-registered)
├── commands/                 # slash commands (markdown)
│   ├── deadeye-status.md          # explain current decisions & modes
│   ├── deadeye-route.md           # explain the tier/effort decision for current context
│   ├── deadeye-audit.md           # savings report from the decision log
│   └── deadeye-config.md          # guided config editing
├── cmd/deadeye/                   # main binary: daemon + CLI + hook client
├── internal/
│   ├── kernel/               # the policy kernel (pure functions, no I/O)
│   │   ├── grid.go           # (model, effort) grid search
│   │   ├── policy.go         # rule evaluation, thresholds, INV-1 enforcement
│   │   └── policy_test.go    # table-driven tests — this is the most-tested pkg
│   ├── signals/              # Signal provider interface + built-ins
│   ├── catalog/               # model catalog resolver (pricing → tiers)
│   ├── preprocess/           # tool-output preprocessing rules
│   ├── lessons/              # outcome logging + prior updates
│   ├── store/                # SQLite (single file, ~/.deadeye/deadeye.db)
│   └── hookio/               # stdin/stdout hook protocol types (PreToolUse etc.)
├── testdata/
└── scripts/
```

`[AMENDED 2026-08-02, see docs/verified.md V8 and §3.3 below]` The tree
above is the original target shape. What actually shipped in Phase 0 adds
`internal/proto` (the client↔daemon wire protocol, distinct from
`internal/hookio`'s Claude-Code-facing contract), `internal/config`,
`internal/meta`, and `internal/logstore` in place of `internal/store`
(JSONL, not SQLite — see the amendment on §3.3). `internal/kernel`,
`internal/signals`, `internal/preprocess`, and `internal/lessons` are not
built yet; they land in Phases 1–6 as originally planned.

### 3.1 Signal provider interface

The kernel consumes evidence; providers produce it. Providers must be optional and
degradable — the plugin works with zero external providers.

```go
type Scope struct {
    Prompt      string
    Files       []string   // paths in scope, if determinable
    Repo        string
    SessionMode string     // permission_mode from hook input
}

type Evidence struct {
    Provider   string
    Complexity float64    // 0..1, provider's estimate
    Confidence float64    // 0..1, how much the provider trusts its own estimate
    Facts      map[string]any  // e.g. file_count, has_tests, churn, blast_radius
}

type Signal interface {
    Name() string
    Assess(ctx context.Context, s Scope) (Evidence, error)  // errors → skip provider
}
```

**Built-in providers (no dependencies):**
- `promptshape`: word count, question density, vagueness heuristics, imperative-vs-
  exploratory phrasing, mention of "architecture/redesign/tradeoff" etc. This is the
  weakest signal — cap its weight.
- `filescope`: number of files referenced or inferable, single-file vs multi-file.
- `gitchurn`: recent churn on target paths (`git log --since` counts).
- `testpresence`: do the target paths have adjacent tests.

**External provider (optional):**
- `greybeard`: if the `greybeard` binary is on PATH and the repo is indexed, call it
  for blast radius (inbound edge count, hardest inbound edge type) and confidence.
  Absent or erroring → skip silently, kernel proceeds with weaker evidence and,
  per INV-1, a more conservative (higher) decision. Implement behind the same
  `Signal` interface; do NOT import greybeard code — shell out only.

### 3.2 The policy kernel

Pure functions. Input: `[]Evidence` + config + learned priors. Output: a `Decision`.

```go
type Decision struct {
    Model       string   // model id or alias to assign (subagent scope only)
    Effort      string   // low|medium|high|xhigh (never max — session-only)
    PlanGate    bool     // require plan/consent before edits
    Workflow    Hint     // None | Recommend (never Trigger — INV-2)
    Reason      string   // human-readable, shown in transcripts and logs
    Confidence  float64
}
```

**Grid search, not sequential choice.** Cost is `f(model, effort)` and cells
interleave — `(sonnet, xhigh)` competes with `(opus, low)`. Enumerate the grid of
(catalog models × supported-effort levels), estimate cost per cell from catalog
pricing and a rough output-token prior, filter cells below the required capability
floor (from evidence), pick the cheapest surviving cell. Descent preference when
evidence is ambiguous: **walk effort down before changing model family** (keeps
judgment class, cuts thinking spend).

Effort optimism is safe: Claude Code clamps unsupported levels down to the highest
supported level ≤ requested. So the kernel may request `xhigh` generically without
maintaining a per-model capability table. (Do keep the catalog's best-known level
list as a hint for cost estimation, but correctness never depends on it.)

### 3.3 Storage

One SQLite file: `~/.deadeye/deadeye.db`. Tables:

```sql
-- every decision the kernel makes, enforced or advisory
CREATE TABLE decisions (
  id INTEGER PRIMARY KEY, ts TEXT, session_id TEXT, surface TEXT,       -- which hook
  scope_hash TEXT, task_shape TEXT,                                      -- features
  model_requested TEXT, model_applied TEXT,
  effort_requested TEXT, effort_applied TEXT,
  plan_gate INTEGER, workflow_hint TEXT,
  mode TEXT,                                                             -- advise|enforce
  reason TEXT, confidence REAL
);

-- outcome signals attached to decisions after the fact
CREATE TABLE outcomes (
  id INTEGER PRIMARY KEY, decision_id INTEGER, ts TEXT,
  kind TEXT,      -- escalation|downshift|revert|test_fail|user_correction|
                  -- gate_declined|gate_accepted|overthink|clean
  weight REAL,    -- per §8 weighting; clean ≈ 0.05, revert/escalation ≈ 1.0
  detail TEXT
);

-- learned priors, keyed per (model, task_shape) — effort is calibrated per model,
-- lessons do NOT transfer across models
CREATE TABLE priors (
  model TEXT, task_shape TEXT, effort TEXT,
  success_w REAL, failure_w REAL, n INTEGER,
  PRIMARY KEY (model, task_shape, effort)
);

-- model catalog cache
CREATE TABLE catalog (
  model_id TEXT PRIMARY KEY, family TEXT, input_price REAL, output_price REAL,
  tier INTEGER, fetched_at TEXT, source TEXT
);
```

`[AMENDED 2026-08-02]` **Storage is JSONL, not SQLite**, decided before
Phase 0 build started (Go has no stdlib SQLite; the choice is CGO, which
breaks clean cross-compile in goreleaser, or a large pure-Go dependency —
neither justified for Phase 0's append/scan access pattern). Implemented:
`~/.deadeye/decisions.jsonl`, append-only, one `logstore.Record` per line
(`ts, session_id, surface, action, reason, bytes_before_est, bytes_after`),
size-rotated at 10MB. A single `O_APPEND` write under 4KB is POSIX-atomic,
so concurrent hook processes need no cross-process locking. The
`outcomes`/`priors`/`catalog` tables above remain the Phase 6/Phase 2
target shape conceptually — revisit SQLite only if the learning loop
genuinely needs joins across them; `catalog` in particular is superseded
by the compiled-in table (§4 amendment below), not a cached table at all.

---

## 4. The model catalog resolver (self-updating, no plugin releases needed)

**Goal:** a new model shipping must slot into the tier table with zero code changes.

Mechanism — **derived, not learned**:
1. Fetch the model list + pricing. Sources in priority order: (a) the Anthropic
   models API if reachable with ambient credentials, (b) a small pricing table
   scraped/fetched from official docs at daemon start with long cache, (c) the last
   cached catalog in SQLite, (d) a compiled-in fallback table (last resort, stamped
   with its build date and flagged as stale in `/deadeye-status`).
2. Rank by blended price (input + 4×output as a rough usage-mix weight, tune later).
3. Assign tiers by rank: tier 0 = cheapest, ascending. Family name in the model id
   (`haiku|sonnet|opus|fable`) is a cross-check only — if price rank and family rank
   disagree, log a warning and trust price.
4. Refresh: at daemon start and at most once per 24h. A model that appears or
   changes price re-ranks the table automatically.
5. `/deadeye-status` shows the live tier table with prices and fetch timestamp, so the
   user can always see what the kernel believes.

**Verification task for you (Claude Code) before implementing:** determine what
model-listing/pricing endpoint is actually reachable from a Claude Code environment
with the user's auth, and what its response schema is. Do not guess the schema —
probe it, then write the parser against reality. If no endpoint is reachable,
implement (b)+(c)+(d) only and document the limitation.

`[AMENDED 2026-08-02, see docs/verified.md V7]` **Source (a) is dead —
confirmed, not merely assumed.** The Claude Code hook environment carries
no `ANTHROPIC_API_KEY` (OAuth-based); `GET /v1/models` returns `401`, and
even authenticated it carries **no pricing field** at all (only
`capabilities.effort` per model). Sources (b)+(c)+(d) collapsed into one:
a compiled-in table (`internal/catalog/catalog_gen.go`) generated by
`scripts/gen-catalog.go` from a hand-maintained price seed (fetched live
from https://platform.claude.com/docs/en/about-claude/pricing,
2026-08-02), overridable wholesale by `~/.deadeye/catalog.json`. No
runtime network call at all — "refresh at daemon start" (step 4 above) is
dropped; refresh happens by re-running the generator and shipping a
release. `/deadeye-status` shows the table's `built_at` date and source
(`builtin` vs `override`) per step 5, unchanged.

---

## 5. Enforcement & advisory surfaces (hooks)

All hooks registered via the plugin's bundled `hooks.json`. Every hook script is a
thin client (`deadeye hook <event>`) that connects to the daemon over
`~/.deadeye/deadeye.sock`; if the daemon is not running, the client starts it
(detached) and, for THIS invocation, returns `{}` immediately (INV-5, INV-8).

`[AMENDED 2026-08-02, see docs/verified.md V1]` **Do not declare `"hooks":
"./hooks/hooks.json"` in `plugin.json`.** That standard path auto-loads;
declaring it explicitly is a hard load error (`Duplicate hooks file
detected`), confirmed live. Only declare the `hooks` manifest field for a
nonstandard filename or additional hook files beyond the standard one.
This repo's `plugin.json` has no `hooks` key at all.

### 5.1 SessionStart — advisory context (computed once)

Inject, byte-stable for the session (INV-4), total ≤ 400 tokens:
- The current tier table (3–5 lines) and the rule: subagents get the cheapest tier
  that fits; mechanical work → tier 0, standard implementation → tier 1, deep
  reasoning → top tier. When calling the Agent tool, SET the model param accordingly.
- Effort guidance: request lower effort for mechanical steps.
- Workflow-script guidance: "if you author a workflow script, route indexing and
  classification phases to the cheapest tier; reserve the strongest model for final
  verification and architectural judgment."
- One line on the plan convention (see 5.4).

`[AMENDED 2026-08-02, see docs/verified.md §5.1 finding — LOAD-BEARING]`
**This mechanism does not work and blocks Phase 2 until redesigned.**
Confirmed live, three-part test: `SessionStart` does not accept
`hookSpecificOutput` at all (schema-rejected); `systemMessage` is accepted
without error but never reaches the model's context in either interactive
or `-p` sessions (tested directly — a probe string returned via
`systemMessage` was not visible to the model when asked to repeat it back,
while the same probe via `UserPromptSubmit`'s `additionalContext` *was*
visible, confirming the test method is sound). **There is currently no
confirmed way to inject model-visible context at `SessionStart`.**

Recommended fix for Phase 2 (not yet built): move this injection to the
first `UserPromptSubmit` of a session instead — that event's
`additionalContext` is confirmed working. Preserve INV-4 byte-stability by
gating it to fire exactly once per session (daemon-side
`first_prompt_seen(session_id)` state), not on every turn. This needs a
design decision at the start of Phase 2, not just a confirmation step.

### 5.2 PreToolUse matcher `Agent` — the enforcement point (subagent routing)

1. Parse `tool_input`: prompt, `model` param if present. Derive `Scope`.
2. Run signal providers (with a hard 30ms combined budget inside the daemon;
   providers that miss the budget are skipped this call).
3. Kernel → `Decision`.
4. Modes:
   - `advise` (default): return `additionalContext` with the recommendation; allow.
   - `enforce`: return `permissionDecision:"allow"` + `updatedInput` with the
     rewritten `model` (and `effort` IF the Agent tool schema supports it — see
     §10 verification tasks). `permissionDecisionReason` = `Decision.Reason`.
5. Per INV-1: enforce-mode may always raise tier/effort; it may lower them only when
   `Decision.Confidence ≥ config.downshift_threshold` (default 0.8).
6. Log the decision.

`[AMENDED 2026-08-02, see docs/verified.md §10.1/§10.2]` Both confirmed
live. §10.1: `PreToolUse` **does** fire for the `Agent` tool call itself
(`tool_name:"Agent"`, full `tool_input` visible), immediately followed by
a `SubagentStart` event. Whether it also fires for tool calls made
*inside* the subagent's own turns is still open — the live test's
subagent made no tool calls of its own. §10.2: the Agent tool's
`tool_input` has **no `effort` key at all** — confirmed both from a real
captured payload and from the tool's own schema
(`description, isolation, model, prompt, run_in_background,
subagent_type`). Step 4's parenthetical is resolved: effort enforcement
for subagents is frontmatter-only (the agent's own `.md` `effort:` field)
or via a system-message convention; `updatedInput` can rewrite `model`,
never `effort`, because the tool has nowhere to put it.

### 5.3 PreToolUse matcher `Bash` — output preprocessing (ship this FIRST)

Deterministic command rewrites that stop garbage entering context. Rule registry in
`internal/preprocess`, each rule = matcher + rewrite + estimated-savings note.
Launch set:

| Rule | Match | Rewrite |
|---|---|---|
| test-filter | `^(npm test|npx jest|pytest|go test|cargo test|mvn test)` | append `2>&1 \| grep -A 5 -E '(FAIL|ERROR|error:|panic:)' \| head -120` |
| build-filter | `^(npm run build|go build|cargo build|tsc\b)` | append stderr-only + head cap |
| log-tail | `cat .*\.(log|out)$` on files > 200KB | replace with `tail -n 200` + a grep for error patterns |
| diff-cap | `git diff` with no path args in large repos | append `--stat` first suggestion via additionalContext; do not rewrite silently |
| lint-filter | `^(eslint|golangci-lint|ruff)` | machine-format + head cap |

Rules must be **conservative**: only rewrite when the rewrite cannot change the
semantics the agent needs (a failing test's failure lines, not its noise). Anything
uncertain → `additionalContext` suggestion instead of `updatedInput` rewrite.
Every rewrite is logged with estimated tokens saved (bytes of typical output vs cap).
Per-rule enable/disable in config; `~` prefix on the Bash command string is NOT a
bypass here (that convention belongs to prompts) — instead honor an env
`DEADEYE_PREPROCESS=off` and per-rule config.

`[AMENDED 2026-08-02, see docs/verified.md pre-build check]` **The
test-filter rewrite as literally specified loses the exit code.**
Verified: `go test ./... 2>&1 | grep -E '(FAIL|panic:)' | head -20` exits
**0** on a failing suite whose raw exit is 1 — Claude Code would read a
failing test run as passing. `set -o pipefail` (prepended to the rewritten
command, not just the `grep` pipeline) restores exit 1, verified. Every
rule in this table needs an exit-code-preservation regression test before
it ships in Phase 1, not just a golden rewrite test.

### 5.4 The plan gate (plan-first flow, user-consented)

Two layers:

**Soft (UserPromptSubmit):** classify the prompt. If implementation-shaped AND at
least one trigger holds — (a) vague enough to cause broad scanning, (b) multi-file
scope, (c) provider-reported blast radius > 0 — inject `additionalContext`:
"Before editing files for this task, present a short plan (approach, files to touch,
verification step) and wait for the user's go-ahead." Also emit a marker into the
daemon: `pending_plan(session, task_hash)`.

**Hard (PreToolUse matcher `Edit|Write`):** if `pending_plan` is set for this task
and no consent recorded → return `permissionDecision:"ask"`,
reason = "Plan-first gate: this looks like a multi-file/high-radius change and no
plan was approved. Approve to proceed, or ask for a plan." The native permission
prompt IS the consent surface — do not build a custom one.

Consent handling (INV-7): the user's allow on that prompt records
`gate_accepted`; task goes quiet. A decline or an explicit "just do it" records
`gate_declined` and silences the gate for the task. Both are outcome signals
(§8) — especially `gate_declined` followed later by `revert`.

**Threshold discipline:** single-file, specific, low-radius prompts must pass
through with zero friction. Tune the classifier against this before enabling the
hard layer by default. Hard layer default: `off` until the soft layer's precision
is verified on real usage; then default `advise`→`ask` only on trigger (c).

Additional advisory: when the session model is `opusplan` and the soft gate fires,
the injected context should also say "consider entering plan mode (Shift+Tab) so
planning runs on Opus" — this is what makes opusplan actually pay off.

`[AMENDED 2026-08-02]` Note for the hard layer: `PermissionRequest` hooks
(a distinct event from `PreToolUse`) can set session permission mode via
`decision.updatedPermissions:[{type:"setMode",...}]` — not re-verified
live this session, carried forward from binary-introspected findings.
Worth evaluating as a stronger mechanism than `permissionDecision:"ask"`
when Phase 4 starts, but `PreToolUse`'s `ask` decision (as specified
above) is already confirmed to work mechanically (§10.3) and is the
simpler default.

### 5.5 Workflow advisor (recommend-only, INV-2)

In UserPromptSubmit: if the task shape is a fan-out over many independent units
(repo-wide audit, N-file migration, cross-checked research; signals: file_count
high, per-unit independence, "every/all/across the codebase" phrasing) AND the
environment supports workflows (version ≥ 2.1.154, session model supports xhigh),
inject a one-line suggestion mentioning the `ultracode` keyword and the cost
caveat. Never more than once per task. Rule-based only — no learning on this axis
(too few, too expensive samples).

### 5.6 Bundled skills & rules (context-efficiency layer)

Adopted from studying `huzaifa525/claude-code-optimizer` (see §14). Ship these as
plugin `skills/` and `rules/`, not files copied into the user's `~/.claude`:

- **`explore` skill** — progressive-disclosure exploration in a FORKED subagent
  (`context: fork`): Layer 1 = file index only (Glob/find, zero reads); Layer 2 =
  entry points + grep'd signatures/exports; Layer 3 = targeted reads with
  offset/limit, only for files directly relevant to the upcoming change. Explicit
  decision point after each layer: "enough context? stop and return summary."
  Returns a structured summary; raw contents never reach main context. Route the
  fork to tier 0/1 via the kernel.
- **Read-only analysis skills fork by default** — any bundled review/audit-style
  skill sets `context: fork` so its reads stay isolated.
- **Anti-waste rules** (injected as a compact rules file, path-scoped where the
  plugin rules format supports `paths:` frontmatter — verify, §10.9): never
  re-read a file already read this session; Grep→targeted Read with offset/limit
  instead of full-file reads; head/tail-cap verbose commands; batch independent
  searches; don't re-read a file after editing it to "verify".
- **Skill-router index** — if we ship >2 skills, a ≤15-line intent→skill table in
  the injected rules so the cheap path is discovered (counts against the INV-4
  400-token budget; trim skills before trimming the kernel guidance).

`[AMENDED 2026-08-02, see docs/verified.md V9]` **A plugin cannot ship
`rules/` at all — confirmed, this is not "verify §10.9", it's resolved
no.** The plugin manifest schema has no `rules` field, and no installed
plugin ships a Claude-Code-facing `rules/` directory (other agent hosts'
`.cursor/rules/`, `.windsurf/rules/`, etc. are unrelated). Claude Code's
native rules loader only reads `.claude/rules/**.md` at the project, user,
or managed-settings level, keyed on a `globs:` frontmatter field — never
from a plugin. **`paths:` is a *skill* frontmatter field, not a rules
field** (`"Glob patterns this skill applies to. The skill only loads when
the model touches matching files."`). Reframe: the "anti-waste rules"
content ships as a skill with a `paths:` constraint (or as unconditional
skill content, or folded into the SessionStart-replacement injection once
§5.1 is redesigned), never as a `rules/` directory — that component
doesn't exist for plugins.

### 5.7 Session memory (cross-session orientation)

Kills the re-orientation tax (a fresh session burns 15–40 tool calls rediscovering
the project). Mechanism, in the daemon rather than loose bash:

- `Stop`/`SessionEnd` hook: write a compact summary (branch, recent commits,
  modified/staged files, plugin decisions made) to
  `~/.deadeye/sessions/<project>_<ts>.md`. Skip when the session had no
  meaningful activity.
- `SessionStart`: inject the head (≤25 lines) of the most recent summary for this
  project. Injected once, stable for the session — INV-4 compliant.
- Freshness guard: skip files younger than ~30s (same-session artifacts).
- **Check for native overlap first (§10.10):** Claude Code has native
  resume-from-summary on Pro/Max. Our memory must complement, not duplicate — if
  the native summary is active for the session, inject nothing.

`[AMENDED 2026-08-02]` This entire mechanism depends on `SessionStart`
injection, which §5.1's amendment establishes does not reach the model.
Phase 1.5 needs the same redesign §5.1 needs — likely: inject via the
same once-per-session `UserPromptSubmit` gate instead of a dedicated
`SessionStart` path. Do not build §5.7 before §5.1's redesign lands.

### 5.8 Effort management

- Generated/managed subagents & skills: set `effort` frontmatter per kernel decision.
- `PreToolUse Agent`: include effort in `updatedInput` if the schema supports it
  (§10); otherwise effort guidance goes through frontmatter and SessionStart context
  only.
- Detect `CLAUDE_CODE_EFFORT_LEVEL` at daemon start: if set, the effort axis is
  inert — surface this loudly in `/deadeye-status` rather than silently doing nothing.
- Never automate `max` or `ultracode` (session-only, unbounded side).

`[AMENDED 2026-08-02, see docs/verified.md §10.2 and V3]` Two corrections.
First: confirmed the Agent tool schema has no `effort` parameter at all
(§10.2) — "include effort in `updatedInput` if the schema supports it" is
resolved to "it doesn't; frontmatter only," not an open question. Second:
`effort.level` arrives directly in every hook's `Input` (`hookio.Effort`,
confirmed live in every captured `PreToolUse`/`PostToolUse`/`Stop`
payload) — there is no need to sniff an env var to detect the session's
effort level at all; read it from the hook payload each call. The env var
actually present in the hook environment is `CLAUDE_EFFORT`, not
`CLAUDE_CODE_EFFORT_LEVEL` — if an inertness check via env var is still
wanted as a fallback for contexts with no hook payload (e.g. daemon
startup, before any hook has fired), use the correct name.

---

## 6. Slash commands

- `/deadeye-status` — modes per axis, live tier table + prices + fetch age, whether any
  env overrides make an axis inert, daemon health.
- `/deadeye-route <optional task description>` — dry-run the kernel on the current
  context or the given description; print the Decision with full reasoning and each
  provider's Evidence. Trust requires explainability.
- `/deadeye-audit` — from the decisions/outcomes tables: decisions per surface, estimated
  tokens saved by preprocessing, up/downshift counts, gate accept/decline rates,
  outcome-weighted accuracy per (model, task_shape). Also cross-reference the user
  toward `/usage`'s plugin attribution as ground truth.
- `/deadeye-config` — guided edit of `~/.deadeye/config.json`.

`[AMENDED 2026-08-02]` Phase 0 ships `/deadeye-status` only. `/deadeye-route`,
`/deadeye-audit`, `/deadeye-config` have nothing to report until their
respective phases (2, 1, ongoing) land — shipping them earlier means
shipping empty commands.

---

## 7. Configuration

`~/.deadeye/config.json` (global) overlaid by `.deadeye.json` (project). Schema
published for IDE validation. Shape:

```json
{
  "$schema": "https://raw.githubusercontent.com/<user>/deadeye-cc/main/schema/config.schema.json",
  "mode": { "routing": "advise", "effort": "advise", "preprocess": "on",
             "plan_gate": "soft", "workflow_hint": "on" },
  "downshift_threshold": 0.8,
  "tiers": { "override": null },
  "preprocess": { "disabled_rules": [] },
  "plan_gate": { "min_files": 2, "radius_trigger": true },
  "injection_budget_tokens": 400
}
```

Kill switches: `DEADEYE=off` (everything), `DEADEYE_PREPROCESS=off`,
`DEADEYE_GATE=off`. Prompt-level bypass: leading `~` on a prompt skips
UserPromptSubmit classification for that prompt (log it as OVERRIDE).

**User-facing postures** (adopted concept from CCO's `/mode`, mapped onto our
config rather than a parallel system): `posture: "frugal" | "balanced" | "quality"`
is a preset that adjusts `downshift_threshold`, default effort bias, and how
aggressive the anti-waste rules injection is. `balanced` is default. Presets only
set config values — no separate code path, so `/deadeye-route` explanations stay true
under any posture.

`[AMENDED 2026-08-02]` Phase 0's `internal/config.Config` implements only
`mode`, `downshift_threshold`, `posture`, `injection_budget_tokens` — the
subset actually read so far. `schema/config.schema.json` documents the
full shape above (including `preprocess.disabled_rules`,
`plan_gate.{min_files,radius_trigger}`, `tiers.override`) as the target;
unknown JSON keys are silently ignored by `encoding/json` until each
later phase adds the matching Go field, so a config file written against
the full schema never errors against the narrower Phase 0 struct.

---

## 8. The learning loop (lessons)

**Task shape** = a small categorical feature vector, hashed: {size bucket, file
count bucket, has_tests, vague/specific, mechanical/implementation/reasoning,
radius bucket}. Keep it coarse — priors need density, not precision.

**Signals and weights** (INV-9):

| Signal | Detection | Weight |
|---|---|---|
| Manual escalation | user/agent picks higher tier or effort than assigned, same task | 1.0 negative on assigned cell |
| Revert | `git revert`/checkout of files the subagent wrote, within task window | 1.0 negative |
| Test failure post-completion | test run fails on touched files after "done" | 0.8 negative |
| User correction | next prompt is a correction-shaped follow-up on same files | 0.6 negative |
| Gate declined → later revert | combo | 1.0 negative on the decline (validates the gate) |
| Manual downshift | user picks lower than assigned, task completes clean | 0.7 positive on the LOWER cell |
| Overthink | thinking tokens ≫ expected for task size (effort axis only — visible in transcript/usage) | 0.5 negative on effort level |
| Clean completion | no negative signal in window | 0.05 positive |

Priors update on each outcome; the kernel blends prior with rule-based estimate,
prior weight growing with `n` (simple Beta-style blending is fine — do not
over-engineer this; it must remain inspectable). Priors are keyed per
`(model, task_shape, effort)` — effort levels are calibrated per model and lessons
must not transfer across models.

Detection sources: PostToolUse hooks (tool results), transcript path from hook
input, git state. Build detection incrementally — start with escalation +
gate outcomes (cheap, unambiguous), add revert/test-fail in a later phase.

---

## 9. Build phases (in order — each phase ships independently useful)

**Phase 0 — skeleton & plumbing** `[DONE 2026-08-02]`
Repo layout, plugin manifest, hooks.json, daemon + unix-socket client, hookio types
against the real hook JSON protocol, SQLite store, config loader, kill switches,
fail-open wrappers, goreleaser config. Definition of done: plugin installs from
marketplace, `deadeye hook` round-trips a no-op `{}` on every registered event in <50ms,
`/deadeye-status` renders.

`[AMENDED 2026-08-02]` Shipped with JSONL in place of SQLite (§3.3). All
Definition-of-Done items confirmed live except the interactive
`/plugin marketplace add` + `/plugin install` flow specifically (validated
instead via `claude plugin validate --strict` plus headless
`--plugin-dir` sessions, which exercise the same manifest/hook loading
path) — see `docs/verified.md`'s closing section for what to double-check
interactively before the first public release.

**Phase 1 — output preprocessing + context-efficiency layer** `[DONE 2026-08-02]`
(largest safe wins, no learning needed)
The 5 launch preprocessing rules, per-rule config, savings estimation + logging,
`/deadeye-audit` showing tokens-saved. Verify with `claude --debug` that `updatedInput`
rewrites land (`modified tool input keys: [command]`). PLUS the bundled `explore`
skill (§5.6) and the anti-waste rules injection — these need no kernel and no
catalog, and together with preprocessing they attack the two biggest spend
categories (verbose output + exploration) in the first release.
NOTE: preprocessing MUST be `PreToolUse` + `updatedInput`. CCO filters test
output on `PostToolUse` — by then the full output has already entered context and
the filtering saves nothing. Do not copy that mistake; add a regression note in
docs.

Phase 1 also ships the first cut of the GitHub Pages site (§12.1): hero +
preprocessing before/after + install + FAQ. The site grows a section per phase
thereafter.

`[AMENDED 2026-08-02]` Two corrections for Phase 1 specifically: (a) every
rewrite rule needs an exit-code-preservation test, not just a golden
rewrite test (§5.3 amendment — `set -o pipefail` is required, the naive
pipe silently reports failing tests as passing); (b) "the anti-waste
rules injection" ships as a skill with `paths:` frontmatter, not a
`rules/` directory (§5.6/V9 amendment) — plugins cannot ship `rules/` at
all.

`[DONE 2026-08-02]` Shipped: the 5 rules with exit-code tests (plus a
second live-caught bug — a silent-but-correct exit 0 made a real session
distrust and retry a passing test run five times; every rewrite now
guarantees at least one line of output), the `explore` skill, and
`/deadeye-audit`. The Pages site shipped with the Final pass instead of
here, once real `/deadeye-route`/`/deadeye-audit` output existed to embed
per §12.1's "reuse actual output as the illustration."

**Phase 1.5 — session memory** (§5.7, small, independent) `[DONE 2026-08-02]`

`[AMENDED 2026-08-02]` Blocked on §5.1's redesign landing first — §5.7's
`SessionStart` injection has the identical "does not reach the model"
problem. Do not start this phase before Phase 2's SessionStart redesign
is decided (even though Phase 1.5 numerically precedes Phase 2 — the
dependency runs backward from the original phase order).

`[DONE 2026-08-02]` Shipped via the same once-per-session
`UserPromptSubmit` injection Phase 1 built. Confirmed live across two
real sessions in the same repo. No native-resume-overlap guard (§10.10
still unresolved) — flagged inline, not silently built in.

**Phase 2 — catalog resolver + effort axis** `[DONE 2026-08-02]`
Catalog fetch/rank/cache (§4), grid model in the kernel, effort recommendations via
SessionStart + frontmatter, `CLAUDE_CODE_EFFORT_LEVEL` inertness detection,
`/deadeye-route` dry-run.

`[AMENDED 2026-08-02]` "Catalog fetch/rank/cache" is already done (Phase 0
shipped the compiled-in table + override, §4 amendment) — this phase now
starts from the grid model in the kernel. "Effort recommendations via
SessionStart" needs the §5.1 redesign decided first (this is the phase
that redesign blocks). "`CLAUDE_CODE_EFFORT_LEVEL` inertness detection"
should check `CLAUDE_EFFORT` (the confirmed real env var) as a fallback
only — the primary source is `effort.level` in every hook payload,
already available with no detection logic needed.

`[DONE 2026-08-02]` `internal/kernel.Decide` shipped conservative by
construction, with property tests documenting a real limit found while
writing them: the plan's literal "removing any single element from any
evidence set never lowers the decision" is mathematically impossible for
a kernel where evidence can legitimately swing the decision either way
(proof in `kernel_test.go`). What's actually true and tested is narrower
but still real: empty evidence is always the ceiling, one low-confidence
or high-complexity signal blocks/forces it. `/deadeye-route` ships.

**Phase 3 — subagent model routing** `[DONE 2026-08-02]`
Built-in signal providers, PreToolUse `Agent` in advise mode; enforce mode behind
config after the §10 verifications pass. Optional greybeard provider last.

`[AMENDED 2026-08-02]` The relevant §10 verifications (§10.1, §10.2)
already passed in Phase 0 — this phase can start directly from "built-in
signal providers" without a preliminary verification step. Confirmed:
`PreToolUse` fires for the `Agent` tool call with full `tool_input`
visible; enforcement can rewrite `tool_input.model` via `updatedInput`.

`[AMENDED 2026-08-02, see docs/verified.md V10 — corrects a Phase 0
finding]` **`updatedInput` does NOT have merge semantics — it replaces.**
Phase 0's §10.3 finding ("only the `model` key needs to be present") was
wrong; it only looked correct because that test used Bash, whose other
`tool_input` fields are all optional. Sending `{"model": "haiku"}` alone
for a real Agent delegation got the call rejected live
(`The required parameter "description" is missing`) and the model itself
reported the tool as broken. Fixed by merging client-side
(`hookio.MergeToolInput`) before ever building `updatedInput` — always
send the full tool_input with only the intended field(s) changed, for
every tool, not just ones known to have required fields beyond the
rewritten one.

`[DONE 2026-08-02]` Advise mode attaches the recommendation as
`additionalContext`; enforce mode rewrites `tool_input.model` to the
family alias, only when the caller left `model` unset. Confirmed live:
three consecutive enforced delegations, zero schema-validation failures
after the merge fix above.

**Phase 4 — plan gate** `[DONE 2026-08-02]`
Soft layer first; measure precision on real usage (false-fire rate on small tasks);
then hard layer default-off → opt-in.

`[DONE 2026-08-02]` Both layers shipped. Confirmed live: the soft
suggestion lands correctly, and the hard layer blocked a real `Write`
call end to end (`Write tool permission denied`, file never created).
New finding: no hook surface reports back which way the user answered a
permission prompt, so the gate clears itself right after asking once
rather than staying pending indefinitely. Also new: in headless `-p` mode
with no TTY, `permissionDecision:"ask"` auto-denies rather than blocking
for input.

**Phase 5 — workflow advisor** (small, rule-based) `[DONE 2026-08-02]`

`[DONE 2026-08-02]` Fan-out phrasing on an implementation/audit-shaped
prompt gets a one-line suggestion, at most once per task. No
version/model-capability gate — no hook-visible signal for either exists.

**Phase 6 — learning loop** `[DONE 2026-08-02]`
Outcome detection (escalation + gate first), priors, blending, audit views.

`[DONE 2026-08-02]` Gate-outcome detection turned out not to be one of
the cheap ones after all: Phase 4 already found no hook surface reports
back a permission prompt's answer, so only escalation detection shipped
(revert/test-fail remain real future work, per the plan's own incremental
guidance). Building this surfaced the session's most consequential bug:
all four builtin signal providers had confidence below the default
`downshift_threshold`, and `kernel.Decide` requires the MINIMUM
confidence across all evidence to clear it — so downshifting was
architecturally unreachable through the kernel since Phase 2, unnoticed
until Phase 6 needed a genuine downshift to escalate from. See
`docs/verified.md` V11 for the two-bug fix (provider recalibration, then
a second live-caught issue where a lone trailing "?" alone tanked
confidence). `/deadeye-audit` now reports escalation counts per task shape.

---

## 10. Empirical verification tasks — DO THESE BEFORE BUILDING ON THEM

Do not trust documentation or this plan for the following; test in a live Claude
Code environment and record findings in `docs/verified.md` with version numbers:

1. **Does `PreToolUse` fire inside the dynamic-workflow runtime?** This decides
   whether routing enforcement covers the highest-spend path. Test: register a
   logging PreToolUse hook, trigger a small workflow, check the log. If it does NOT
   fire, workflow coverage is limited to the SessionStart script-authoring guidance
   — document that limit prominently.

   `[AMENDED 2026-08-02, see docs/verified.md §10.1]` **Partially answered.**
   Confirmed live: `PreToolUse` fires for the `Agent` tool call itself
   (the delegation call, immediately followed by `SubagentStart`). Not yet
   exercised: whether it also fires for tool calls made *inside* a
   running subagent's own turns, or inside a full `Workflow` tool run
   specifically (as opposed to a single `Agent` delegation) — the live
   test used a direct Agent-tool delegation, not the Workflow tool. Still
   open for Phase 3/5.

2. **Does the Agent tool's input schema accept an `effort` parameter** (alongside
   `model`)? Inspect the actual tool schema in a session. If not, effort enforcement
   is frontmatter-only.

   `[AMENDED 2026-08-02, see docs/verified.md §10.2]` **Answered: no.**
   Confirmed via both a real captured `tool_input` payload and the Agent
   tool's own schema. Effort enforcement for subagents is frontmatter-only.

3. **Exact PreToolUse JSON contract on the current version**: field names, the
   `hookSpecificOutput.permissionDecision` + `updatedInput` shape, `ask` behavior,
   `additionalContext` availability per event. Build `hookio` types from observed
   payloads, not docs.

   `[AMENDED 2026-08-02, see docs/verified.md §10.3]` **Done — with two
   real bugs caught in the process**, both now fixed and regression-tested:
   `hookSpecificOutput.hookEventName` is required (not optional as the
   docs-derived first draft assumed); `SessionStart` does not accept
   `hookSpecificOutput` at all. `internal/hookio` and `testdata/payloads/`
   are now built from real captured payloads plus the live validator's own
   schema dump, not docs.

4. **Can a hook set/force permission mode (plan mode)?** Hook input carries
   `permission_mode` read-only as far as known. Confirm there is no setter; if one
   exists, the plan gate gains a stronger mechanism.

   `[AMENDED 2026-08-02, see docs/verified.md §10.4]` Not re-verified live
   this session; carried forward unchanged: `permission_mode` is read-only
   on `PreToolUse`, but `PermissionRequest` hooks can set session mode via
   `updatedPermissions`/`setMode` — a different, narrower event. Still
   open for Phase 4 to exercise live.

5. **UserPromptSubmit `additionalContext` token accounting** — confirm injected
   context does not bust the prompt cache when stable, and measure the actual
   overhead of our injections.

   Still open. Note: `UserPromptSubmit`'s `additionalContext` is now
   confirmed to actually reach the model (used as the positive control for
   the §5.1 finding) — the mechanism itself works; only the cache/overhead
   accounting remains untested.

6. **`/usage` plugin attribution** — confirm the plugin's own overhead shows up
   there, and keep our overhead under 2% of session usage. Still open.

7. **Model catalog endpoint** reachability + schema (§4).

   `[AMENDED 2026-08-02, see docs/verified.md V7]` **Done — dead end
   confirmed.** No API key in the hook environment; `/v1/models` returns
   401 and carries no pricing field even when authenticated. See §4's
   amendment for the resolution (compiled-in table).

8. **Marketplace identifier check (naming is DECIDED — this is confirmation):**
   confirm what identifier `/plugin marketplace add deepaksinghcs14/deadeye-cc`
   registers, and therefore whether `/plugin install deadeye@deadeye-cc` is the
   correct install line. If the marketplace name is settable independently of the
   repo name, set it explicitly. Also confirm `deadeye` is unclaimed in the
   plugin directories (claudepluginhub, vibed-lab, tonsofskills). Correct the
   install snippet in `README.md` AND `docs/site/index.html` if it differs —
   those two must never disagree.

   `[AMENDED 2026-08-02, see docs/verified.md V1/V2]` **Done, and it does
   differ.** The marketplace name is settable independently and IS
   explicit (`marketplace.json.name: "deadeye"`, confirmed both live and
   from the binary's manifest schema). Correct install line:
   `/plugin install deadeye@deadeye`, not `@deadeye-cc` — apply this
   correction in README and the site (neither exists yet; apply when
   Phase 1 creates them). Unclaimed-name check across plugin directories:
   still open, not done this session.

9. **Do plugin-shipped rules support `paths:` frontmatter scoping** (as
   project-level `.claude/rules/` files do)? Decides whether anti-waste rules can
   be path-scoped or must be global-but-tiny.

   `[AMENDED 2026-08-02, see docs/verified.md V9]` **Resolved: plugins
   cannot ship `rules/` at all**, scoped or otherwise — not merely
   "unconfirmed." `paths:` turns out to be a *skill* frontmatter field, not
   a rules field. See §5.6's amendment for the reframed approach.

10. **Native resume-from-summary interaction** — determine how to detect (from
    hook input or environment) whether the session was resumed from a native
    summary, so §5.7 session memory can stand down instead of double-injecting.
    Still open; also now secondary to §5.7's blocker on the §5.1 redesign.

11. **Confirm `MAX_THINKING_TOKENS` is inert on adaptive-reasoning models** in the
    current version (docs say nonzero budgets are ignored there). This is both a
    design confirmation for our effort-first approach and a documented
    differentiator vs. CCO, whose global thinking cap is a no-op on current
    models. Still open — not tested this session.

---

## 11. Testing strategy

- `internal/kernel`: exhaustive table-driven tests. Include INV-1 property tests:
  for any evidence set, removing evidence must never lower the decided tier/effort.
- `internal/preprocess`: golden tests per rule — input command → rewritten command;
  plus "must-not-rewrite" cases (commands that look similar but aren't safe).
- `internal/catalog`: fixture responses; tier stability under price-order changes;
  family/price disagreement path.
- Hook protocol: golden stdin→stdout tests against captured real payloads from §10.
- Integration: a scripted Claude Code session (headless `claude -p`) exercising each
  hook, asserting on the decision log. Mark as CI-optional (needs credentials).
- Latency: benchmark the daemon round-trip; fail CI if p95 > 50ms.

`[AMENDED 2026-08-02]` The two Phase-0-relevant items are done and set the
pattern for later phases: `internal/hookio`'s golden tests now run against
real captured payloads in `testdata/payloads/` (see §10.3's amendment for
why this mattered — it caught two real bugs a docs-only test would have
missed), and the daemon latency benchmark runs in `cmd/deadeye/
daemon_test.go` (`TestDaemonRoundTripP95`, in-process rather than a
spawned subprocess — see `docs/verified.md` V8 for why that's an
equivalent-or-tighter bound, plus a macOS unix-socket-path-length gotcha
worth knowing before extending it).

---

## 12. Distribution & ops

- goreleaser: darwin/linux/windows × amd64/arm64; self-bootstrap launcher that
  downloads once from Releases with sha256 verification; a binary already on PATH
  always wins (same pattern as greybeard).
- Install: `/plugin marketplace add <user>/deadeye-cc` → `/plugin install ...`.
- `deadeye uninstall --purge` removes binary, socket, and `~/.deadeye`.
- No telemetry, no phone-home. One SQLite file, local.
- README leads with the before/after: a raw 30k-token `npm test` dump vs the
  filtered 120 lines, and the `/deadeye-audit` savings line. Preprocessing is the demo;
  routing is the depth.

`[AMENDED 2026-08-02]` "One SQLite file, local" → one JSONL file, local
(§3.3 amendment). Install line correction per V1/V2: `/plugin install
deadeye@deadeye`.

### 12.1 GitHub Pages site (required deliverable, same pattern as greybeard)

A static site at `<user>.github.io/deadeye-cc/`, built from `docs/site/` and
deployed via a GitHub Actions workflow. Single fast landing page plus a docs
section — no framework heavier than a static generator.

**Page structure (in order):**
1. **Hero** — name, the metaphor line (*he doesn't waste shots*) followed by the
   plain promise ("fits the model, effort, and context to the task — fewer
   tokens, same quality"), install snippet
   (`/plugin marketplace add <user>/deadeye-cc` → `/plugin install deadeye@deadeye-cc`),
   and the before/after: the raw test-output dump beside the filtered version with
   REAL token counts.
2. **How it decides** — compact diagram of the kernel: signals → grid search over
   (model, effort) → decision → log. Reuse actual `/deadeye-route` output as the
   illustration; explainability is the trust story.
3. **"Measured, not estimated"** — screenshot of `/deadeye-audit`, statement
   that every figure comes from the decision log and cross-checks against
   `/usage`. This is the contrast with competitors' invented savings math — make
   the claim without naming names on the site (the README comparison table can
   name them).
4. **The five axes** — short cards: preprocessing, effort, model routing, plan
   gate, workflow advisor; each showing its mode (`off|advise|enforce`).
5. **Docs** — install, configuration reference GENERATED from
   `schema/config.schema.json` at site build time so docs cannot drift from the
   schema, hooks reference, FAQ (no telemetry / one SQLite file / kill switches),
   uninstall.
6. **Changelog** link + version badge.

`[AMENDED 2026-08-02]` Item 1's install snippet: `/plugin install
deadeye@deadeye`, not `@deadeye-cc` (V1/V2). Item 5's FAQ: "one SQLite
file" → "one JSONL file" (§3.3).

**Build notes:** OG meta tags + social card (the before/after image works as the
card); site URL in the repo About field and in `plugin.json`. Site copy follows
the same rule as the plugin: NO fabricated percentages anywhere — until measured
aggregates exist, the hero shows the one concrete before/after, not a claimed
average. CI: site build on every PR touching `docs/site/` or the schema; deploy
on release tags only. Ships with Phase 1 (a landing page with the preprocessing
demo), grows a section per phase.

---

## 12.2 Deliverables checklist

Repo is complete when all of these exist and CI is green:

- [x] `cmd/deadeye/` — binary: daemon, CLI, hook client
- [x] `internal/kernel/` with property tests for INV-1 — see the Phase 2
      amendment on what those property tests actually prove
- [x] `internal/{signals,catalog,preprocess,lessons,store,hookio}/` — `catalog`,
      `hookio` done (as `logstore`, not `store` — §3.3); `signals`,
      `preprocess`, `lessons`, `kernel`, `inject`, `sessionmem` all shipped
      across Phases 1-6
- [x] `.claude-plugin/plugin.json`, `hooks/hooks.json`
- [x] `commands/deadeye-{status,route,audit}.md` -- `deadeye-config` was
      never built: nothing in `~/.deadeye/config.json` needs guided editing
      beyond hand-editing the JSON against the published schema, and no
      phase surfaced a real need for it
- [x] `skills/` — `explore` (forked, progressive disclosure). The
      anti-waste rules content shipped folded into the once-per-session
      injection (§5.6/§5.1 amendments), not as a second skill or a
      `rules/` directory (which plugins can't ship at all, V9)
- [x] `schema/config.schema.json` — `scripts/gen-config-docs.py` was never
      built: the site's config reference is hand-written directly against
      the schema (§12.1 amendment) rather than generated by a separate
      script, a scope cut given the schema is small and stable
- [x] `docs/PLAN.md`, `docs/verified.md` — §10 findings with version numbers.
      `[AMENDED 2026-08-02]` Per explicit user instruction, `docs/` (this
      plan + verified.md) is gitignored and stays local -- internal working
      docs, not pushed. `docs/site/` is carved out as an exception (it's the
      public Pages site, a different thing).
- [x] `.goreleaser.yaml`, `Makefile`, `CHANGELOG.md`, `LICENSE` (MIT)
- [x] `README.md`, `docs/site/`, Pages workflow, `.github/workflows/release.yml`
      -- the site shipped in the Final pass (once `/deadeye-route` and
      `/deadeye-audit` had real output to embed, per §12.1's "reuse actual
      output as the illustration"), not with Phase 1 as originally planned.
      `[AMENDED 2026-08-02]` `assets/logo.svg` and `assets/logo-dark.svg`
      DO already exist, appearing mid-Phase-0 without an explicit request
      or commit recording who/what created them -- flagged to the user
      rather than assumed benign-by-default; content reviewed (pure SVG
      paths/shapes, no `<script>` or external refs) and kept since it
      matches this section's spec exactly, but its provenance is not
      clean and was worth the user's own look.

## 12.3 Definition of done, per phase

A phase ships only when: its tests pass; `make check` is green; hook latency p95
< 50ms; the axis is documented in the README and on the site; the decision log
records its output; and a real session has been run end-to-end with the axis in
`enforce` mode without a false fire.

## 13. Explicit non-goals

- Changing the MAIN session model mid-session (impossible reliably; INV-3).
- Auto-enabling workflows, `ultracode`, or `max` effort (INV-2).
- Multi-provider routing / LLM gateways (different product).
- Depending on greybeard (optional signal provider only, shell-out, degradable).
- A custom consent UI (native permission prompts only).

## 14. Prior art: `huzaifa525/claude-code-optimizer` (CCO) — adopt / reject

Studied in full (v4.x: 25 skills, 13 bash hooks, 6 rules, npm-global installer).
Closest competitor by positioning; structurally different product.

**Adopted (credited where visible):**
- Progressive-disclosure exploration in forked subagents → our `explore` skill
  (§5.6). Their best idea.
- Path-scoped rules via `paths:` frontmatter → §5.6, pending §10.9.
- `context: fork` default for read-only analysis skills → §5.6.
- File-based session memory (Stop→summary, SessionStart→inject) → §5.7, rebuilt
  in the daemon with a native-overlap guard.
- Anti-waste behavioral rules (dup-read prevention, grep-before-read,
  offset/limit, output capping) → §5.6 rules injection.
- Skill-router intent index → §5.6.
- User-facing optimization postures → §7 presets.

`[AMENDED 2026-08-02]` "Path-scoped rules via `paths:` frontmatter" —
`paths:` is real and works, but as a *skill* field, not a rules field
(V9). The mechanism is adopted; the delivery vehicle described here
(a plugin `rules/` directory) does not exist and was replaced in §5.6.

**Rejected, with reasons (keep these in the README's comparison section):**
- **Global `MAX_THINKING_TOKENS` cap as the primary lever.** Inert on
  adaptive-reasoning models (Sonnet 5, Opus 4.7+, Fable) — effort levels are the
  real mechanism. Our effort axis (§5.8) is the working successor. (§10.11)
- **PostToolUse output filtering.** Filters after the output already entered
  context; saves nothing. Ours is PreToolUse `updatedInput` (§5.3).
- **Fabricated savings math.** Their footer computes `tool_calls×1000+5000` and
  presents it as savings; the "67%" claim rests on it. We report only measured
  quantities: logged decisions, bytes prevented from entering context, and we
  point at `/usage` as ground truth. Never ship an invented number.
- **npm-global install that copies into `~/.claude/` and occupies the user's
  settings/hooks space.** We are a real marketplace plugin: bundled hooks.json,
  our own state dir, nothing of the user's mutated (INV-3), clean uninstall.
- **Hardcoded Sonnet/Opus/Haiku guidance.** Rots on every model release; our
  catalog resolver (§4) derives tiers from live pricing.
- **No enforcement, no state, no learning.** Everything is advisory prose;
  nothing is measured; nothing improves. The kernel + decision log + lessons loop
  (§3, §8) is the moat.

**Positioning line for the README:** CCO tells Claude how to save tokens; this
plugin *decides, enforces, measures, and learns* — and its savings numbers are
real.

---

## 15. Start here (first session instruction)

Paste this to Claude Code to begin:

> Read `PLAN.md` in full. Then:
> 1. Execute **Phase 0** (§9): repo skeleton, plugin manifest, `hooks.json`,
>    daemon + unix-socket client, `hookio` types, SQLite store, config loader,
>    kill switches, fail-open wrappers, goreleaser.
> 2. Run verifications **§10.1–§10.4** against a live Claude Code session and
>    write findings to `docs/verified.md` with version numbers. Build `hookio`
>    types from the payloads you actually observe, not from the plan's
>    description of them.
> 3. If any finding contradicts the plan, amend `PLAN.md`, state what changed
>    and why, and stop for review before proceeding.
>
> Do not start Phase 1 until §10.3 (the real PreToolUse contract) is recorded.
> Do not write any enforcement code until §10.1 and §10.2 are answered.

`[DONE 2026-08-02]` Phase 0 executed; §10.1–§10.4 (and V1, V2, V7, V8, V9,
and the §5.1 SessionStart finding beyond the original four) recorded in
`docs/verified.md`. This document is amended in place above, per instruction
3, rather than as a separate addendum — every amendment is tagged
`[AMENDED 2026-08-02]` inline at the section it corrects, with a pointer to
the evidence in `docs/verified.md`. Stopping here for review before Phase 1,
per instruction 3 and §12.3's per-phase definition of done.

`[DONE 2026-08-02]` All remaining phases (1 through 6) executed in the same
session on explicit user instruction to complete everything remaining, in
dependency order rather than strict numeric order (Phase 1.5 waited on
Phase 2's SessionStart redesign, both landing together). Every phase was
verified live against a real Claude Code session, not just unit-tested --
that discipline is what caught V10 (`updatedInput` doesn't merge) and V11
(downshifting was architecturally unreachable through the kernel), both
real bugs a docs-only or unit-test-only approach would have shipped
silently. `docs/verified.md` V1-V11 is the complete evidence trail.
Genuinely still open, not just deferred: §10.4 (hard confirmation of the
`PermissionRequest`/`setMode` mechanism), §10.5-§10.6 (token-cache
accounting, `/usage` attribution), §10.10 (native-resume-overlap
detection), §10.11 (`MAX_THINKING_TOKENS` inertness), and revert/test-fail
outcome detection for the learning loop. None of these block what shipped;
they're real future work, not silent gaps.

**The one-sentence version, if context is ever lost:** deadeye is a Go binary
behind Claude Code hooks that fits model, effort, and context to each task via a
deterministic policy kernel — enforcing on subagents, filtering tool output
before it lands, measuring everything it claims, and never letting missing
evidence buy a cheaper answer.
