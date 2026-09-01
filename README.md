<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.svg">
    <img src="assets/logo.svg" width="180" alt="deadeye, the kid who never wastes a shot">
  </picture>
</p>

<h1 align="center">deadeye</h1>

<p align="center">
  <em>He doesn't waste shots.</em>
</p>

<p align="center">
  <a href="https://github.com/deepaksinghcs14/deadeye-cc/actions/workflows/ci.yml"><img src="https://github.com/deepaksinghcs14/deadeye-cc/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <img src="https://img.shields.io/github/v/release/deepaksinghcs14/deadeye-cc?style=flat-square&color=111111&label=release" alt="Release">
  <img src="https://img.shields.io/github/license/deepaksinghcs14/deadeye-cc?style=flat-square&color=111111" alt="MIT license">
  <img src="https://img.shields.io/github/go-mod/go-version/deepaksinghcs14/deadeye-cc?style=flat-square&color=111111" alt="Go version">
  <img src="https://img.shields.io/badge/measured_reduction-79.6%E2%80%9399.5%25-C89A46?style=flat-square&labelColor=1B2127" alt="Measured token reduction 79.6 to 99.5 percent">
</p>

He doesn't check twice, and he doesn't spray and pray. One round chambered,
one shot on target, and he's done — because the shot you never had to take
is the best one. deadeye puts that discipline inside your coding agent.

It watches what your agent is about to do — spawn a subagent, dump a noisy
test log, make a big multi-file edit — and fits the model, effort level, and
context to the task **before the tokens are spent, not after**. Left alone, a
coding agent spends more than a task needs; deadeye catches that at the choke
points it already sits on. Everything it remembers lives in one local file:
no hosted service, no API keys, no telemetry.

