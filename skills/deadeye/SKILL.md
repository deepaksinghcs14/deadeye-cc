---
description: "deadeye's home command: `/deadeye` (status), `/deadeye config` (view or change any setting), `/deadeye theme [name]`, `/deadeye stats [view]` (every report deadeye computes about itself), `/deadeye help` (the reference card). With the Claude Code mod on these open panes; otherwise they work from chat."
argument-hint: "[config|theme [name]|stats [impact|savings|context|accuracy|disagreement|adherence] [session-id]|help]"
allowed-tools: Bash(deadeye status), Bash(~/.deadeye/bin/deadeye status), Bash(deadeye config *), Bash(~/.deadeye/bin/deadeye config *), Bash(deadeye gain), Bash(~/.deadeye/bin/deadeye gain), Bash(deadeye audit), Bash(~/.deadeye/bin/deadeye audit), Bash(deadeye context *), Bash(~/.deadeye/bin/deadeye context *), Bash(deadeye misses), Bash(~/.deadeye/bin/deadeye misses), Bash(deadeye disagreement), Bash(~/.deadeye/bin/deadeye disagreement), Bash(deadeye adherence), Bash(~/.deadeye/bin/deadeye adherence)
---

# /deadeye

One command, five jobs. The first argument picks the job; with the Claude
Code mod on (`mode.mod on`) the mod answers the same command with panes
instead of this text, so nothing here runs. If `deadeye` reports "command
not found" it's just not on PATH (deadeye never adds itself to PATH; it
resolves its own binary internally for hooks): retry with
`~/.deadeye/bin/deadeye`. Only if that also fails is it not bootstrapped
yet (it self-installs on the first hook invocation).

## `/deadeye` (no argument)

Run `deadeye status` and present it under these headings: **Modes**
(per-axis), **Coder mode** (persona, live level), **Kill switches** (flag
any OFF), **Catalog** (tier table, source, build date -- flag if stale),
**Daemon** (up/down). Surface a `CLAUDE_CODE_EFFORT_LEVEL` note prominently
if present.

## `/deadeye config [what to change]`

You are the settings picker. Run `deadeye config list` for every key, its
current value and allowed values; show it as a short list. For what a key
DOES, link https://deepaksinghcs14.github.io/deadeye-cc/settings.html --
don't retype those explanations.

