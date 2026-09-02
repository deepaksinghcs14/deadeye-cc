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

- An argument (a PR number like `123` or a full PR URL, after stripping
  `--post`) → that PR.
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
  parallel, each returning findings in the standard format. Spawn each cluster
  subagent at the cheapest tier that fits it, but the review floor is tier 1
  (sonnet) for any cluster with real logic — drop to tier 0 only for a purely
  mechanical cluster (generated code, lockfiles, vendored deps, pure renames),
  and reserve the top tier for a cluster on a risky surface (auth, crypto,
  concurrency, raw SQL or shell, money). Verify every returned finding yourself
  before reporting it. Never truncate, and never report partial coverage as
  complete. Then run one integration pass over the combined findings for what
  no single cluster sees alone — an export removed in one, its only caller
  in another (`break:`/`contract:`).

## Verify before reporting

Before claiming a check is MISSING — a sanitizer, an authz guard, a
nil-check — grep OUTSIDE the diff AND follow the value into the callee: a
base class, a caller that guards, or the deeper function
it's handed to — the real guard often lives one call down. An `authz`/bypass
claim needs a concrete input that reaches the sink, or drop it; one wrong
finding erodes trust in all of them.

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

## Rigor — where reviews miss

Precision is the floor. Four habits separate a real review from a plausible one:

- **Sweep every instance.** One leak, missing registration, or hollow test → check every sibling, in AND out of the diff. A fix with an unfixed twin is a half-fix — name the twin.
- **Disprove your own mitigation.** "X covers it" isn't a pass until X provably runs on the failing path — an early `return`/guard that fires first makes X moot. For a branch gated on a non-null/present field, read the migration: is old data backfilled?
- **The bugs a scan slides past:** two arms handling one value (success/error) should mirror — flag the one missing a capture/close/guard; a rewritten condition must keep every predicate it AND-ed (a dropped `ok &&` re-admits what it rejected); a value can pass `isinstance`/`!= undefined` yet be wrong (`str` subclass, `null` vs `undefined`); an error branch returning a nil used later; in-place mutation of a list aliased from a default arg, shared config, or module cache; every `await` — can it never resolve, and does pre-await state still hold after (abort, concurrent completion)?
- **Sweep the cheap layer:** dead scaffolding, unused imports, placeholder secrets, unpinned deps, a `default:` giving a CPU host a GPU image; a test that mocks its own unit proves nothing.

## The four lenses

Review the diff through each lens. One line per finding, ranked most-severe
first within each lens:

Each finding is one comment — write it like a sharp human reviewer, not a
linter firing rules:

`<glyph> path:line — <tag>: <what actually happens, concretely>. Fix: <fix>. proof: <evidence>.`

- `<glyph>` carries the severity: 🔴 `critical` (exploitable now, data loss,
  breaks prod), 🟠 `high` (pre-merge), 🟡 `medium` (should fix), ⚪ `nit`
  (optional).
- **Lead with the consequence, in plain words** — what breaks or what an
  attacker reaches, not just the tag. "The raw user URL reaches `http.Get`, so
  `target=http://169.254.169.254/` walks to your cloud metadata" lands;
  "unvalidated input" does not.
- The path is required — a PR spans files. If a sibling path shares the bug,
  name it in the same breath.
- `proof:` is required (see "Verify before reporting"). For `inject`/`authz`/
  `logic`/`race`, the proof IS a reproduction: the concrete input and the sink
  it reaches.
- Append `(confirmed)` when a tool or test backs the finding; otherwise it
  reads as `likely`. Direct, not rude — you're helping a peer ship.

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

- `logic:` — wrong result or a mishandled edge case (empty, zero, boundary, unicode, before/after state, rollback/revert, an AST/node-kind contract)
- `nil:` — an unchecked nil / null / undefined, a swallowed/ignored error, or a failure path that leaves no diagnostic behind
- `race:` — a data race, unsynchronized shared state, async cancellation, a promise that never resolves, an ordering race, or check-then-act invalidated across `await`
- `bound:` — off-by-one, slice/array overrun, integer overflow
- `contract:` — violates a caller assumption or the function's own documented contract
- `leak:` — a resource opened and never released: file/conn/rows, goroutine, context, remote/session handle, transaction, timer, lock, subscription, temp file, or missing cleanup-registration.
- `break:` — a removed/renamed export, or a changed public signature/behavior, that breaks existing consumers — even when the diff compiles.
- `untested:` — non-trivial changed logic with no test exercising it, or a hollow test that mocks its own unit or skips rollback/cancel/error. Name the regression that would slip through.
- `a11y:` — (UI diffs only) a control that shuts some users out (missing alt text, an unlabeled input, a non-interactive click handler with no keyboard path, a stripped focus outline, color as the only signal) or breaks visually (clips on mobile, unreadable contrast, a broken breakpoint).

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

