---
name: deadeye-debt
description: >
  Harvest every `deadeye:` marker comment in the codebase into a debt
  ledger, so the deliberate shortcuts coder mode leaves behind get
  tracked instead of rotting into "later means never". Also finds legacy
  `ponytail:` markers. Use when the user says "deadeye debt", "what did
  we defer", "list the shortcuts", or invokes /deadeye-debt. One-shot
  report; changes nothing.
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
grep -rnE '(#|//) ?(deadeye|ponytail):' . \
  --exclude-dir=node_modules --exclude-dir=.git \
  --exclude-dir=dist --exclude-dir=build --exclude-dir=vendor
```

(`ponytail:` markers are legacy from the upstream convention this was
ported from — report them identically, noting the marker type.)

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