**[Full feature tour, with live examples → the site](https://deepaksinghcs14.github.io/deadeye-cc/)**

<p align="center">
  <a href="https://deepaksinghcs14.github.io/deadeye-cc/soon.html"><img src="assets/soon-banner.svg" alt="Something's coming — join the waitlist" width="860"></a>
</p>

## Numbers

Every figure is measured from a real run and logged to
`~/.deadeye/decisions.jsonl` — nothing averaged, nothing modelled. The
rewrite happens *before* the command runs, so only the part worth keeping
ever enters context.

| Real run | Before | After | Cut |
|---|---:|---:|---:|
| `go test` with one genuine failure | 485 B | 99 B | **79.6%** |
| This repo's own suite (202 lines, all passing) | 10,301 B | 55 B | **99.5%** |
| `npm install express mocha` (progress spam) | 553 B | 55 B | **90.1%** |
| Coder persona a subagent inherits, per spawn | 7,666 B | 1,073 B | **86.0%** |

Run `/deadeye-stats savings` to see your own. Quiet no-op events (95% of all
rows on one real machine) aren't logged at all — the log holds only the rows
where deadeye actually did something.

## How it works

```
1. Look      Cheap, deterministic signals: files touched, recent git activity, whether tests exist nearby, how the request reads
2. Decide    The cheapest (model, effort) that should still clear the bar for this task
3. Apply     Rewrites the subagent's model, trims noisy command output, asks before risky multi-file edits
4. Learn     If you manually pick a bigger model than it recommended, it gets more cautious for that kind of task next time
```

The rule behind all four: **when unsure, a capable middle — not the priciest.**
Thin or shaky evidence defaults to the sonnet tier, reserving opus for genuinely
high-complexity work; picking cheaper needs real evidence above a confidence bar.
Optionally, an AI judge (`claude -p`, no API key) classifies the ambiguous cases
for a sharper call.
Every decision is printable: run `/deadeye-route` to see the full reasoning,
never a black box. deadeye advises by default and never touches your
`settings.json`; if anything inside it errors, your call passes through
untouched.

## Benchmark — routing savings

Does routing to a cheaper model actually save money, or just do worse work? We
measured it: every task run on all three tiers, graded by a **hidden test the
model never saw**, priced from real billed `total_cost_usd` (deadeye off during
runs, so it's raw per-tier model cost).

**Identical, verified-correct output cost 3–9× more on opus than haiku** (median
~6×). And **5 of 6 well-scoped tasks passed on haiku** — including SemVer
precedence and a concurrent counter under `-race` — so downshifting the common
case is nearly free in quality. The one task that needed opus (a recursive
expression evaluator) is exactly the case routing sends *up*.

| Task | Band | haiku | opus | opus ÷ haiku |
|---|---|---:|---:|---:|
| counter | hard | $0.034 | $0.311 | **9.1×** |
| wordwrap | standard | $0.046 | $0.331 | **7.1×** |
| clamp | mechanical | $0.026 | $0.160 | **6.1×** |
| semver | hard | $0.055 | $0.265 | **4.8×** |
| csv | standard | $0.060 | $0.204 | **3.4×** |

Oracle routing (cheapest tier that passed) cut model cost ~63% vs all-opus on
this set — illustrative, since it depends on task mix; the per-task ratio above
is the robust, mix-independent claim. Full method, honest caveats, and the
pass/fail grid: **[the benchmark page](https://deepaksinghcs14.github.io/deadeye-cc/benchmark.html)**
· reproduce it in [`benchmarks/routing/`](benchmarks/routing/).

## Benchmark — review quality (held-out)

Does the PR reviewer actually catch real bugs? We measured it on **24 real-world
pull requests across 17 projects**, each one shipping a bug the project *later
fixed* in a follow-up commit. deadeye reviewed the introducing PR **cold** — diff
and source only, no fix, no comments — and the project's own fix is the answer
key. A finding counts as a catch only when it matches what the fix changed;
anything ambiguous is scored a miss.

**Recall 54% (13/24) at 97% precision** — one false positive across 31 findings,
every finding re-verified in source by an adversarial pass. It caught **every
authorization regression in the set** (4/4) — the cross-file permission bugs a
diff-only reviewer misses — and was weakest on concurrency races (1/4), the
honest soft spot. Beyond the 24 seeded bugs it surfaced 17 more real issues on
the same PRs.

Held-out, graded against public git history, precision adversarially verified.
Full breakdown by bug class: **[the benchmark page](https://deepaksinghcs14.github.io/deadeye-cc/benchmark.html#review)**.

## Install

```
/plugin marketplace add deepaksinghcs14/deadeye-cc
```
```
/plugin install deadeye@deadeye
```

That's everything on macOS/Linux — hooks, slash commands, and the binary
bootstraps itself on first use (downloaded once from Releases,
sha256-verified). On Windows, install the binary from
[Releases](https://github.com/deepaksinghcs14/deadeye-cc/releases) first
(self-bootstrap isn't built there yet).

deadeye also runs on four other hosts, each to the depth its extension API
allows:

### Codex CLI (experimental)

Install the binary, then `deadeye init codex` — it shows the exact
`~/.codex/hooks.json` changes and writes them only after you confirm
(deadeye never edits another tool's config silently). You get output
trimming, the coder persona, the plan gate, `/deadeye-mute`, and the full
decision log; not model routing (Codex has no subagent surface to route).
Needs `[features] hooks = true` in `~/.codex/config.toml`. Remove with
`deadeye uninstall codex`.

### Gemini CLI (experimental — context-injection tier)

`deadeye init gemini` writes a self-contained extension under
`~/.deadeye/gemini-extension/` and prints the `gemini extensions install`
command — deadeye never edits Gemini's own config. You get the coder
persona plus session guidance (codemap, vulnerable-dependency flag, soft
plan gate). The tool-level features (exfil guard, output trimming, routing)
wait on live schema verification rather than risk firing blind. Remove with
`deadeye uninstall gemini`.

### Cursor and Windsurf (persona only)

No hook contract, so these get the **coder persona only**, as a rules file:

```bash
deadeye init cursor      # writes .cursor/rules/deadeye.md
deadeye init windsurf    # writes .windsurf/rules/deadeye.md
```

Level-filtered to your `coder.default_level`, and uninstall removes only
what deadeye wrote. For the full engine, use Claude Code or Codex.

### PR review on every host (experimental)

`deadeye init <host>` also installs the `/deadeye-pr` review command in
that host's native format — a Codex prompt, a Gemini TOML command, a Cursor
skill, a Windsurf workflow — carrying the same four-lens rubric Claude Code
ships as the `deadeye-pr` skill. Experimental until live-verified.

### From source

```bash
go install github.com/deepaksinghcs14/deadeye-cc/cmd/deadeye@latest
```

### Uninstall

```bash
deadeye uninstall --purge   # removes the binary, the daemon socket, and ~/.deadeye
```

Then `/plugin uninstall deadeye@deadeye` in Claude Code.

## What it controls

Eight independent controls — each has its own on/off, and switching one off
never touches the others.

| What | Modes | What it does |
|---|---|---|
| Context hygiene | `off` / `on` | Trims verbose command output (test suites across nine ecosystems, builds, linters, installs, log tails) before it enters context, and flags wasteful patterns — re-reading unchanged files, re-running the same command, an oversized MCP response, a good moment to `/compact`. |
| Coder persona | `off` / `spotter` / `marksman` / `sniper` | A lean-first coding discipline — including a live security check on what's written and its dependencies — injected each session and into subagents. |
| Security | `off` / `advise` / `ask` | An exfiltration guard: a Read of a credential file, or a Bash command shipping one out, escalates to a permission prompt a prompt-injected model can't answer. Independent of the persona. |
| Codebase map | `off` / `on` | A persistent per-project map — skeleton, most-touched files, exploration notes — injected once per session so a fresh session doesn't re-explore from scratch. |
| Effort level | `off` / `advise` | Suggests lower effort for mechanical steps; no effect if `CLAUDE_EFFORT` is pinned. |
| Model choice | `off` / `advise` / `enforce` | Picks the model for a subagent — only when you didn't already choose one. |
| AI routing judge | `off` / `on` | Opt-in: when the cheap signals can't place a subtask, classify it with a `claude -p` call (haiku, no API key) instead of defaulting a tier. |
| Plan-first gate | `off` / `soft` / `hard` | Suggests (or requires) a short plan before a risky multi-file edit. |
| Workflow suggestion | `off` / `on` | Flags tasks that look like parallel/fan-out work — only ever suggests, never starts one. |
| Update check | `off` / `on` | Once/day background check for a newer release; asks (once per version) whether to update. |

Settings live in `~/.deadeye/config.json` (with an optional per-repo
`.deadeye.json` override); full schema in
[`schema/config.schema.json`](schema/config.schema.json). Four env vars are
kill switches: `DEADEYE=off` (everything), `DEADEYE_PREPROCESS=off`,
`DEADEYE_GATE=off`, `DEADEYE_CODER=off`.

Change any setting without editing JSON: **`/deadeye-config`** from chat (or
just say what you want — "turn off the plan gate"), **`deadeye config`** for an
interactive picker in your terminal, or **`deadeye config set <key> <value>`**.
`/deadeye-status` shows every setting with its key and allowed values; new
installs get a one-time welcome pointing at all of this.

## Commands

| Command | What it does |
|---|---|
| `/deadeye-status` | Modes, coder level, kill switches, model list, daemon health |
| `/deadeye-route [task]` | Shows what deadeye *would* decide for a task, and why — without doing anything |
| `/deadeye-config` | View or change any setting from chat, or interactively with `deadeye config` |
| `/deadeye-stats [savings\|context]` | Decision-log reports: measured-impact scoreboard (default), token-savings, per-session context bytes |
| `/deadeye-coder [level]` | Switch or report the coder persona level |
| `/deadeye-mute [off]` | Mute advisories and nags for this session (rewrites keep working) |
| `/deadeye-review [--repo]` | Over-engineering review of the working diff, or the whole repo with `--repo` |
| `/deadeye-guard` | Security review of the current diff — injection, secrets, authz, crypto, deps, DoS |
| `/deadeye-pr [<PR>] [--post]` | PR review across four lenses; prints locally, opt-in to post to the PR. Huge PRs fan out to parallel subagents, each at the cheapest model tier its cluster needs. |
| `/deadeye-debt` | Ledger of every `deadeye:` shortcut marker in the repo |
| `/deadeye-help` | Quick-reference card for all of the above |
| `deadeye update` | Update the managed binary (sha256-verified, atomic) — for Codex-only installs |
| `deadeye config` | Interactive settings picker in your terminal (or `config get/set <key>`) |
| `deadeye uninstall --purge` | Remove the binary, its background process, and all local state |

## Coder mode

deadeye's coding persona is the lazy-senior-dev discipline: it pushes every
change toward the leanest solution that actually works — question whether
the code needs to exist, stdlib before custom code, native before
dependencies, one line before fifty. Injected every session (it survives
compaction) and into subagents.

| Level | Discipline |
|---|---|
| `spotter` | Builds what's asked, names the leaner alternative in one line — you pick. |
| `marksman` | The lean-first ladder enforced: YAGNI, stdlib and native first, shortest working diff. **(default)** |
| `sniper` | One shot only — ships the one-liner and challenges the rest of the requirement. |

Deliberate shortcuts get a `deadeye:` comment naming the ceiling and upgrade
trigger, which `/deadeye-debt` collects into a ledger. Safety is never cut:
input validation, error handling, security, and accessibility hold at every
level. [See it per-level on the site →](https://deepaksinghcs14.github.io/deadeye-cc/#layers)

## Development

```bash
make check   # vet, gofmt, tests -- everything CI runs
make build   # ./bin/deadeye
```

`scripts/gen-catalog.go` regenerates the compiled-in model/pricing table
after editing its seed prices — a release-time step, since there's no
reachable pricing API at runtime.

## FAQ

**Does it phone home?**
No. Everything it remembers lives in one file at `~/.deadeye/`. No hosted
service, no API keys, no telemetry.

**Why not just ask an LLM which model to use?**
Four cheap, predictable signals — files touched, recent git activity,
whether tests exist nearby, how the request reads — are enough to make a
reasonable call, and they're free to check. Asking an LLM would spend tokens
to figure out how to save tokens, and add the network dependency this tool
is built to avoid.

**Will it make Claude dumber?**
That's the exact failure mode it's built to avoid. When it isn't confident,
it defaults to the more capable option — cheaper always needs evidence
first. If you find a case where it under-powered a task, that's a bug;
report it with the `/deadeye-route` output.

**Why "deadeye"?**
Because efficiency isn't spending less — it's not missing.

## Contributing

Contributions welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for the ground
rules (the short version: fail open, go big when unsure, bring a regression
test you've watched fail). Security reports go through
[SECURITY.md](SECURITY.md) — never a public issue.

## License

[MIT](LICENSE) — the shortest license that hits. Third-party notices in
[THIRD-PARTY.md](THIRD-PARTY.md).
