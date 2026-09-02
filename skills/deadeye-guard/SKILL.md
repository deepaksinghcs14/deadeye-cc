---
name: deadeye-guard
description: Security review of the current diff -- injection, secrets, authz, crypto, exposure, DoS, and vulnerable dependencies.
license: MIT
---

# Deadeye Guard

Review ONLY the changed code for security exposures. Nothing else: the
other three lenses (`/deadeye-review`) and lean-lens over-engineering
specifically are not this skill's job.

This is the deep, dedicated security pass: deeper than the security lens
`/deadeye-review`/`/deadeye-pr` run alongside their other lenses, and the
one that reads around the hunk, verifies before reporting, and runs real
dependency auditors where they're installed. It's also the pass behind
coder mode's live Edit/Write advisory: that advisory is a fast regex
reminder on the text just written; this skill does the deep read.

## Scope

Get the diff with `git diff` (or `git diff --staged` if the user says
staged, or `git diff <ref>` for a named base). Read the changed hunks plus
enough surrounding context to judge a trust boundary — "is this input
actually validated" often requires seeing the caller.

- Empty diff (nothing changed or staged): say so plainly and stop — do
  not substitute a different scope.
- Not a git repo: ask the user which files to review.
- Diff-scoped by design, not repo-wide — a whole-repo sweep would re-read
  everything into context for exposures that haven't changed; that's what
  native auditors and periodic CI scanning are for.

## Verify before reporting

Before claiming a sanitizer, an authz check, or input validation is
MISSING, grep OUTSIDE the diff AND follow the value into the callee — a
base class, a caller that guards, or the deeper function it's handed to —
the real guard often lives one call down, not just in middleware or a
framework-level decorator. An unguarded-looking handler whose auth
actually lives in a router `Use()` call, or one call deeper, is a false
positive, and one wrong finding erodes trust in all of them. An `authz`
claim needs a concrete input that reaches the sink, or drop it.

**Every finding carries its proof.** Append a `proof:` clause naming the
concrete thing in THIS repo that makes the finding true — the caller you
traced, the grep that came back empty, the auditor line. A finding you
cannot prove from the code in front of you is a guess; drop it.

**Run the repo's own checks and fuse them in.** Before you finalize, run
what the project already ships — `go vet`, `tsc --noEmit`, the linter, a
touched test — and let their output confirm or kill findings. Mark a
finding `(confirmed)` when a tool agrees; otherwise it stands as `likely`.

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

**Feed the learning loop.** For each finding that survives verification and
makes your final report (never a candidate you dropped), record it so
coder mode gets reminded next session (best-effort — if `deadeye` isn't on
PATH, retry once with `~/.deadeye/bin/deadeye`; if that also fails, move
on, it's never a reason to withhold the finding):

```bash
deadeye lessons record coder-miss security:<tag>
```

using the finding's tag name without its trailing colon (a `crypto:`
finding → `security:crypto`). This is a no-op when coder mode wasn't
active this session — nothing to attribute, nothing gets written.

## Dependency pass

Detect the ecosystem from the manifest OR its lockfile touched in the diff
(`go.mod`/`go.sum`, `package.json`+lockfile,
`requirements.txt`/`pyproject.toml`+lockfile, `Cargo.toml`/`Cargo.lock`,
`pom.xml`/`build.gradle`), then run its native auditor if installed:

| Ecosystem | Command |
|---|---|
| Go | `govulncheck ./...` |
| npm | `npm audit --json` |
| Python | `pip-audit -f json` (or `osv-scanner -L requirements.txt`) |
| Rust | `cargo audit` |
| any | `osv-scanner -L <manifest>` as a fallback |

A lockfile-only bump needs the same pass — a vulnerable version can land
transitively with no manifest edit. A newly ADDED dependency also gets a
direct OSV cross-check even when a native auditor exists, matching the bar
coder mode's live Edit/Write advisory already holds new deps to.

Also flag CI supply chain: an unpinned GitHub Actions ref (`uses: x@main`,
not a SHA), a mutable Docker base image (`:latest`), or a `curl | sh`
install script.

If the tool isn't installed, SAY SO and fall back to what coder mode's
live advisory already used — the bundled superseded-package table and
`~/.deadeye/osv-cache.json` — rather than fabricating a CVE list. When a
dependency is vulnerable or abandoned, report the fix in ladder order:
stdlib or native first, a maintained sibling second, a version bump last
— deleting the dependency is a fix too, and often the shortest one.

## Format

One line per finding:

`<glyph> path:line — <tag>: <what reaches what>. Fix: <fix>. proof: <evidence>.`

`<glyph>` carries the severity: 🔴 `critical` (exploitable now, data loss,
breaks prod), 🟠 `high` (must fix before merge), 🟡 `medium` (should fix),
⚪ `nit` (optional). The path is required — a diff can span files; name a
sibling path in the same breath if it shares the bug.

Seven tags, use exactly these:

- `inject:` — untrusted input reaches SQL, a shell, a template, a path, `eval`, a URL fetch (SSRF), a raw-HTML/DOM sink (XSS), or a deserializer
- `secret:` — a credential literal, or a secret handled somewhere it can leak (logs, error messages, client-visible output)
- `authz:` — a decision or resource access with no confirmed permission check
- `crypto:` — hand-rolled or weak crypto (MD5/SHA1 for passwords, a non-CSPRNG for a token, TLS verification disabled)
- `expose:` — sensitive data returned/logged beyond what the caller needs
- `dep:` — a vulnerable or superseded dependency, from the pass above
- `dos:` — untrusted input sizes an allocation, an unbounded loop, or unbounded recursion → memory or CPU exhaustion. Cap it, or bound the input first

Rank by exploitability (reachable from untrusted input first). End with
`<C> critical, <H> high, <M> medium, <N> nits, <A> accepted` (accepted = the
marked, decided-corners count from the verify step) — or, when nothing
survives, exactly: `Clean line of fire.`

More than ~12 findings: keep the highest-exploitability ones and say how
many lower-severity ones were omitted.

## Examples

✅ `🔴 auth.go:42 — inject: name interpolated into a raw SQL string. Fix: bind it as a query parameter. proof: L42 builds the query with fmt.Sprintf, no placeholder.`
✅ `🟠 admin.go:88 — authz: /admin/users has no role check in this diff or its router group. Fix: add one, or confirm it's covered upstream. proof: grep for RequireRole across the package returned nothing.`
✅ `🟠 package.json:15 — dep: lodash 4.17.20 has an open OSV advisory (GHSA-p6mc-m468-83gg). Fix: 4.17.21 patches it, or drop it -- the two helpers used here are stdlib now. proof: osv-scanner -L package.json.`
✅ `🟡 auth.go:120 — crypto: password hashed with md5. Fix: use bcrypt/scrypt/argon2 instead. proof: L120 calls md5.Sum on the raw password.`

❌ "This endpoint might have some security considerations worth thinking
about..." — hedging isn't a finding. Name the path, the line, the
reachable input, the fix, the proof.

## Boundaries

- Findings are a LIST. Do not apply them unless asked.
- Never flag coder mode's one runnable check for deletion — lean code
  without its check is unfinished.
- Never fabricate a CVE, an advisory ID, or a fixed version you didn't
  actually see from a tool or the OSV cache.
- Lean-lens over-engineering (unnecessary abstraction, reinvented stdlib,
  a shorter form that does the same job) is OUT of scope here — that's
  `/deadeye-review`.
