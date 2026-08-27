---
name: deadeye-pr
description: PR review across four lenses -- over-engineering, correctness, performance, security -- printed locally, opt-in to post.
license: MIT
argument-hint: "[<PR number or URL>] [--post]"
---

<!-- deadeye-pr: canonical rubric; edit internal/prreview/ruleset.md, the skill and every host rendering are generated from it -->
# Deadeye PR Review

One shot over a whole pull request: four lenses, one pass, tagged findings.
This is broader than `/deadeye-review` (lean lens only) and `/deadeye-guard`
(security only) on purpose — a PR wants correctness and performance judged
too. It reuses those two skills' exact tag rubrics and adds two more, and
keeps the same terse one-line-per-finding discipline. Correctness- and
performance-heavy PRs can still be escalated to the host's own deep reviewer;
this is the fast, on-demand, deadeye-flavored pass.

## Scope

Resolve the target PR, then review only its diff:

- An argument (a PR number like `123` or a full PR URL) → that PR.
- No argument → the PR for the current branch.
- Fetch the diff and metadata with the GitHub CLI:
  - `gh pr diff <N>` (or `gh pr diff` for the current branch) for the unified diff.
  - `gh pr view <N> --json title,body,additions,deletions,files,baseRefName,headRefName` for the header.
- Read the changed hunks **plus enough surrounding context to judge a trust
  boundary or a caller contract** — "is this input validated" and "does this
  break a caller" both need the code around the hunk, not just the `+` lines.

Preconditions and graceful degradation:

- `gh` not installed or not authenticated → say so plainly and stop, or, if
  the user has a local branch, offer to review `git diff <base>...HEAD`
  instead. Do not invent PR contents.
- Not a GitHub repo / no PR for the branch → say so; don't substitute a
  different scope.
- Huge PR (more than ~40 changed files or a few thousand lines) → review it ALL
  by fanning out one subagent per ~2,500-line package-grouped cluster, in
  parallel, each returning findings in the standard format. Verify every
  returned finding yourself before reporting it. Never truncate, and never
  report partial coverage as complete.

## Verify before reporting

Before claiming a check is MISSING — a sanitizer, an authz guard, input
validation, a nil-check — grep for it OUTSIDE the diff: middleware, a
decorator, a base class, a router `Use()` call, a caller that already
guards. An unguarded-looking handler whose auth lives one file over is a
false positive, and one wrong finding erodes trust in all of them. Report
only what you confirmed.

**Every finding carries its proof.** Append a `proof:` clause naming the
concrete thing in THIS repo that makes the finding true — the caller you
traced, the grep that came back empty, the auditor line, the test that
fails. A finding you cannot prove from the code in front of you is a guess;
drop it. Precision is the product: one finding that's true beats ten maybes,
and every hosted reviewer drowns in the maybes — that's the gap you win on.

**Run the repo's own checks and fuse them in.** Before you finalize, run what
the project already ships when it's present — `go vet`, `tsc --noEmit`, the
linter, the tests the diff touches — and let their output confirm or kill
findings. Mark a finding `(confirmed)` when a tool or a failing test agrees,
otherwise it stands as `likely`. You can run the code; a diff-only bot can't
— that is the edge, so use it.

A `deadeye: <shortcut>. ceiling: <limit>. upgrade: <trigger>.` comment over a
hunk is a recorded DECISION, not a finding — someone already chose to ship
that corner with eyes open. Count those separately as accepted, don't flag
them. Never flag the one runnable check coder mode leaves behind for
deletion — lean code without its check is unfinished.

## The four lenses

Review the diff through each lens. One line per finding, ranked most-severe
first within each lens:

`path:line: <sev> <tag> <what>. <fix>. proof: <evidence>.`

- `<sev>` is `block` (must fix before merge), `warn` (should fix), or `nit` (optional).
- The path is required — a PR spans files, so every finding names its file.
- `proof:` is required (see "Verify before reporting"). For `inject`/`authz`/
  `logic`/`race`, the proof IS a reproduction: the concrete input and the
  sink it reaches.
- Append `(confirmed)` when a tool or test backs the finding; otherwise it
  reads as `likely`.

### Over-engineering (lean lens — from `/deadeye-review`)

- `delete:` — code that shouldn't exist at all (speculative, dead, duplicated)
- `stdlib:` — reinvents what the standard library, or a dependency already in the project, ships
- `native:` — reinvents a platform feature (HTML input types, CSS, DB constraints)
- `yagni:` — flexibility nothing uses (interface with one impl, config for a constant)
- `shrink:` — works, but a shorter form does the same job

Log spam is over-instrumentation, cut it: a line per loop iteration, a metric
nobody reads, a span on a trivial call → `delete:`/`shrink:`. But the one
breadcrumb at a real failure boundary is signal, not bloat — leave it.