- If the user already said what they want ("plan gate off", "coder to
  sniper", "theme nord"), map it to the key and value and apply it.
- Otherwise ask which setting, then which value.

Apply with exactly:

    deadeye config set <key> <value>

It validates against the schema and writes `~/.deadeye/config.json`,
preserving every other setting. Show the confirmation line it prints.

Keys and allowed values (authoritative -- never invent others):

| Key | Values |
|---|---|
| `mode.routing` | off · advise · enforce |
| `mode.effort` | off · advise · enforce |
| `mode.preprocess` | off · on |
| `mode.plan_gate` | off · soft · hard |
| `mode.workflow_hint` | off · on |
| `mode.codemap` | off · on |
| `mode.update_check` | off · on |
| `mode.routing_judge` | off · on |
| `mode.catalog_check` | off · on |
| `mode.tier_sample` | off · on |
| `mode.mod` | off · on |
| `mod.theme` | auto · marksman · nightvision · highnoon · blueprint · ghost · tokyonight · catppuccin · rosepine · gruvbox · dracula · nord · kanagawa · everforest · synthwave · solarized · dawn |
| `mod.repaint` | off · on |
| `mod.background` | off · on |
| `mod.hud` | auto · full · deadeye · off |
| `mod.tips` | off · on |
| `mod.sounds` | off · on |
| `mod.notify` | off · on |
| `coder.default_level` | off · spotter · marksman · sniper |
| `coder.security` | off · advise · ask |
| `coder.security_osv` | true · false |
| `security.exfil` | off · advise · ask |

Also settable (free-form): `coder.subagent_matcher` (regex),
`downshift_threshold` (0-1), `injection_budget_tokens` /
`coder.injection_budget_tokens` (int), `plan_gate.min_files` (int),
`tier_sample.rate` (int).

## `/deadeye theme [name]`

With a name: `deadeye config set mod.theme <name>` (one of the `mod.theme`
values above, or `auto`). Without one: list the themes with a one-line
feel for each, point at https://deepaksinghcs14.github.io/deadeye-cc/themes.html
to see them drawn, and ask which. The theme shows only with the mod on.

## `/deadeye stats [view] [session-id]`

One entry point for every report deadeye computes about itself. Pick a
view by the argument; with none, show the measured-impact scoreboard.

Economics, from the decision log (`~/.deadeye/decisions.jsonl`):

- no view, or `impact` → the measured scoreboard (`deadeye gain`)
- `savings` → the full token-savings report (`deadeye audit`)
- `context [session-id]` → per-session context-byte breakdown (`deadeye context [session-id]`)

Judgment, from review/coder receipts (`~/.deadeye/receipts.jsonl`) and the
outcomes store -- deadeye checked against what actually happened:

- `accuracy` → candidate review misses and recorded disputes (`deadeye misses`)
- `disagreement` → where the judge would have routed cheaper (`deadeye disagreement`)
- `adherence` → the coder ladder's checkable rungs on shipped diffs (`deadeye adherence`)

The three judgment views measure GOING FORWARD only: they read receipts
written when a review or coder session runs, and nothing before this
feature existed left one. An empty report means "not measured yet", never
"nothing wrong" -- say which.

`deadeye gain`'s last line is a `file://` link to a regenerated visual
report (`deadeye report` under the hood) -- relay it as-is, the same as
every other figure.

Run the matching binary and present its output **as-is** -- every figure
comes from the decision log, the receipts log, or the repo's own git
history, not an invented aggregate. If the log is empty, the binary prints
its own explanation -- relay it as-is and suggest running a task with the
plugin active first.

### Honesty boundaries (load-bearing -- do not soften)

**impact (`deadeye gain`)**
- NEVER print a per-repo savings percentage for code that was never
  written -- the unbuilt version has no baseline to subtract from.
- Estimates and measurements are labeled differently in the output; keep
  that distinction.
- For per-repo reality, point at `/deadeye-debt` (shortcuts actually taken)
  and `/deadeye-review --repo` (what's still cuttable, or risky).

**savings (`deadeye audit`)**
- Decisions per surface and per action are counts from the log.
- Preprocessing rewrite figures are per-rule **estimates** (a typical-case
  constant -- PreToolUse runs before the command does, so the real output
  size isn't known yet). Present them as estimates, not measurements.
- The trailing "Real Claude Code usage" block is **measured**, not
  estimated -- read straight from this project's own Claude Code session
  transcripts (the same numbers `/usage` renders), not from deadeye's
  decision log. Keep its scope caveat when relaying it: it's scoped to the
  CURRENT PROJECT, while every other figure in this report is global
  across every project deadeye has ever run in on this machine -- a real
  number on a different scope, not a direct reconciliation. If it's
  missing (best-effort: Claude Code's transcript format/layout is
  undocumented), fall back to suggesting a manual `/usage` check.

**accuracy (`deadeye misses`)**
- Every candidate is `likely`, NEVER confirmed. A fix-shaped commit
  touching lines a review passed is evidence, not proof -- the fix may be
  new work, a refactor, or a bug the reviewed diff never contained.
- Nothing is recorded automatically. If a candidate is genuinely a miss,
  the user records it (`deadeye lessons record external-miss <lens>:<tag> "<what the fix showed>"`);
  never present the report's count as a confirmed miss count.
- Reviewed work that hasn't been committed yet is excluded from the
  denominator, not counted as a clean pass. Keep that distinction when
  relaying the numbers.
- Both error directions belong together: candidate misses (false
  negatives) AND recorded disputes (false positives). Never relay one
  alone as "accuracy".

**disagreement (`deadeye disagreement`)**
- This is the judge's OPINION, not a measured over-route rate. Never call
  it one. A cheaper tier might still have failed the task.
- The reason it's an opinion: an arbitrary production subtask has no
  grader, so the work can't be replayed and scored. Graded comparison
  lives in `benchmarks/routing/`, on fixtures that ship hidden tests.
- Sampling is OFF by default and costs a real `claude -p` call per sample.
  If it's off, the report says so -- relay that rather than reporting zero
  disagreements as a clean result.
- It changes no routing behavior: escalation bias stays one-directional
  until the number has been trusted for a while.

**adherence (`deadeye adherence`)**
- Measures only the mechanically checkable ladder rungs on diffs that
  SHIPPED: rung 5 (a new dependency) and rung 7 (files/insertions per
  commit). These are signals, not verdicts -- a wide commit can be the
  right shape, and a new dependency can be exactly what rung 5 asks for.
- Never present it as full ladder coverage. Rung 2 (already in this
  codebase) is unmeasured -- it needs a function-level symbol index this
  plugin doesn't build. Rungs 1, 3, 4 and 6 are judgment calls.
- Never turn it into a lines-not-written saving. Same boundary as
  `impact`: the unbuilt version has no baseline to subtract from.
- Attribution is by time window (12h after a recorded coder session), not
  proof -- git records no persona, so a commit written with coder mode off
  inside that window still counts. Say so if the figures are load-bearing.

**context (`deadeye context`)**
- "Injected by deadeye" figures are real byte measurements taken at
  injection time.
- "Observed arrivals" is a FLOOR, not a session total -- only outliers are
  logged (built-in tool responses over 8KB, every MCP response), so the
  true arrival total is higher.
- "Kept out of context" keeps the estimated/measured split -- never blend a
  per-rule estimate with a measured filtered size.
- No id shows the newest session; pass a session id for an older one (an
  unknown id lists the newest five to pick from).

One-shot: do NOT change the coder level or persist anything else. The one
exception is `report.html`, regenerated as a side effect of `deadeye gain`
-- a disposable cache artifact rebuilt from the same decision log every
time, not persisted session state.

## `/deadeye help`

Present this reference card, formatted cleanly:

### Coder mode

| Level | What it does |
|---|---|
| `spotter` | Builds what's asked, names the leaner alternative in one line |
| `marksman` | The lean-first ladder enforced. Default. |
| `sniper` | Maximum minimalism -- one-liners, challenges the requirement itself |

- Switch: `/deadeye-coder spotter|marksman|sniper|off`
- Report current: `/deadeye-coder`
- Persist a default for new sessions: `/deadeye-coder default <level>`
  (writes deadeye's own `~/.deadeye/config.json`, never Claude's settings)
- Turn off mid-session: say exactly `normal mode` or `stop coder`

### Commands

| Command | What it does |
|---|---|
| `/deadeye` | Scope pane with the mod on (toggle); `deadeye status` from chat otherwise |
| `/deadeye config` | View or change any setting: a settings pane with the mod on, from chat otherwise (or just say what to change) |
| `/deadeye theme [name]` | Theme picker with the mod on, or set one by name; `auto` follows the coder level |
| `/deadeye stats [impact\|savings\|context\|accuracy\|disagreement\|adherence]` | Every report deadeye computes about itself: measured impact (default), token savings, per-session context bytes, plus reviewer accuracy, routing disagreement, and coder-ladder adherence |
| `/deadeye help` | This card |
| `/deadeye-status` | Modes, coder level, kill switches, catalog, daemon health |
| `/deadeye-route [task]` | Dry-run the routing decision with full reasoning |
| `/deadeye-coder [level]` | Switch/report the coder persona level |
| `/deadeye-mute [off]` | Session-scoped mute for advisories/nags (rewrites stay on) |
| `/deadeye-review [--repo]` | Four-lens self-review of the working diff, or the whole repo with `--repo` -- the pre-PR version of `/deadeye-pr` |
| `/deadeye-guard` | Security review of the current diff -- full OWASP-mapped coverage (Top 10:2025, API Security Top 10 2023, LLM Top 10:2025), deps |
| `/deadeye-vapt` | Whole-service pen-test/VAPT pass -- full OWASP Top 10:2025/API Security Top 10 2023/LLM Top 10:2025 coverage |
| `/deadeye-pr [<PR>] [--post]` | PR review across four lenses -- over-engineering, correctness, performance, security |
| `/deadeye-sweep [--repo\|--pr [<PR>]] [<N>] [--commit] [--all]` | Applies review/guard findings (crit/high, or every severity with `--all`), verifies the build, loops until clean (cap 5). `--pr` mode also fixes/replies/resolves the PR's own open review comments |
| `/deadeye-debt` | Ledger of `deadeye:` shortcut markers |

CLI-only: `deadeye lessons [reset]` inspects/clears the recorded routing
outcomes that bias future decisions. `deadeye update` updates the managed
binary directly (sha256-verified, atomic) -- for Codex-only installs.

### Kill switches (env)

- `DEADEYE=off` -- everything
- `DEADEYE_PREPROCESS=off` -- context hygiene (output trimming + read/repeat advisories)
- `DEADEYE_GATE=off` -- plan gate
- `DEADEYE_CODER=off` -- coder persona

### Config

`~/.deadeye/config.json` (seeded with defaults on first run; project
overrides in `.deadeye.json`). Change any key with `/deadeye config`; the
full explanation of every knob is on the settings page.

### Update

`/plugin marketplace update deadeye` then `/reload-plugins` -- or enable
auto-update under `/plugin` → Marketplaces. Codex-only install: run
`deadeye update` instead.

One-shot: do NOT change any mode or write anything.

## Boundaries

- Only ever run `deadeye status`, `deadeye config get|set|list`, and the
  stats binaries named above. Never hand-edit the JSON, never set a key or
  value not listed above.
- Kill switches (`DEADEYE=off`, `DEADEYE_PREPROCESS/GATE/CODER=off`) are
  env vars, not config keys; for "everything off", say to set `DEADEYE=off`.
- A new coder default takes effect next session; the live level for THIS
  session changes with `/deadeye-coder <level>`. `mod.*` changes apply on
  the next turn.
