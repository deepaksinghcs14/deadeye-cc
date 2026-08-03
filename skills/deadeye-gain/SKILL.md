---
name: deadeye-gain
description: Measured-impact scoreboard from the decision log -- real numbers only.
license: MIT
---

# Deadeye Gain

Run `deadeye gain` (fall back to `~/.deadeye/bin/deadeye gain` if the
binary isn't on PATH) and present its output as-is: the scoreboard is
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

One-shot: do NOT change the coder level, write any files, or persist
anything.
