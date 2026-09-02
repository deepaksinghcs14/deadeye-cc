---
name: deadeye-help
description: Quick-reference card for every deadeye command and setting.
license: MIT
---

# Deadeye Help

Present this reference card, formatted cleanly:

## Coder mode

| Level | What it does |
|---|---|
| `spotter` | Builds what's asked, names the leaner alternative in one line |
| `marksman` | The lean-first ladder enforced. Default. |
| `sniper` | Maximum minimalism — one-liners, challenges the requirement itself |

- Switch: `/deadeye-coder spotter|marksman|sniper|off`
- Report current: `/deadeye-coder`
- Persist a default for new sessions: `/deadeye-coder default <level>`
  (writes deadeye's own `~/.deadeye/config.json`, never Claude's settings)
- Turn off mid-session: say exactly `normal mode` or `stop coder`

## Commands

| Command | What it does |
|---|---|
| `/deadeye-status` | Modes, coder level, kill switches, catalog, daemon health |
| `/deadeye-config` | View or change any setting from chat (or just say what to change) |
| `/deadeye-route [task]` | Dry-run the routing decision with full reasoning |
| `/deadeye-stats [savings\|context]` | Decision-log reports: measured impact (default), token savings, per-session context breakdown |
| `/deadeye-coder [level]` | Switch/report the coder persona level |
| `/deadeye-mute [off]` | Session-scoped mute for advisories/nags (rewrites stay on) |
| `/deadeye-review [--repo]` | Over-engineering review of the working diff, or the whole repo with `--repo` |
| `/deadeye-guard` | Security review of the current diff -- injection, secrets, authz, crypto, exposure, deps, DoS |
| `/deadeye-pr [<PR>] [--post]` | PR review across four lenses -- over-engineering, correctness, performance, security |
| `/deadeye-debt` | Ledger of `deadeye:` shortcut markers |
| `/deadeye-help` | This card |

CLI-only: `deadeye lessons [reset]` inspects/clears the recorded routing
outcomes that bias future decisions. `deadeye update` updates the managed
binary directly (sha256-verified, atomic) — for Codex-only installs.

## Kill switches (env)

- `DEADEYE=off` — everything
- `DEADEYE_PREPROCESS=off` — context hygiene (output trimming + read/repeat advisories)
- `DEADEYE_GATE=off` — plan gate
- `DEADEYE_CODER=off` — coder persona

## Config

`~/.deadeye/config.json` (seeded with defaults on first run; project
overrides in `.deadeye.json`). Coder knobs: `coder.default_level`,
`coder.subagent_matcher`, `coder.injection_budget_tokens`,
`coder.security` (`off`/`advise`/`ask`, the live Edit/Write advisory),
`coder.security_osv` (`false` keeps the dependency check fully offline).

## Update

`/plugin marketplace update deadeye` then `/reload-plugins` — or enable
auto-update under `/plugin` → Marketplaces. Codex-only install: run
`deadeye update` instead (see CLI-only above).

One-shot: do NOT change any mode or write anything.
