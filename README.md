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
  <img src="https://img.shields.io/github/v/release/deepaksinghcs14/deadeye-cc?style=flat-square&color=111111&label=release" alt="Release">
  <img src="https://img.shields.io/github/license/deepaksinghcs14/deadeye-cc?style=flat-square&color=111111" alt="MIT license">
  <img src="https://img.shields.io/badge/storage-1%20jsonl%20file-111111?style=flat-square" alt="One JSONL file">
  <img src="https://img.shields.io/badge/measured%20reduction-79.6%25%20to%2099.5%25-111111?style=flat-square" alt="79.6% to 99.5% measured reduction">
</p>

He doesn't check twice or spray and pray — he doesn't need to. deadeye is
a Claude Code plugin that watches what your agent is about to do — spawn a
subagent, dump a noisy test log, make a big multi-file edit — and picks
the cheapest model, effort level, and amount of context that will still
get the job done right. Left on its own, Claude Code tends to spend more
tokens than a task actually needs. deadeye catches that before the tokens
are gone. **[Site →](https://deepaksinghcs14.github.io/deadeye-cc/)**

## Before / after

You ask your agent to run the test suite before merging, and one test is
actually broken.

Without deadeye:

> All 14 lines of verbose output enter context — 4 `PASS` lines, then the
> failure, no differently weighted than if all 5 had passed.

With deadeye:

> ```
> --- FAIL: TestReconciliationAppliesTaxBeforeDiscount (0.00s)
> FAIL
> FAIL	canondemo/orders	0.539s
> FAIL
> ```

*(On that real run: 485 bytes shrank to 99 — a 79.6% reduction — and the
failure details stayed fully intact. On this repo's own full test suite,
which all passes: 10,301 bytes shrank to 55, a 99.5% reduction. On a real
`npm install express mocha`: 553 bytes of progress/funding/audit spam
shrank to 55, a 90.1% reduction, with errors and warnings still passed
through when they occur. Three separate real measurements, not one number
averaged across different situations — see
[the site](https://deepaksinghcs14.github.io/deadeye-cc/) for details, or
run `/deadeye-audit` to see your own numbers. One gotcha worth knowing: an
early, naive version of this filter could report a passing test suite as
"failed" whenever the filter pattern matched nothing. That's fixed now,
and tested against both directions.)*

deadeye also shows one quiet line at the end of a turn, telling you how
much it's kept out of context so far this session — `deadeye: ~9,600
bytes kept out of context this session (1 rewrite).` It only shows up
when that total has actually grown since the last time you saw it.

## Real task, end to end

Not a scripted demo — a real feature task run through the installed
plugin: add a `Mark()` method to a small package, write a test for it,
verify with `go test` and `go build`, and hand part of the work off to a
subagent. Here's the decision log for that one turn:

```
PreToolUse/Agent   advise         all evidence supports downshift: low complexity, confidence >= threshold
SubagentStart      noop
PreToolUse/Bash    rewrite        reason=test-filter
PreToolUse/Bash    rewrite        reason=build-filter
Stop               savings-shown  bytes_after=25800
```

deadeye recommended a cheaper model before the subagent even started, both
the test and build commands had their noisy output trimmed, and the turn
ended with `deadeye: ~25,800 bytes kept out of context this session (2
rewrites).` That 25,800 number is an estimate each rewrite rule carries
around (the same number `/deadeye-audit` prints, and it labels it as an
estimate right there in the output) — not a fresh measurement of this
specific task, whose real output happened to be pretty small. The
`485 → 99` and `10,301 → 55` numbers above are the actual measured ones;
this example is here to show the model-picking, output-trimming, and
end-of-turn summary all working together on one real task, not to add a
third headline number.

## How it works

```
1. Look      Checks a few cheap, deterministic signals: files touched, recent git activity, whether tests exist nearby, how the request reads
2. Decide    Picks the cheapest (model, effort) combination that should still clear the bar for this task
3. Apply     Rewrites the subagent's model, trims noisy command output, and asks before risky multi-file edits
4. Learn     If you manually pick a bigger model than it recommended, it remembers and gets more cautious for that kind of task next time
```

The rule behind all four steps: **when it doesn't know, it goes big.**
Missing or shaky evidence never buys a cheaper model or a lower effort
level — deadeye defaults to the most capable option. Picking something
cheaper requires real supporting evidence and a minimum confidence level;
picking something more capable never needs a reason. Every decision is
printable — run `/deadeye-route` any time to see the full reasoning, not
a black box. Full config schema:
[`schema/config.schema.json`](schema/config.schema.json).

## Install

```
/plugin marketplace add deepaksinghcs14/deadeye-cc
```
```
/plugin install deadeye@deadeye
```

That's everything on macOS/Linux — hooks, slash commands, and the binary
bootstraps itself on first use (downloaded once from Releases,
sha256-verified). Windows needs the binary from
[Releases](https://github.com/deepaksinghcs14/deadeye-cc/releases) first —
self-bootstrap there isn't built yet.

### From source

```bash
go install github.com/deepaksinghcs14/deadeye-cc/cmd/deadeye@latest
```

A binary already on PATH always beats the bootstrap, so your build stays in
charge.

### Uninstall

```bash
deadeye uninstall --purge   # removes the binary, the daemon socket, and ~/.deadeye
```

Then `/plugin uninstall deadeye@deadeye` in Claude Code.

## Commands

| Command | What it does |
|---|---|
| `/deadeye-status` | Shows current modes, kill switches, model list, and whether the background daemon is running |
| `/deadeye-route [task]` | Shows what deadeye *would* decide for a task, and why — without actually doing anything |
| `/deadeye-audit` | Prints a savings report straight from the decision log |
| `deadeye uninstall --purge` | Removes the binary, its background process, and all local state |

## The five things it controls

| What | Modes | What it does |
|---|---|---|
| Context hygiene | `off` / `on` | Trims verbose command output before it enters context — test suites (Go, JS, Python, Rust, Java, Gradle, .NET, Ruby, PHP), builds, linters, package installs, pod logs, log tails. Also flags wasteful reads: re-reading a file that hasn't changed, whole-reads of huge files, and running the identical command twice in a row |
| Effort level | `off` / `advise` | Suggests using lower effort for mechanical steps; has no effect if `CLAUDE_EFFORT` is already pinned for the session |
| Model choice | `off` / `advise` / `enforce` | Picks the model for a subagent — only when you didn't already choose one yourself |
| Plan-first gate | `off` / `soft` / `hard` | Suggests (or requires) a short plan before a risky multi-file edit |
| Workflow suggestion | `off` / `on` | Flags tasks that look like they'd benefit from running many things in parallel — only ever suggests it, never starts one on its own |

Each of these five works independently — you can turn any one off without
affecting the others. Settings live in `~/.deadeye/config.json`, with an
optional project-level `.deadeye.json` that overrides it for one repo.
Three env vars act as kill switches: `DEADEYE=off` turns everything off;
`DEADEYE_PREPROCESS=off` and `DEADEYE_GATE=off` turn off just the context
hygiene (output trimming plus the read/repeat advisories) or just the
plan gate, respectively.

## Development

```bash
make check   # vet, gofmt, tests -- everything CI runs
make build   # ./bin/deadeye
```

`scripts/gen-catalog.go` regenerates the compiled-in model/pricing table
after editing its seed prices — there's no reachable pricing API to fetch
from at runtime, so this is a release-time step, not a background refresh.

## FAQ

**Does it phone home?**
No. Everything it remembers lives in one file on your own machine, at
`~/.deadeye/`. No hosted service, no API keys, no telemetry.

**Why not just ask an LLM which model to use?**
Four cheap, predictable signals — how many files are touched, recent git
activity, whether tests exist nearby, and how the request is phrased — are
enough to make a reasonable call, and they're free to check (no extra API
call needed). Asking an LLM to decide would spend tokens just to figure out
how to save tokens, and would add a network dependency this tool is
specifically built to avoid.

**Will it make Claude dumber?**
That's exactly the failure mode it's built to avoid. When deadeye isn't
confident about a task, it defaults to the more capable option — picking
something cheaper always requires real evidence first. If you ever find a
case where it under-powered a task, that's a bug — report it along with
the `/deadeye-route` output.

**What doesn't it know how to do (yet)?**
It can't tell whether an edit actually broke something later — it only
knows when you manually pick a bigger model than it suggested, not whether
a test started failing afterward. It doesn't know which other repos depend
on the one you're editing — that's a different tool's job,
[greybeard](https://github.com/deepaksinghcs14/greybeard). And it can't
tell whether you approved or declined a plan-gate prompt, because Claude
Code doesn't report that back to plugins — so it just asks once per task
and doesn't ask again either way.

**Why "deadeye"?**
Because efficiency isn't spending less — it's not missing.

## License

[MIT](LICENSE)
