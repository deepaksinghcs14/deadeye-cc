---
name: deadeye-review
description: Over-engineering review -- what to delete and what replaces it. The working diff by default, or the whole repo with --repo.
license: MIT
argument-hint: "[--repo]"
---

# Deadeye Review

Review code for over-engineering. Nothing else: correctness and
performance are other reviews' jobs (Claude Code's own `/code-review`
covers those, and `/deadeye-pr` folds them in at PR scope), and security
has its own dedicated pass — `/deadeye-guard`. This is the lean lens only.

Two scopes:

- **default** — the current working diff.
- **`--repo`** (or "audit the whole repo") — the entire repository, ranked
  biggest cut first. See "Whole-repo mode" below.

## Scope (default: the working diff)

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
- `stdlib:` — reinvents what the standard library, or a dependency already in the project, ships
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

## Whole-repo mode (`--repo`)

Scan the whole repository for over-engineering and report a ranked list —
biggest cut first. Same five tags, one line each, but path-anchored since
findings span files:

`<tag> <what to cut>. <replacement>. [path]`

End with `net: -<N> lines, -<M> deps possible.`

**Scope cheaply — token thrift is this plugin's whole point:**

1. Enumerate with `git ls-files` (or `find` with `-maxdepth` if not a git
   repo) — never by reading directories of files whole.
2. Grep-first for candidates before opening ANY file body: duplicate deps
   (`go.mod`/`package.json` vs stdlib), `interface` declarations (then
   `grep -c` their implementers), one-export files, config keys (then grep
   for readers), wrapper-shaped names (`*Wrapper`, `*Manager`, `*Factory`,
   `*Helper`).
3. Read full file contents ONLY for the top candidates you intend to list —
   a sweep that reads the whole repo into context is the exact waste this
   plugin exists to prevent.

**Verify before reporting:** grep for ALL implementers/callers across the
repo (including test files and other packages) — "interface with one
implementation" must mean one implementer exists, not one you happened to
find. Report only what you confirmed.

**What to hunt:** dependencies duplicating the stdlib; interfaces with a
single implementation; factories that only build one product; wrappers that
purely delegate; files exporting one small thing that belongs next to its
caller; feature flags and config keys nothing reads; abstractions with
exactly one call site.

**Output discipline:** rank by lines removable, not by how easy the fix is.
Cap at 20 findings — fewer exist → stop, never pad; more → keep the 20
biggest and say how many were omitted. Nothing found: exactly
`Lean already. Nothing to cut.` If a replacement is itself a deliberate
simplification with a known ceiling (not a straight deletion), plant the
marker line: `# deadeye: <shortcut>. ceiling: <limit>. upgrade: <trigger>.`
Skip vendored code, generated code, and lockfiles.

## Boundaries

- Findings are a LIST. Do not apply them unless asked.
- Never flag the one runnable check coder mode leaves behind for
  deletion — lean code without its check is unfinished.
- Log spam is over-instrumentation, cut it (a line per loop, a metric nobody
  reads) — but never flag the one breadcrumb at a real failure boundary as
  bloat; a wrapped error or the log where it fails is load-bearing, like the
  runnable check.
- Correctness and performance are OUT of scope here; security is
  `/deadeye-guard`'s job, not this one.
