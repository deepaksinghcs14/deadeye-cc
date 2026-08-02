<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.svg">
    <img src="assets/logo.svg" width="140" alt="deadeye">
  </picture>
</p>

<h1 align="center">deadeye</h1>
<p align="center"><em>He doesn't waste shots.</em></p>

<p align="center">
  <a href="https://github.com/deepaksinghcs14/deadeye-cc/actions/workflows/release.yml"><img src="https://img.shields.io/github/actions/workflow/status/deepaksinghcs14/deadeye-cc/release.yml?label=release" alt="release status"></a>
  <img src="https://img.shields.io/badge/license-MIT-informational" alt="MIT license">
  <img src="https://img.shields.io/badge/version-0.1.0--dev-lightgrey" alt="version">
</p>

A Claude Code plugin that fits the model, effort, and context to each task —
fewer tokens, same quality. A deterministic policy kernel behind Claude Code
hooks: every decision it makes is logged, every number it reports is
measured, not estimated. **[Site →](https://deepaksinghcs14.github.io/deadeye-cc/)**

```
/plugin marketplace add deepaksinghcs14/deadeye-cc
/plugin install deadeye@deadeye
```

## The before/after

Not a hypothetical — this repo's own test suite, run raw and then through
deadeye's `test-filter` rewrite:

| | Bytes |
|---|---|
| Raw `go test ./... -v` | 10,001 |
| Filtered | 55 |

The 55 bytes are `deadeye: command exited 0, no output survived
filtering` — the real exit code, plus a one-line confirmation so a passing
run never gets mistaken for a hang. (A naive `pipefail \| grep \| head`
rewrite — the obvious first attempt — reports a passing suite as *failed*
whenever the filter matches nothing. Verified against both directions
before this shipped; see `docs/verified.md`.)

## How it decides

```
signals → evidence → kernel.Decide → decision → decisions.jsonl
```

Four built-in providers assess a task (prompt shape, file scope, recent git
churn, adjacent test coverage), each contributing a complexity estimate and
a confidence in that estimate. The kernel picks the cheapest `(model,
effort)` cell that clears the evidence-backed floor. Missing or unreliable
evidence never buys a cheaper answer — it defaults to the ceiling. Run
`/deadeye-route` any time to see the live reasoning for the current task.

## The five axes

| Axis | Modes | What it does |
|---|---|---|
| Preprocessing | `off` / `on` | Rewrites verbose Bash output before it enters context |
| Effort | `off` / `advise` | Requests lower effort for mechanical steps |
| Model routing | `off` / `advise` / `enforce` | Recommends or rewrites the model for subagent delegations |
| Plan gate | `off` / `soft` / `hard` | Suggests (or requires) a short plan before multi-file edits |
| Workflow advisor | `off` / `on` | Flags genuinely fan-out tasks — never triggers one itself |

## Commands

| Command | What it shows |
|---|---|
| `/deadeye-status` | Modes, kill switches, model catalog, daemon health |
| `/deadeye-route [task]` | Dry-run of the kernel's decision, with full reasoning |
| `/deadeye-audit` | Savings report straight from the decision log |

## Configuration

`~/.deadeye/config.json`, overlaid by project-level `.deadeye.json`. Full
reference: [`schema/config.schema.json`](schema/config.schema.json).

Kill switches: `DEADEYE=off` disables everything; `DEADEYE_PREPROCESS=off`
and `DEADEYE_GATE=off` disable just those axes.

## Design

Six hard invariants govern everything above (`docs/PLAN.md` §2), enforced
as unit tests, not just documentation:

- **Unknown routes up.** Missing evidence never produces a cheaper model or
  lower effort. Downshifting requires confidence above a threshold;
  upshifting is always free.
- **Never resolve toward the unbounded side.** The workflow advisor only
  ever recommends — it can never trigger a workflow itself.
- **Fail open.** A panic or error anywhere in a hook still yields a no-op
  response. A broken policy layer must never block real work.
- **Every decision is visible and reversible.** Every decision is logged
  with its reason; every axis has a mode, a per-invocation bypass, and a
  kill switch.
- **Consent is sticky.** Decline a gate once, it doesn't ask again for that
  task.
- **Hooks stay fast.** p95 well under 50ms — measured, not assumed
  (`docs/verified.md`).

No telemetry, no network calls at runtime. One JSONL decision log, local,
at `~/.deadeye/`. `deadeye uninstall --purge` removes all of it.

## Why not just tell Claude to be careful with tokens?

Prose guidance degrades — it competes with everything else in context and
gets deprioritized under pressure. deadeye is a policy kernel wrapped
around the model, not a suggestion inside it: the decisions are
deterministic, tested without an LLM in the loop, and every number
`/deadeye-audit` reports traces back to a specific logged decision, never an
assumed average.

## License

MIT
