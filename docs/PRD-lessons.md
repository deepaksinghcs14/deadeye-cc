# PRD — Cross-surface learning loop (lessons v2)

Status: draft, pre-implementation. Companion to `docs/PLAN.md`; write findings
from any live verification into `docs/verified.md` per that document's
discipline before building on them.

## 1. Problem

The learning loop (`internal/lessons`, PLAN.md §8) exists today but is
narrow on every axis:

- **One signal.** `escalation` is the only outcome kind ever recorded: the
  caller requests a higher-tier model than deadeye last recommended for a
  matching task shape.
- **One consumer.** The only place an outcome is read back is
  `lessons.AdjustedDownshiftThreshold`, called from one site
  (`decide.go:384`) to raise the routing confidence bar for a task shape.
- **One scope.** `outcomes.jsonl` is global under `~/.deadeye/`, keyed by a
  routing-specific shape (`files=…,impl=…,tests=…`) that means nothing to
  the other two surfaces.
- **No self-correction for coder mode or PR review.** Both exist and both
  produce misses — proven by `internal/coder/size_test.go`'s own comment
  trail: three lessons from a real SSRF-review miss were added to
  `ruleset.md` **by hand**, by the maintainer, after the fact, permanently,
  competing for a hard 9.35KB injection-size ceiling. There is no mechanism
  by which deadeye learns from its own review misses without a human
  patching a markdown file.
- **Revert/test-fail detection remains genuinely unbuilt** (PLAN.md §15,
  §10.10-ish territory) — no hook surface currently reports whether a
  downshifted task's code later got reverted or failed CI. Not solved here;
  scoped out explicitly (§4).

The insight this PRD is built on: **deadeye already runs three review
surfaces in the same repo** (`/deadeye-guard`, `/deadeye-review`,
`/deadeye-pr`) that can catch what coder mode just shipped, in the same
session, without needing CI, a revert, or the user to say anything. That
signal is sitting unused. It's the cheapest, highest-density new outcome
kind available and this PRD leads with it.

## 2. Goal

Generalize `internal/lessons` from a single-purpose routing-threshold input
into a shared substrate that **routing, coder mode, and PR review** all
write to and read from — without breaking any of the nine hard invariants
in PLAN.md §2, most load-bearing here:

- **INV-1 / INV-9**: new signals only ever make the system more cautious;
  "no error observed" is never treated as a positive.
- **INV-4**: whatever coder mode injects at SessionStart stays byte-stable
  for the session and inside the existing ≤400-token preamble budget.
- **INV-5**: a broken lessons read/write never blocks a hook or a skill —
  fail open, same as everywhere else.
- **INV-6**: every recorded outcome is visible (`deadeye lessons`) and
  reversible (`deadeye lessons reset`).

## 3. Non-goals

- Cross-machine or team-shared lessons. Still one local file per machine,
  per the project's own "no hosted service, no telemetry" line.
- LLM-summarized lessons. Outcome recording stays deterministic and
  structured, same philosophy as escalation today — no paraphrasing a
  finding into prose at write time.
- Revert/test-fail detection for routing. Still blocked on an unavailable
  hook signal; stays on the "genuinely open" list.
- Changing `AdjustedDownshiftThreshold`'s math. Routing's existing
  smoothing/recency mechanism is untouched; it just becomes one of three
  consumers of a shared store instead of the only one.

## 4. New outcome kinds

| Kind | Surface | Detected how | New mechanism needed? |
|---|---|---|---|
| `escalation` (existing) | routing | caller requests higher tier than last recommendation, same session | none |
| `coder-miss` | coder | `/deadeye-guard`, `/deadeye-review`, or `/deadeye-pr` finds a real issue in a file git-modified this session while coder mode was active | yes — see §6 |
| `review-false-positive` | pr-review | user explicitly disputes a finding during/after a review skill run | yes — see §6 |
| `revert` / `test-fail` | routing | — | **deferred**, no hook surface exists (unchanged from PLAN.md) |

