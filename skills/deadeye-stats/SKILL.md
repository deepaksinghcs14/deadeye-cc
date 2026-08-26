---
name: deadeye-stats
description: deadeye's decision-log reports in one place -- measured impact, token savings, and per-session context breakdown.
license: MIT
argument-hint: "[savings|context|impact] [session-id]"
---

# Deadeye Stats

One entry point for the three reports deadeye computes from its decision
log (`~/.deadeye/decisions.jsonl`). Pick a view by the first argument;
with none, show the measured-impact scoreboard.

- no arg, or `impact` → the measured scoreboard (`deadeye gain`)
- `savings` → the full token-savings report (`deadeye audit`)
- `context [session-id]` → per-session context-byte breakdown (`deadeye context [session-id]`)

Run the matching binary and present its output **as-is** — every figure
comes from the decision log, not an invented aggregate. If the binary
reports "command not found", it's almost certainly just not on PATH
(deadeye never adds itself to PATH; it resolves its own binary internally
for hooks). Retry the self-bootstrap path directly, e.g.
`~/.deadeye/bin/deadeye gain`. Only if that also fails is it genuinely not
bootstrapped yet (it self-installs on the first hook invocation).

If the log is empty, the binary prints its own explanation — relay it as-is
and suggest running a task with the plugin active first.

## Honesty boundaries (load-bearing — do not soften)

These carry over from the three reports this skill replaces. Keep them
exactly when presenting any view:

**impact (`deadeye gain`)**
- NEVER print a per-repo savings percentage for code that was never
  written — the unbuilt version has no baseline to subtract from.
- Estimates and measurements are labeled differently in the output; keep
  that distinction.
- For per-repo reality, point at `/deadeye-debt` (shortcuts actually taken)
  and `/deadeye-review --repo` (what's still cuttable).

**savings (`deadeye audit`)**
- Decisions per surface and per action are counts from the log.
- Preprocessing rewrite figures are per-rule **estimates** (a typical-case
  constant — PreToolUse runs before the command does, so the real output
  size isn't known yet). Present them as estimates, not measurements.
- Suggest cross-checking against `/usage`'s plugin attribution — that's
  ground truth for actual token spend.

**context (`deadeye context`)**
- "Injected by deadeye" figures are real byte measurements taken at
  injection time.
- "Observed arrivals" is a FLOOR, not a session total — only outliers are
  logged (built-in tool responses over 8KB, every MCP response), so the
  true arrival total is higher.
- "Kept out of context" keeps the estimated/measured split — never blend a
  per-rule estimate with a measured filtered size.
- No id shows the newest session; pass a session id for an older one (an
  unknown id lists the newest five to pick from).

One-shot: do NOT change the coder level, write any files, or persist
anything.
