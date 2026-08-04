---
name: deadeye-gain
description: Measured-impact scoreboard from the decision log -- real numbers only.
license: MIT
---

# Deadeye Gain

Run `deadeye gain`. If that reports "command not found", it's very likely
just not on PATH -- deadeye never adds itself to PATH, it only resolves its
own binary internally for hook invocations. Before concluding it's missing,
retry with the self-bootstrap install path directly:
`~/.deadeye/bin/deadeye gain`. Only if that also fails is it genuinely not
bootstrapped yet (it self-installs on first hook invocation).

Present its output as-is: the scoreboard is
rendered from `~/.deadeye/decisions.jsonl` — real `measured` bytes from
actual command runs, per-rule estimates labeled as estimates, and MCP
observation totals.

## Honesty boundary (load-bearing — do not soften)

- NEVER print a per-repo savings percentage for code that was never
  written: the unbuilt version has no baseline to subtract from.
- Estimates and measurements are labeled differently in the output —
  keep that distinction when presenting.
- For per-repo reality, point at `/deadeye-debt` (the shortcuts actually
  taken) and `/deadeye-sweep` (what's still cuttable).

If the log is empty, `deadeye gain` prints its own explanation -- relay
it as-is, don't add your own.

One-shot: do NOT change the coder level, write any files, or persist
anything.
