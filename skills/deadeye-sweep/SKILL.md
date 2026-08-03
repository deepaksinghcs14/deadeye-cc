---
name: deadeye-sweep
description: Whole-repo over-engineering audit, ranked by biggest cut first.
license: MIT
---

# Deadeye Sweep

Scan the whole repository for over-engineering and report a ranked
list — biggest cut first. Same five tags as /deadeye-review
(`delete:` `stdlib:` `native:` `yagni:` `shrink:`), one line each:

`<tag> <what to cut>. <replacement>. [path]`

End with `net: -<N> lines, -<M> deps possible.`

## What to hunt

- Dependencies that duplicate what the stdlib ships
- Interfaces with a single implementation
- Factories that only ever build one product
- Wrappers that purely delegate
- Files exporting one small thing that belongs next to its caller
- Feature flags and config keys nothing reads
- Abstractions with exactly one call site

## Boundaries

- Rank by lines removable, not by how easy the fix is.
- Report only. Apply nothing unless asked.
- Skip vendored code, generated code, and lockfiles.
- The one runnable check per non-trivial change stays — never list
  smoke tests as bloat.
