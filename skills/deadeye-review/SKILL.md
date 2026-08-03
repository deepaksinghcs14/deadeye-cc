---
name: deadeye-review
description: >
  Code review focused exclusively on over-engineering in the current
  diff. Finds what to delete: reinvented standard library, unneeded
  dependencies, speculative abstractions, dead flexibility. One line per
  finding. Use when the user says "review for over-engineering", "what
  can we delete", "is this over-engineered", "simplify review", or
  invokes /deadeye-review. Complements correctness-focused review -- this
  one only hunts complexity.
license: MIT
---

# Deadeye Review

Review ONLY the changed code (the working diff, or the diff the user
names) for over-engineering. Nothing else: correctness, security, and
performance are other reviews' jobs.

## Format

One line per finding:

`L<line>: <tag> <what>. <replacement>.`

Five tags, use exactly these:

- `delete:` — code that shouldn't exist at all (speculative, dead, duplicated)
- `stdlib:` — reinvents something the standard library ships
- `native:` — reinvents a platform feature (HTML input types, CSS, DB constraints)
- `yagni:` — flexibility nothing uses (interface with one impl, config for a constant)
- `shrink:` — works, but a shorter form does the same job

End with `net: -<N> lines possible.` — or, when the diff is already
minimal, exactly: `Lean already. Ship.`

## Examples

✅ `L42: stdlib: hand-rolled JSON deep-merge. encoding/json + one loop covers it.`
✅ `L88: yagni: StorageBackend interface with one implementation. Use the struct.`
✅ `L120: delete: feature flag checked nowhere. Remove flag and dead branch.`
✅ `L07: native: custom date validation regex. <input type="date"> already enforces it.`
✅ `L155: shrink: 12-line builder for a 3-field struct. A literal does it.`

❌ "This section could potentially benefit from some simplification in
certain areas, though it depends on future requirements..." — hedging
prose is itself over-engineering. Name the line, the cut, the
replacement.

## Boundaries

- Findings are a LIST. Do not apply them unless asked.
- Never flag the one runnable check coder mode leaves behind for
  deletion — lean code without its check is unfinished.
- Correctness, security, and performance are OUT of scope here.
