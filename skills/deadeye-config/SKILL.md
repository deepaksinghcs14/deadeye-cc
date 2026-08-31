---
name: deadeye-config
description: View and change deadeye's settings from chat -- the conversational settings picker.
license: MIT
argument-hint: "[<what to change>]"
---

# Deadeye Config

Change deadeye's settings without anyone editing JSON. You are the selector.

Run `deadeye config list` to see every tunable key, its current value, and its
allowed values. If that reports "command not found", it's just not on PATH --
retry `~/.deadeye/bin/deadeye config list`. Present the result to the user as a
short list.

- If the user already said what they want ("turn off the plan gate", "coder to
  sniper", "make reviews stricter"), map it to the right key/value and apply it
  directly -- don't make them pick from a menu.
- Otherwise, ask which setting to change, then which value.

Apply a change with exactly:

    deadeye config set <key> <value>

It validates against the schema and writes `~/.deadeye/config.json`, preserving
every other setting. Show the user the confirmation line it prints.

Keys and allowed values (authoritative -- never invent others):

| Key | Values |
|---|---|
| `mode.routing` | off · advise · enforce |
| `mode.effort` | off · advise |
| `mode.preprocess` | off · on |
| `mode.plan_gate` | off · soft · hard |
| `mode.workflow_hint` | off · on |
| `mode.codemap` | off · on |
| `mode.update_check` | off · on |
| `mode.routing_judge` | off · on |
| `coder.default_level` | off · spotter · marksman · sniper |
| `coder.security` | off · advise · ask |
| `coder.security_osv` | true · false |
| `security.exfil` | off · advise · ask |

Also settable (free-form): `coder.subagent_matcher` (regex),
`downshift_threshold` (0-1), `injection_budget_tokens` /
`coder.injection_budget_tokens` (int), `plan_gate.min_files` (int).

## Boundaries

- Only ever run `deadeye config get|set|list`. Never hand-edit the JSON, and
  never set a key or value not listed above.
- Kill switches (`DEADEYE=off`, `DEADEYE_PREPROCESS/GATE/CODER=off`) are ENV
  vars, not config keys. If the user wants everything off, tell them to set
  `DEADEYE=off` in their shell (or config a specific axis to `off`).
- Changes to `default_level` and other defaults take effect next session; the
  live coder level for THIS session changes with `/deadeye-coder <level>`.
- For the full list of commands, point the user at `/deadeye-help`.
