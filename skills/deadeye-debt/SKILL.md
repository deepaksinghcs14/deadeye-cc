---
name: deadeye-debt
description: Ledger of every deadeye: shortcut marker left in the code.
license: MIT
---

# Deadeye Debt

Coder mode marks deliberate simplifications with a comment naming the
ceiling and the upgrade trigger:

`# deadeye: global lock, per-account locks if throughput matters`

This skill collects them into a ledger.

## How

Run:

```
grep -rnE '(#|//) ?deadeye:' . \
  --exclude-dir=node_modules --exclude-dir=.git \
  --exclude-dir=dist --exclude-dir=build --exclude-dir=vendor
```

## Ledger format

One row per marker:

`<file>:<line> — <what was simplified>. ceiling: <limit>. upgrade: <trigger>.`

A marker whose comment names no upgrade condition gets tagged
`no-trigger` — those are the ones most likely to rot.

For ownership questions, suggest `git blame -L<line>,<line> <file>`.

End with: `<N> markers, <M> with no trigger.` — or, if the grep finds
nothing: `No deadeye: debt. Clean ledger.`

## Boundaries

- Report only. Write a `DEADEYE-DEBT.md` file only if explicitly asked.
- Do not editorialize about whether a shortcut was justified — the
  ledger's job is visibility, not judgment.
