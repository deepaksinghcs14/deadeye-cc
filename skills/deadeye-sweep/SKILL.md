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

## How — scope cheaply, this plugin's whole point is token thrift

1. Enumerate with `git ls-files` (or `find` with `-maxdepth` if not a
   git repo) — never by reading directories of files whole.
2. Grep-first for candidates before opening ANY file body: duplicate
   deps (`go.mod`/`package.json` vs stdlib), `interface` declarations
   (then `grep -c` their implementers), one-export files, config keys
   (then grep for readers), wrapper-shaped names (`*Wrapper`, `*Manager`,
   `*Factory`, `*Helper`).
3. Read full file contents ONLY for the top candidates you intend to
   list — a sweep that reads the whole repo into context is the exact
   waste this plugin exists to prevent.

## Verify before reporting

Before listing a finding, confirm it: grep for ALL implementers/callers
across the repo (including test files and other packages) — "interface
with one implementation" must mean one implementer exists, not one you
happened to find. A finding that's wrong on second look erodes trust in
every other finding. Report only what you confirmed.

## What to hunt

- Dependencies that duplicate what the stdlib ships
- Interfaces with a single implementation
- Factories that only ever build one product
- Wrappers that purely delegate
- Files exporting one small thing that belongs next to its caller
- Feature flags and config keys nothing reads
- Abstractions with exactly one call site

## Output discipline

- Rank by lines removable, not by how easy the fix is.
- Cap at 20 findings. Fewer exist → stop; never pad with weak ones.
  More exist → keep the 20 biggest and say how many were omitted.
- Nothing found: exactly `Lean already. Nothing to cut.`
- If a replacement is itself a deliberate simplification with a known
  ceiling (not a straight deletion), include the marker line to plant:
  `# deadeye: <shortcut>. ceiling: <limit>. upgrade: <trigger>.`

## Boundaries

- Report only. Apply nothing unless asked.
- Skip vendored code, generated code, and lockfiles.
- The one runnable check per non-trivial change stays — never list
  smoke tests as bloat.
