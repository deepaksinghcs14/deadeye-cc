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
</p>

He doesn't check twice or spray and pray — he doesn't need to. deadeye
watches every subagent spawn, every verbose test dump, every plan-worthy
edit, and picks exactly the model, effort, and context the task actually
needs. Your agent spends tokens by default; deadeye is the discipline that
stops it before they're gone. **[Site →](https://deepaksinghcs14.github.io/deadeye-cc/)**

## Before / after

You ask your agent to run the test suite before merging.

Without deadeye:

> All 202 lines of verbose output enter context — every `PASS`, every
> timing line — whether or not anything failed.

With deadeye:

> `deadeye: command exited 0, no output survived filtering`

*(10,013 raw bytes, filtered before they ever entered context. When a test
actually fails, the FAIL lines survive intact — verified both directions,
including the failure mode a naive `pipefail | grep | head` rewrite has:
it reports a passing suite as failed whenever the filter matches nothing.)*

## How it works

```
1. Observe   task shape from cheap, deterministic signals -- files in scope, git churn, adjacent tests, prompt shape
2. Decide    grid search over (model, effort); the cheapest cell that clears the evidence-backed floor wins
3. Enforce   rewrites the subagent's model, filters verbose Bash output, gates multi-file edits behind a plan
4. Learn     an explicit escalation (you picked bigger than it recommended) raises the bar for that task shape next time
```

The rule under all four: **unknown routes up.** Missing or unreliable
evidence never buys a cheaper model or lower effort — the kernel defaults to
the ceiling, and downshifting requires confidence above a threshold.
Upshifting is always free. Every decision is printable — run
`/deadeye-route` any time to see the live reasoning, not a black box. Full
schema: [`schema/config.schema.json`](schema/config.schema.json).

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
| `/deadeye-status` | Modes, kill switches, model catalog, daemon health |
| `/deadeye-route [task]` | Dry-run the kernel's decision, with full reasoning |
| `/deadeye-audit` | Savings report straight from the decision log |
| `deadeye uninstall --purge` | Remove the binary, socket, and all local state |

## The five axes

| Axis | Modes | What it does |
|---|---|---|
| Output | `off` / `on` | Rewrites verbose Bash commands before they run — test suites, builds, linters, log tails |
| Effort | `off` / `advise` | Requests lower effort for mechanical steps; inert if `CLAUDE_EFFORT` pins the session |
| Model | `off` / `advise` / `enforce` | Rewrites the model on subagent spawn — only when you left it unset |
| Plan gate | `off` / `soft` / `hard` | Suggests (or asks for) a short plan before multi-file edits |
| Workflow | `off` / `on` | Flags genuinely fan-out tasks — recommends only, never triggers one itself |

Each axis runs independently and can be switched off without touching the
others. Config: `~/.deadeye/config.json`, overlaid by project-level
`.deadeye.json`. Kill switches: `DEADEYE=off` disables everything;
`DEADEYE_PREPROCESS=off` and `DEADEYE_GATE=off` disable just those axes.

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
No. One JSONL file on your machine, at `~/.deadeye/`. No hosted service, no
API keys, no telemetry.

**Why not route with an LLM call, or embeddings?**
Four cheap, deterministic signals — file count, git churn, adjacent tests,
prompt shape — are enough for a coarse routing decision, and they cost
nothing to compute. A classifier call would spend tokens deciding how to
save tokens, and add a network dependency this tool is built to avoid.

**Will it make Claude dumber?**
That's the failure mode it's built against. Unknown scope routes up, and
downshifting needs positive evidence above a confidence threshold. If you
find a case where it under-powered a task, that's a bug — file it with the
`/deadeye-route` output.

**What doesn't it know?**
Whether an edit actually broke something later (revert/test-fail detection
isn't built yet — only explicit escalations are), which repos depend on the
one you're editing (that's [greybeard](https://github.com/deepaksinghcs14/greybeard)'s
job), and which way you answered a plan-gate permission prompt — no hook
surface reports that back, so it asks once and stays quiet either way.

**Why "deadeye"?**
Because efficiency isn't spending less — it's not missing.

## License

[MIT](LICENSE)
