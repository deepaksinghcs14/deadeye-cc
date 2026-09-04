---
name: deadeye-review
description: Four-lens self-review (over-engineering, correctness, performance, security) of the working diff, or the whole repo with --repo.
license: MIT
argument-hint: "[--repo]"
---

<!-- deadeye-review: canonical rubric; edit internal/prreview/review.md, the skill and every host rendering are generated from it -->
# Deadeye Review

Review code through four lenses — over-engineering, correctness,
performance, security — the same rubric `/deadeye-pr` runs on a whole pull
request, scoped instead to your working diff or the whole repo. This is the
local, pre-PR self-review: catch what would otherwise wait for a PR (or a
bot) to find. For a real GitHub PR, use `/deadeye-pr`. For a deeper,
dependency-audit-backed security-only pass, use `/deadeye-guard`.

Two scopes:

- **default** — the current working diff.
- **`--repo`** (or "audit the whole repo") — the entire repository, ranked
  worst-first across all four lenses. See "Whole-repo mode" below.

## Scope (default: the working diff)

Get the diff with `git diff` (or `git diff --staged` if the user says
staged, or `git diff <ref>` for a named base). Read the changed hunks
**plus enough surrounding context to judge a trust boundary or a caller
contract** — "is this input validated" and "does this break a caller" both
need the code around the hunk, not just the `+` lines.

- Empty diff (nothing changed or staged): say so plainly and stop — do
  not substitute a different scope.
- Not a git repo: ask the user which files to review.

Before tagging `yagni:`/`delete:`, or claiming an `authz`/nil/sanitizer
check is MISSING, grep for implementers/callers/guards OUTSIDE the diff —
an "interface with one impl" whose second impl lives in a test file, or a
guard that lives one call down, is a false positive. Report only what you
confirmed.

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
- The path is required — a diff can span files. If a sibling path shares the
  bug, name it in the same breath.
- `proof:` is required (see "Verify before reporting"). For `inject`/`authz`/
  `logic`/`race`, the proof IS a reproduction: the concrete input and the sink
  it reaches.
- Append `(confirmed)` when a tool or test backs the finding; otherwise it
  reads as `likely`. Direct, not rude — you're helping a peer ship.

### Over-engineering

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

### Security

