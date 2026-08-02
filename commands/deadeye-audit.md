---
description: Show deadeye's savings report from the decision log
allowed-tools: Bash(deadeye audit)
---

Run `deadeye audit` and present its output: decisions per surface, decisions
per action, and preprocessing rewrite estimates (before/after bytes per
rule, and the total). Every figure comes from the decision log --
`~/.deadeye/decisions.jsonl` -- not an invented aggregate. Preprocessing
byte figures are per-rule *estimates* (a typical-case constant, since
PreToolUse runs before the command executes and can't know the real output
size yet) -- present them as estimates, not measurements.

Suggest the user cross-check against `/usage`'s plugin attribution, since
that's ground truth for actual token spend.

If no decisions are logged yet, say so plainly and suggest running a task
with the plugin active first.
