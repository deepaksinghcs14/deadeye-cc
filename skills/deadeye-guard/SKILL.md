---
name: deadeye-guard
description: Security review of the current diff -- injection, secrets, authz, crypto, exposure, DoS, and vulnerable dependencies.
license: MIT
---

# Deadeye Guard

Review ONLY the changed code for security exposures. Nothing else: lean-
lens over-engineering is `/deadeye-review`'s job, not this one.

This is the deep pass behind coder mode's live Edit/Write advisory: the
advisory is a fast regex reminder on the text just written; this skill
reads around the hunk, verifies before reporting, and runs real dependency
auditors where they're installed.

## Scope

Get the diff with `git diff` (or `git diff --staged` if the user says
staged, or `git diff <ref>` for a named base). Read the changed hunks plus
enough surrounding context to judge a trust boundary — more than
`/deadeye-review` needs, since "is this input actually validated" often
requires seeing the caller.

- Empty diff (nothing changed or staged): say so plainly and stop — do
  not substitute a different scope.
- Not a git repo: ask the user which files to review.
- Diff-scoped by design, not repo-wide — a whole-repo sweep would re-read
  everything into context for exposures that haven't changed; that's what
  native auditors and periodic CI scanning are for.

## Verify before reporting

Before claiming a sanitizer, an authz check, or input validation is
MISSING, grep for it OUTSIDE the diff — middleware, a decorator, a
framework-level guard, a base class. An unguarded-looking handler whose
auth actually lives in a router `Use()` call is a false positive, and one
wrong finding erodes trust in all of them. Report only what you confirmed.

The inverse is just as important: a guard is only as good as its weakest
path. When the diff adds or hardens a check on a sink, grep the file and
package for every OTHER path to the same sink — a second `http.Client`, a raw
fetch, a probe that runs before the guarded call, a duplicate "is-this-safe"
predicate that can drift. A guard on one path with an unguarded sibling is a
fix-shaped diff, not a fix: flag the sibling, cite both lines. The SSRF that
ships is almost always the door nobody guarded, next to the one that got
reviewed.

A `deadeye: <shortcut>. ceiling: <limit>. upgrade: <trigger>.` comment
covering a hunk is a recorded DECISION, not a finding — someone already
chose to ship that exposure with eyes open. Count it separately from what
you flag; `/deadeye-debt` owns the ledger of those.

## Dependency pass

Detect the ecosystem from the manifest touched in the diff (`go.mod`,
`package.json`, `requirements.txt`/`pyproject.toml`, `Cargo.toml`,
`pom.xml`/`build.gradle`), then run its native auditor if installed:

| Ecosystem | Command |
|---|---|
| Go | `govulncheck ./...` |
| npm | `npm audit --json` |
| Python | `pip-audit -f json` (or `osv-scanner -L requirements.txt`) |
| Rust | `cargo audit` |
| any | `osv-scanner -L <manifest>` as a fallback |

If the tool isn't installed, SAY SO and fall back to what coder mode's
live advisory already used — the bundled superseded-package table and
`~/.deadeye/osv-cache.json` — rather than fabricating a CVE list. When a
dependency is vulnerable or abandoned, report the fix in ladder order:
stdlib or native first, a maintained sibling second, a version bump last
— deleting the dependency is a fix too, and often the shortest one.

## Format

One line per finding:

`L<line>: <tag> <what reaches what>. <fix>.`

Seven tags, use exactly these:

- `inject:` — untrusted input reaches SQL, a shell, a template, a path, or `eval`
- `secret:` — a credential literal, or a secret handled somewhere it can leak (logs, error messages, client-visible output)
- `authz:` — a decision or resource access with no confirmed permission check
- `crypto:` — hand-rolled or weak crypto (MD5/SHA1 for passwords, a non-CSPRNG for a token, TLS verification disabled)
- `expose:` — sensitive data returned/logged beyond what the caller needs
- `dep:` — a vulnerable or superseded dependency, from the pass above
- `dos:` — untrusted input sizes an allocation, an unbounded loop, or unbounded recursion → memory or CPU exhaustion. Cap it, or bound the input first

Rank by exploitability (reachable from untrusted input first). End with
`<N> exposures, <M> accepted.` (accepted = the marked, decided-corners
count from the verify step) — or, when nothing survives, exactly:
`Clean line of fire.`

More than ~12 findings: keep the highest-exploitability ones and say how
many lower-severity ones were omitted.

## Examples

✅ `L42: inject: name interpolated into a raw SQL string. Bind it as a query parameter.`
✅ `L88: authz: /admin/users has no role check in this diff or its router group. Add one, or confirm it's covered upstream.`
✅ `L15: dep: lodash 4.17.20 has an open OSV advisory (GHSA-p6mc-m468-83gg). 4.17.21 patches it, or drop it -- the two helpers used here are stdlib now.`
✅ `L120: crypto: password hashed with md5. Use bcrypt/scrypt/argon2 instead.`

❌ "This endpoint might have some security considerations worth thinking
about..." — hedging isn't a finding. Name the line, the reachable input,
the fix.

## Boundaries

- Findings are a LIST. Do not apply them unless asked.
- Never flag coder mode's one runnable check for deletion — lean code
  without its check is unfinished.
- Never fabricate a CVE, an advisory ID, or a fixed version you didn't
  actually see from a tool or the OSV cache.
- Lean-lens over-engineering (unnecessary abstraction, reinvented stdlib,
  a shorter form that does the same job) is OUT of scope here — that's
  `/deadeye-review`.