- `inject:` — untrusted input reaches SQL, a shell, a template, a path, `eval`, a raw-HTML/DOM sink (XSS), or a deserializer
- `secret:` — a credential literal, or a secret handled where it can leak (logs, errors, client output)
- `authz:` — a decision or resource access with no confirmed permission check
- `crypto:` — hand-rolled or weak crypto (MD5/SHA1 for passwords, non-CSPRNG token, TLS verification off)
- `expose:` — sensitive data returned or logged beyond what the caller needs, on the NORMAL response path (an error path leaking a trace is `exceptions:`, not this)
- `dep:` — a vulnerable or superseded dependency
- `dos:` — untrusted input sizes an allocation, an unbounded loop, or unbounded recursion → memory or CPU exhaustion. Cap it, or bound the input first.
<!-- pentest-tags -->
- `ssrf:` — an attacker-controlled URL reaching a fetch: cloud metadata, internal network, a webhook or redirect-follow target
- `authn:` — absent/weak authentication: unverified JWT signature, `alg:none`, no expiry, session fixation, a weak reset/OTP flow
- `bizlogic:` — a business flow with no abuse control: TOCTOU on a balance/inventory value, a negative/overflow quantity, a skippable workflow step
- `massassign:` — a request body bound straight to a model, letting a client set `role`/`is_admin`/`balance`/`verified`
- `validation:` — absent/weak boundary validation: no schema, type confusion, unbounded size, a missing allow-list
- `ratelimit:` — no throttle/quota on login, OTP, reset, signup, or an expensive query — the ABSENCE of a limit, not the allocation shape (that's `dos:`)
- `config:` — misconfiguration: permissive CORS, missing security headers, insecure cookie flags, debug mode left on, default credentials
- `integrity:` — an unsigned/unverified update or plugin load, a CI/CD pipeline trusting unreviewed input, subdomain takeover — the SUPPLY-CHAIN/trust dimension; a deserializer that executes attacker-controlled code is `inject:`, not this
- `logging:` — an auth failure or privileged action with no audit trail
- `inventory:` — an undocumented or deprecated endpoint still routable (a live `/v1/` beside a `/v2/`, an orphaned route)
- `thirdparty:` — a third-party API response trusted without validation, or an unvalidated redirect to a partner service
- `exceptions:` — a mishandled exceptional condition: an uncaught exception leaking a stack trace or internal state, a caught error that fails open on a security-relevant path — the ERROR-path counterpart to `expose:`
- `llm:` — only when the diff touches an LLM/agent surface: prompt injection, system-prompt leakage, excessive agency, unbounded token/cost consumption
<!-- /pentest-tags -->

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

## Whole-repo mode (`--repo`)

Scan the whole repository through all four lenses and report a ranked
list — worst-first, most severe finding leads regardless of lens:

`<glyph> [path] — <tag>: <what actually happens, concretely>. Fix: <fix>.`

Same tags, same proof discipline as the diff mode above.

**Scope cheaply — token thrift is this plugin's whole point:**

1. Enumerate with `git ls-files` (or `find` with `-maxdepth` if not a git
   repo) — never by reading directories of files whole.
2. Grep-first for candidates before opening ANY file body:
   - over-engineering — duplicate deps (`go.mod`/`package.json` vs stdlib),
     `interface` declarations (then `grep -c` their implementers),
     one-export files, config keys (then grep for readers), wrapper-shaped
     names (`*Wrapper`, `*Manager`, `*Factory`, `*Helper`)
   - correctness/performance — unbounded loops, `O(n²)`-shaped nested
     iteration over slices/maps, resource-open calls (`Open`/`Dial`/`Begin`)
     without a nearby `Close`/`defer`, goroutines/threads touching shared
     state
   - security — raw SQL/shell/template/`eval` call sites, URL-fetch calls
     on user-controlled input, hand-rolled crypto (`md5`/`sha1` near
     "password"/"token"), a dependency manifest or lockfile that changed
     recently (`git log -1 --format=%ct go.sum package-lock.json`)
3. Read full file contents ONLY for the top candidates you intend to list —
   a sweep that reads the whole repo into context is the exact waste this
   plugin exists to prevent.

If a dependency manifest exists, run its native auditor when installed
(`govulncheck ./...`, `npm audit`, `pip-audit`, `cargo audit`, or
`osv-scanner -L <manifest>` if none is) rather than reading the dependency
tree into context — same discipline as `/deadeye-guard`'s dependency pass.

**Verify before reporting:** grep for ALL implementers/callers/guards across
the repo (including test files and other packages) — "interface with one
implementation" must mean one implementer exists, not one you happened to
find. Report only what you confirmed.

**Output discipline:** rank by severity first, then impact within a
severity. Cap at 20 findings — fewer exist → stop, never pad; more → keep
the 20 worst and say how many were omitted. This is a ranked sample of a
whole repo, not exhaustive coverage — never report partial coverage as
complete. Nothing found: exactly `Clean — nothing survived verification.`
If a replacement is itself a deliberate simplification with a known
ceiling, plant the marker line: `# deadeye: <shortcut>. ceiling: <limit>.
upgrade: <trigger>.` Skip vendored code, generated code, and lockfiles.

## Learning loop (repo-scoped priority)

Before finalizing, run `deadeye lessons priority` (best-effort — if
`deadeye` isn't on PATH, retry once with `~/.deadeye/bin/deadeye`; if that
also fails, review normally). It prints this repo's recent signal, if any:

- **Recent coder misses** — scrutinize those lens/tags harder; a shape that
  slipped through before is worth a second look.
- **Recently disputed findings** — need stronger `proof:` before reporting
  that lens/tag again. Never skip it outright: one dismissal doesn't retire
  a whole tag, it only raises the bar for the next one.

For each finding that survives verification and makes your final report
(never a candidate you dropped), record it so coder mode gets reminded next
session (best-effort, same retry-once contract as above):

```bash
deadeye lessons record coder-miss <lens>:<tag>
```

using the lens the finding came from (`over-engineering`, `correctness`,
`performance`, or `security`) and its tag without the trailing colon — e.g.
a `race:` finding → `deadeye lessons record coder-miss correctness:race`.
This is a no-op when coder mode wasn't active this session — nothing to
attribute, nothing gets written. Diff-scope only — `--repo` mode above scans
pre-existing code nothing here wrote this session, so it never attributes to
coder mode.

When the user disputes a finding you reported ("that's not a bug",
"already handled", "won't fix"), record it so the next review on this repo
weighs that lens/tag accordingly:

```bash
deadeye lessons record review-false-positive <lens>:<tag>
```

## Output

Lead with a one-line header, then the four lens sections, then a verdict:

```
<files> files, +<adds>/-<dels>
```

(`git diff --shortstat` gives you the numbers; omit the header entirely in
`--repo` mode, where the ranked list above is the output.)

End with the tally and the verdict — `<C> critical, <H> high, <M> medium,
<N> nits` and the one `critical` that must ship fixed — or, when nothing
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

## Copy for AI

After the tally, print one more block: every finding that survived,
worst-severity first, as a self-contained task list a coding agent could
run directly from — no PR context needed, just this block pasted into a
prompt. One entry per finding: `path:line — <tag>: <what>. Fix: <the
snippet if you have one, else the prose fix>.` Wrap the whole list in a
single fenced block so it copies in one motion. Skip this section entirely
when nothing survived verification — an empty task list helps no one.

## Boundaries

- Findings are a LIST. Do not apply them unless asked.
- Never flag the one runnable check coder mode leaves behind for
  deletion — lean code without its check is unfinished.
- Log spam is over-instrumentation, cut it (a line per loop, a metric nobody
  reads) — but never flag the one breadcrumb at a real failure boundary as
  bloat; a wrapped error or the log where it fails is load-bearing, like the
  runnable check.
- All four lenses are in scope here. `/deadeye-guard` is the deeper,
  dedicated security pass (native dependency auditors, a wider weakest-path
  sweep) for when security alone is the ask; `/deadeye-pr` is this same
  rubric run against a real GitHub PR.