Before `yagni:`/`delete:`, grep for implementers/callers outside the diff — a
second impl in a test file makes it a false positive. Footer:
`net: -<N> lines possible.` or, if already minimal, `Lean already.`

### Correctness

- `logic:` — wrong result or a mishandled edge case (empty, zero, boundary, unicode)
- `nil:` — an unchecked nil / null / undefined, a swallowed/ignored error, or a failure path that leaves no diagnostic behind
- `race:` — a data race or unsynchronized shared state under concurrency
- `bound:` — off-by-one, slice/array overrun, integer overflow
- `contract:` — violates a caller assumption or the function's own documented contract
- `leak:` — a resource opened and never released: a file/conn/rows with no `defer Close()`, a leaked goroutine, an un-cancelled context.
- `break:` — a removed/renamed export, or a changed public signature/behavior, that breaks existing consumers — even when the diff compiles.
- `untested:` — non-trivial changed logic (branch, loop, parser, money/security path) with no test exercising it. Name the regression that would slip through.
- `a11y:` — (UI diffs only) a control that shuts some users out: an image with no alt text, an input with no label, a click handler on a non-interactive element with no keyboard path, a stripped focus outline, or color as the only signal.

Rank by likelihood of actually firing. Footer: `<N> correctness risks.` or
`Reads correct.`

### Performance

- `alloc:` — a needless allocation or copy on a hot path
- `nplus1:` — a query or expensive call repeated in a loop that could be batched
- `complexity:` — O(n²) or worse where n grows with real input
- `blocking:` — synchronous I/O or a lock held on a latency-sensitive path
- `copy:` — a large value passed or returned by value where a reference would do

Only flag what a realistic input size makes matter — a triple loop over three
config keys is not a finding. Footer: `<N> perf risks.` or `No hot-path cost.`

### Security (from `/deadeye-guard`)

- `inject:` — untrusted input reaches SQL, a shell, a template, a path, or `eval`
- `secret:` — a credential literal, or a secret handled where it can leak (logs, errors, client output)
- `authz:` — a decision or resource access with no confirmed permission check
- `crypto:` — hand-rolled or weak crypto (MD5/SHA1 for passwords, non-CSPRNG token, TLS verification off)
- `expose:` — sensitive data returned or logged beyond what the caller needs
- `dep:` — a vulnerable or superseded dependency
- `dos:` — untrusted input sizes an allocation, an unbounded loop, or unbounded recursion → memory or CPU exhaustion. Cap it, or bound the input first.

If a dependency manifest changed (`go.mod`, `package.json`,
`requirements.txt`/`pyproject.toml`, `Cargo.toml`, `pom.xml`/`build.gradle`),
run its native auditor if installed — `govulncheck ./...`, `npm audit`,
`pip-audit`, `cargo audit`, or `osv-scanner -L <manifest>` as a fallback. If
none is installed, SAY SO rather than fabricating a CVE. Never invent an
advisory ID or a fixed version you didn't see from a tool. Rank by
exploitability. Footer: `<N> exposures, <M> accepted.` or `Clean line of fire.`

## Output

Lead with a one-line header, then the four lens sections, then a verdict:

```
PR #<N> "<title>"  +<adds>/-<dels>, <files> files
```

End with the tally and the verdict — `<B> blockers, <W> warns, <N> nits` and
the one `block` that must be fixed before merge — or, when nothing survived
verification, exactly: `Clean — nothing survived verification. Ship it.`

Findings are a LIST. Do not apply or push any code change unless asked.

## Posting back to the PR (opt-in only)

Default is print-only — nothing is sent anywhere. Post the review to GitHub
ONLY when the user passes `--post` or explicitly asks:

- Show the exact comment body first and get an explicit yes — posting is
  outward-facing and public on the PR.
- **Redact any secret value** a `secret:`/`expose:` finding surfaced before it
  goes into a public comment — name the location, never the credential.
- Post ONE review that anchors each finding to its line — not a wall of text
  in a single comment. `gh pr review` only posts a summary body, so use the
  API for inline anchors: build a JSON payload and
  `gh api repos/{owner}/{repo}/pulls/<N>/reviews --input -` with
  - `event: "COMMENT"`,
  - `body`: the tally + verdict (the summary),
  - `comments`: one entry per finding, `{path, line, side: "RIGHT", body}`,
    the body being the finding line (severity, tag, fix, proof).
  Get `{owner}/{repo}` from `gh repo view --json nameWithOwner`. Anchor `line`
  to a line the diff actually touches, or GitHub rejects the comment.
- `event: "COMMENT"` only — never approve, request-changes, merge, or close.
