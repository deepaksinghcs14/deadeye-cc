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
