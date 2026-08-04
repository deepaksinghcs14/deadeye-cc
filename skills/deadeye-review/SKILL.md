---
name: deadeye-review
description: Over-engineering review of the current diff -- what to delete and what replaces it.
license: MIT
---

# Deadeye Review

Review ONLY the changed code for over-engineering. Nothing else:
correctness, security, and performance are other reviews' jobs (Claude
Code's own `/code-review` covers those; this is the lean lens).

## Scope

Get the diff with `git diff` (or `git diff --staged` if the user says
staged, or `git diff <ref>` for a named base). Read only the changed
hunks plus minimal surrounding context — do not open unrelated files.

- Empty diff (nothing changed or staged): say so plainly and stop — do
  not substitute a different scope.
- Not a git repo: ask the user which files to review.

Before tagging `yagni:` or `delete:`, grep for implementers/callers
OUTSIDE the diff — an "interface with one impl" whose second impl lives
in a test file is a false positive, and one wrong finding erodes trust
in all of them. Report only what you confirmed.

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

More than ~15 findings: keep the ones with the biggest `net:` impact
and say how many smaller ones were omitted.

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
