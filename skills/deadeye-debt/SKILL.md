---
name: deadeye-debt
description: Ledger of every deadeye: shortcut marker left in the code.
license: MIT
---

# Deadeye Debt

Coder mode marks deliberate simplifications with a pinned grammar — the
literal `ceiling:` and `upgrade:` keywords are required so this ledger
can parse them reliably:

`# deadeye: <shortcut>. ceiling: <limit>. upgrade: <trigger>.`

e.g. `# deadeye: global lock. ceiling: single-writer throughput. upgrade: per-account locks when contention shows.`

This skill collects them into a ledger. Older freeform markers (no
`ceiling:`/`upgrade:` keywords) still count — parse them best-effort.

## How

Run:

```
grep -rnE '(#|//|/\*|<!--) ?deadeye:' . \
  --exclude-dir=node_modules --exclude-dir=.git \
  --exclude-dir=dist --exclude-dir=build --exclude-dir=vendor
```

**Filter before counting — three kinds of hit are not markers, verify each
one before it lands in the ledger:**

1. This skill's own file (`skills/deadeye-debt/SKILL.md`) — its own grammar
   example matches its own grep.
2. A line quoting the grammar template verbatim, with the literal
   placeholders `<shortcut>`, `<limit>`, or `<trigger>` still in it — docs
   and other skills quote the sentence to explain the convention; a real
   marker always has those filled in with actual text, never the bracketed
   words themselves.
3. Non-code brand/marketing copy (an SVG comment, a README aside) that
   happens to start with "deadeye:" but isn't a shortcut-marker comment at
   all — read the hit, don't just count it.

Only what survives that filter is a real marker.

## Ledger format

One row per marker:

`<file>:<line> — <what was simplified>. ceiling: <limit>. upgrade: <trigger>.`

A marker whose comment names no upgrade condition gets tagged
`no-trigger` — those are the ones most likely to rot.

For ownership questions, suggest `git blame -L<line>,<line> <file>`.

More than ~30 markers: group rows by directory and report per-group
counts plus the worst offenders, still ending with the exact total.

End with: `<N> markers, <M> with no trigger.` — or, if the grep finds
nothing: `No deadeye: debt. Clean ledger.`

## Boundaries

- Report only. Write a `DEADEYE-DEBT.md` file only if explicitly asked.
- Do not editorialize about whether a shortcut was justified — the
  ledger's job is visibility, not judgment.