`coder-miss` is the one worth building first: it needs no user cooperation,
no new hook, and no CI integration — it's two skills that already run in
this repo cross-referencing each other in the same session.

## 5. Storage changes

`lessons.Outcome` gains two fields, both additive and backward-compatible
(old rows without them read as their zero value, which must map to today's
behavior):

```go
type Outcome struct {
    TS        string
    SessionID string
    Surface   string  // NEW: "routing" | "coder" | "pr-review". Empty == "routing" (back-compat).
    TaskShape string  // renamed in doc only; format stays surface-specific
    Model     string
    Effort    string
    Kind      string  // "escalation" | "coder-miss" | "review-false-positive"
    Weight    float64
    Repo      string  // NEW: stable key for the repo this outcome came from; empty == global
}
```

`Repo` matters because `coder-miss` and `review-false-positive` are
pattern-specific to a codebase — an SSRF miss in project A says nothing
about project B — while `escalation`'s routing signal is arguably fine
staying global (task shape there is already codebase-agnostic: file count,
mechanical-vs-not, test presence). Key candidates for `Repo`: `git remote
get-url origin` if present, else the repo's absolute path. No new file;
stays one `outcomes.jsonl`, filtered at read time — consistent with the
project's "everything lives in one local file" line in the README/FAQ.

`Shape` vocabulary is surface-scoped, not universal:
- routing: unchanged (`files=…,impl=…,tests=…`)
- coder: the review lens + tag, e.g. `security:inject`, `correctness:race`,
  `correctness:leak` — same taxonomy already in `internal/prreview`'s rubric
  (`inject:`, `race:`, `leak:`, `logic:`, `untested:`, `a11y:`), so no new
  vocabulary to invent.
- pr-review: same lens/tag taxonomy, for false-positive suppression.

## 6. The hard part: skills need a way to write an outcome

`/deadeye-guard`, `/deadeye-review`, and `/deadeye-pr` are prose-driven
skills (Claude following markdown instructions), not Go hook code — nothing
in the binary observes "this skill just produced N findings." Recording an
outcome has to be an explicit step the skill is instructed to take, via a
new CLI subcommand in the same family as the existing `deadeye capture` /
`deadeye notes-append` write-back commands:

```
deadeye lessons record --surface coder --kind coder-miss \
  --shape security:inject --repo <key>
```

Skill-side logic, added to `/deadeye-guard` and `/deadeye-review`'s
existing "Verify before reporting" pass: for each **confirmed** finding
(post-verification, not a raw candidate), check whether the touched file
was git-modified this session; if the modifying commits fall inside a
window where coder mode was active (`coder-mode` state file already tracks
this per PLAN.md/`meta.CoderModePath`), call `deadeye lessons record`. One
call per confirmed finding, not per candidate — false candidates the
skill's own adversarial verify pass already rejects must never become a
`coder-miss` outcome (that would teach coder mode from the reviewer's own
mistakes, backwards).

