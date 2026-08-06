---
description: Per-session ranked breakdown of context bytes by source
argument-hint: "[session-id]"
allowed-tools: Bash(deadeye context:*), Bash(~/.deadeye/bin/deadeye context:*)
---

Run `deadeye context $ARGUMENTS`. If that reports "command not found",
it's very likely just not on PATH -- deadeye never adds itself to PATH,
it only resolves its own binary internally for hook invocations. Before
concluding it's missing, retry with the self-bootstrap install path
directly: `~/.deadeye/bin/deadeye context $ARGUMENTS`. Only if that also
fails is it genuinely not bootstrapped yet (self-installs on first hook
invocation -- running any tool once and retrying should resolve it).

Present the ranked breakdown as-is. Every figure comes from the decision
log -- `~/.deadeye/decisions.jsonl` -- not an invented aggregate. Three
honesty boundaries to preserve when discussing the numbers:

- "Injected by deadeye" figures are real byte measurements taken at
  injection time.
- "Observed arrivals" is a FLOOR, not a session total: only outliers are
  ever logged (built-in tool responses over 8KB, every MCP response), so
  the true arrival total is higher.
- "Kept out of context" keeps the estimated/measured split -- estimated
  rewrite figures are per-rule typical-case constants, measured figures
  are real filtered output sizes. Never blend them.

With no argument it shows the newest session; pass a session id for an
older one (an unknown id lists the newest five to pick from).

If no decisions are logged yet, say so plainly and suggest running a task
with the plugin active first.
