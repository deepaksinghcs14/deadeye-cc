---
description: Show deadeye's savings report from the decision log
allowed-tools: Bash(deadeye audit), Bash(~/.deadeye/bin/deadeye audit)
---

Run `deadeye audit`. If that reports "command not found", it's very likely
just not on PATH -- deadeye never adds itself to PATH, it only resolves its
own binary internally for hook invocations. Before concluding it's missing,
retry with the self-bootstrap install path directly:
`~/.deadeye/bin/deadeye audit`. Only if that also fails is it genuinely not
bootstrapped yet (self-installs on first hook invocation -- running any tool
once and retrying should resolve it).

Present its output: decisions per surface, decisions
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