`review-false-positive` is symmetric: when a user's follow-up message
disputes a finding the skill just reported ("that's not a bug", "already
handled", "won't fix"), the skill calls
`deadeye lessons record --surface pr-review --kind review-false-positive --shape <lens:tag>`.

Open question to verify before building this (mirrors PLAN.md §10's
discipline): **will a skill reliably make this call every time**, or does
it silently drop under context pressure / compaction / a terse response?
Needs a live-verification pass, same as the hook contract got in
`docs/verified.md`, before Phase B is trusted.

## 7. Consumption per surface

**Routing** — unchanged. Still just `escalation` → `AdjustedDownshiftThreshold`.
Cross-surface outcomes don't obviously map onto a numeric confidence bar;
not forcing a fit.

**Coder mode (SessionStart injection)** — a new, small, separately-budgeted
section appended after the static ruleset: the top 2–3 `coder-miss` shapes
for this repo, by recency-weighted frequency (reuse the existing
Beta-smoothing + 30-day recency shape from `AdjustedDownshiftThreshold`,
don't invent new math), rendered as one line each, e.g.:

```
Recent misses in this repo: security:inject (2×), correctness:leak (1×) —
recheck these before calling a change done.
```

This has to fit inside **INV-4's existing ≤400-token total preamble
budget**, not the ruleset's own separate 9.35KB ceiling — it's dynamic
content, added to `internal/coder`'s injection function, with its own size
test the same shape as `TestInjectionSizeBudget`, so growth here is as
visible and deliberate as every prior ruleset addition already is.

**PR review** — the skill's existing "Don't repeat what's already on the
PR" pass (which already fetches prior comments/reviews to dedupe) gains a
second read: recent `review-false-positive` shapes for this repo lower
that lens/tag's priority (not silence it — see §8), and recent `coder-miss`
shapes raise it, mirroring how the project's own held-out benchmark already
names concurrency races as the weak spot worth over-weighting.

## 8. Safety properties (must hold before shipping)

- A `review-false-positive` outcome **never fully disables** a lens/tag —
  same INV-1/INV-9 posture as escalation: it only ever raises the bar for
  reporting that category again, decaying over the same 30-day window,
  never a permanent kill. A user dismissing one real finding to avoid
  friction must not silently blind that check forever.
- A `coder-miss` outcome only ever adds a *reminder line*, never a new hard
  rule enforced in code — coder mode's own philosophy (`ruleset.md`: "no
  scaffolding for later," "boring over clever") argues directly against an
  unboundedly growing checklist; capping the injected list at 2–3 entries
  and decaying old ones out is the check against that.
- Fail open: `lessons.Scan`/`Append` errors already return nil / are
  ignored at call sites (`lessons.go:105`, `.116`) — new call sites follow
  the identical pattern, no new panic surface.

## 9. Phases

- **Phase A — storage + visibility.** Add `Surface`/`Repo` fields,
  backward-compat default. Extend `deadeye lessons` to group by surface;
  extend `deadeye lessons reset` with an optional `--surface` filter.
  Routing behavior is a no-op change (same outcomes, same consumer).
- **Phase B — coder-miss.** `deadeye lessons record` CLI; wire
  `/deadeye-guard` and `/deadeye-review`'s verify pass to call it on
  confirmed findings against coder-mode-authored code; add the capped
  "recent misses" section to coder's SessionStart injection, with its own
  size-budget test.
- **Phase C — review-false-positive.** Wire skill-side dispute detection;
  wire the PR-review skill's dedupe pass to weight lens/tag priority by
  recent false-positive/coder-miss history for the repo.
- **Phase D — deferred.** Revert/test-fail detection for routing. Stays
  blocked on hook-surface availability; re-evaluate if Claude Code ever
  exposes a Stop-time outcome signal.

## 10. Open questions to verify before Phase B starts

1. Do skills reliably invoke a write-back CLI call every time a finding is
   confirmed, under real context/compaction pressure? (live-verify, like
   PLAN.md §10)
2. `Repo` key: git remote URL vs. absolute path vs. staying global for v1 —
   which one actually distinguishes projects without leaking a path into a
   log a user might share for debugging?
3. Does injecting "recent misses" measurably reduce repeat findings on the
   same repo, or just add noise coder mode's own ladder already covers?
   Needs a before/after check, not just a shipped feature.
4. Does false-positive suppression regress the existing 24-PR held-out
   review benchmark (61% recall / 97% precision)? Re-run it before/after
   Phase C ships — this is the one number in the README that must not get
   worse quietly.

## 11. Success criteria

- `deadeye lessons` shows outcomes from all three surfaces, groupable and
  resettable independently.
- A `coder-miss` recorded in session N measurably shows up as a reminder
  line in session N+1's coder injection for the same repo, within the
  ≤400-token INV-4 budget.
- The held-out PR-review benchmark (precision, specifically) does not
  regress after Phase C.
- No new hard rule ever gets silently baked into `ruleset.md` from a
  `coder-miss` — the only auto-write path is the capped reminder section;
  promoting a repeated miss into a permanent rule stays a human decision,
  same as the SSRF lessons were.
