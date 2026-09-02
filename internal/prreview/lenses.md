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