- `inject:` — untrusted input reaches SQL, a shell, a template, a path, `eval`, a URL fetch (SSRF), a raw-HTML/DOM sink (XSS), or a deserializer
- `secret:` — a credential literal, or a secret handled where it can leak (logs, errors, client output)
- `authz:` — a decision or resource access with no confirmed permission check
- `crypto:` — hand-rolled or weak crypto (MD5/SHA1 for passwords, non-CSPRNG token, TLS verification off)
- `expose:` — sensitive data returned or logged beyond what the caller needs
- `dep:` — a vulnerable or superseded dependency
- `dos:` — untrusted input sizes an allocation, an unbounded loop, or unbounded recursion → memory or CPU exhaustion. Cap it, or bound the input first.

**A guard is only as good as its weakest path.** When the diff adds or hardens
a check on a sink, grep the file and package for *every other path to the same
sink* — a second `http.Client`, a raw fetch, a probe that runs *before* the
guarded call, a duplicate "is-this-safe" predicate that can drift. A guard on
one path with an unguarded sibling is a fix-shaped diff, not a fix: flag the
sibling with the same tag and cite both lines in `proof:`. The SSRF that ships
is almost always the door nobody guarded.

If a dependency manifest OR its lockfile changed (`go.mod`/`go.sum`,
`package.json`+lockfile, `requirements.txt`/`pyproject.toml`+lockfile,
`Cargo.toml`/`Cargo.lock`, `pom.xml`/`build.gradle`), run its native auditor
if installed — `govulncheck ./...`, `npm audit`, `pip-audit`, `cargo audit`
— or `osv-scanner -L <manifest>` if none is. A newly ADDED dep also gets a
direct OSV cross-check. A lockfile-only bump needs the same pass — a vuln
can land transitively with no manifest edit. Also
flag CI supply chain: an unpinned Action ref (`x@main`), a `:latest`
Docker base, or `curl | sh`. No auditor installed →
SAY SO, don't fabricate a CVE. Never invent an advisory ID or fixed version
you didn't see from a tool. Rank by exploitability. Footer: `<N> exposures,
<M> accepted.` or `Clean line of fire.`

## Don't repeat what's already on the PR

Before you report, read what's already there — re-posting a finding another
reviewer already made is how a review loses trust. Fetch the existing comments
(bots like CodeRabbit / CodeAnts post here too):

- `gh api repos/{owner}/{repo}/pulls/<N>/comments` — inline review threads
- `gh api repos/{owner}/{repo}/issues/<N>/comments` — the PR conversation
- `gh api repos/{owner}/{repo}/pulls/<N>/reviews` — summary bodies, incl.
  deadeye's own prior run

Drop anything already raised — match on the sink or the fix, not exact wording
(you and a bot word the same bug differently). Report only net-new, and print
one honest line so coverage stays clear —
`N findings already raised by existing reviewers — skipped` — whether you're
posting or just printing.

## Learning loop (repo-scoped priority)

Before finalizing, run `deadeye lessons priority` (best-effort — if
`deadeye` isn't on PATH, retry once with `~/.deadeye/bin/deadeye`; if that
also fails, review normally). It prints this repo's recent signal, if any:

- **Recent coder misses** — scrutinize those lens/tags harder; a shape that
  slipped through before is worth a second look.
- **Recently disputed findings** — need stronger `proof:` before reporting
  that lens/tag again. Never skip it outright: one dismissal doesn't retire
  a whole tag, it only raises the bar for the next one.

When the user disputes a finding you reported ("that's not a bug",
"already handled", "won't fix"), record it so the next review on this repo
weighs that lens/tag accordingly:

```bash
deadeye lessons record review-false-positive <lens>:<tag>
```

using the lens the finding came from (`over-engineering`, `correctness`,
`performance`, or `security`) and its tag without the trailing colon —
e.g. a disputed `race:` finding → `deadeye lessons record review-false-positive correctness:race`.

**Catch what you missed.** Among the other reviewers' comments you already
fetched above (for dedup), some may be a real, concrete finding you did NOT
report yourself — a human or another bot catching something your own pass
missed. For each one that reads like a genuine bug or exposure, not a style
preference, a question, or unrelated feedback: verify it the same way you'd
verify your own finding — trace it in the actual diff, don't just trust the
claim (a review comment is a claim, not a work order). If it holds up and
you didn't already report it, record it under the lens/tag it belongs to:

```bash
deadeye lessons record external-miss <lens>:<tag>
```

## Activity tracking (for the report)

Separately from the learning loop above, `deadeye report` builds a local
status page from raw review activity — how many PRs got reviewed, how many
findings, how many actually posted. Same best-effort contract as every
other write-back here (if `deadeye` isn't on PATH, retry once with
`~/.deadeye/bin/deadeye`; if that also fails, keep going — this never gates
the review):

- **Once per run, always**, regardless of what you find:
  `deadeye report record reviewed`
- **Once per finding that survives to the final report** (never a raw
  candidate, never one already raised by another reviewer):
  `deadeye report record finding <lens>:<severity>` — the lens
  (`over-engineering`, `correctness`, `performance`, `security`) and the
  finding's severity word (`critical`, `high`, `medium`, `nit`, matching its
  glyph), e.g. `deadeye report record finding security:critical`.
- **Once per finding dropped in the "Don't repeat" pass above**:
  `deadeye report record skipped`
- **Once per finding actually included in a `--post`ed review** (see
  "Posting back to the PR" below) — only after the post succeeds, never for
  a print-only run: `deadeye report record posted`

## Output

Lead with a one-line header, then the four lens sections, then a verdict:

```
PR #<N> "<title>"  +<adds>/-<dels>, <files> files
```

End with the tally and the verdict — `<C> critical, <H> high, <M> medium, <N>
nits` and the one `critical` that must ship fixed — or, when nothing
survived verification, exactly: `Clean — nothing survived verification.
Ship it.`

Findings are a LIST. Do not apply or push any code change unless asked.

## Suggested fixes

For each finding whose fix is concrete and mechanical — not a judgment call
("which auth policy is correct," "what should this business rule be") —
add the replacement as a fenced code block right after the finding line:
minimal, just the changed lines plus a line or two of context, language-tagged.
Skip the snippet and keep the prose `Fix:` alone when the right fix genuinely
needs a human decision. Same proof discipline as everywhere else in this
rubric: never fabricate a plausible-looking snippet for a fix you're not
sure of.

When posting (see "Posting back to the PR" below), that snippet becomes the
comment's fix content as a `` ```suggestion `` block instead of a plain
fenced one, anchored to the exact lines the diff shows — GitHub renders a
one-click "Apply suggestion" button, the fastest path from finding to fix.
A suggestion block can only replace lines already in the diff; if the fix
reaches outside them, post the plain snippet and prose fix instead — GitHub
rejects a suggestion that doesn't fit the anchored range.

## Copy for AI

After the tally, print one more block: every finding that survived,
worst-severity first, as a self-contained task list a coding agent could
run directly from — no PR context needed, just this block pasted into a
prompt. One entry per finding: `path:line — <tag>: <what>. Fix: <the
snippet if you have one, else the prose fix>.` Wrap the whole list in a
single fenced block so it copies in one motion. Skip this section entirely
when nothing survived verification — an empty task list helps no one.

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
  - `comments`: one entry per finding, `{path, line, side, body}` —
    `side: "RIGHT"` for an added/context line, or `side: "LEFT"` with the
    ORIGINAL file's line number for a finding on a deleted line (a removed
    guard, a dropped `ok &&`) — the body being the finding line (severity,
    tag, fix, proof).
  Get `{owner}/{repo}` from `gh repo view --json nameWithOwner`. Anchor `line`
  to a line the diff actually touches, or GitHub rejects the comment.
- `event: "COMMENT"` only — never approve, request-changes, merge, or close.
