# Routing-savings benchmark

deadeye routes each subagent task to the cheapest model tier that can actually
do it, instead of letting every subtask inherit the parent's (usually top-tier)
model. This benchmark measures what that's worth in **real billed dollars**, and
— just as importantly — whether the cheaper tier actually *did the task*.

## The claim it earns

> On a representative task set, routing to the cheapest tier that passes cuts
> model cost by **N%** versus everything-on-opus — and the cheaper tier passed
> its hidden test on **M** of the tasks, so the saving is real, not a re-run in
> disguise.

Both halves matter. A "saving" from routing a task to a tier that then *fails*
is fake — you re-run on a stronger model and pay twice. So a saving is only
counted on a task where the cheaper tier **passed a hidden test it never saw.**

## Method

For every task × tier (`haiku`, `sonnet`, `opus`):

1. **Isolate.** `git archive HEAD` into a fresh temp tree — each run starts from
   a pristine checkout, no cross-contamination.
2. **Run for real.** `claude -p --model <tier> --output-format json` executes a
   full headless coding session on that model. `DEADEYE=off` during the run, so
   we measure raw per-tier model cost, not deadeye-injected behavior.
   `--permission-mode acceptEdits` auto-approves file edits (everything else is
   auto-denied, so nothing hangs).
3. **Grade blind.** A **hidden** `*_test.go` — never included in the prompt — is
   dropped into the package and run with `go test` (`-race` for the concurrency
   tasks). The model is graded against a spec it could not overfit to.
4. **Measure.** Real billed `total_cost_usd` and token counts come straight from
   the run's JSON. Nothing is estimated.

`summarize.py` then takes, per task, the **cheapest tier that passed** (the
oracle deadeye's router approximates) and compares the routed total to the
all-opus baseline.

## Task set (pilot: 6 tasks — 1 mechanical, 2 standard, 3 hard)

| Band | Task | What it probes |
|---|---|---|
| mechanical | `m1-clamp` | a trivial pure function — should pass on every tier (so opus is waste) |
| standard | `s2-wordwrap`, `s3-csv` | rune-vs-byte width counting and whitespace collapsing; a quote-aware RFC4180 CSV split with escaped quotes — where a weak tier starts to slip |
| hard | `h4-semver`, `h5-expr`, `h6-counter` | full SemVer 2.0.0 precedence rules; a recursive-descent arithmetic expression evaluator; a concurrency-safe counter that must pass `-race` — where reasoning separates tiers |

Tasks are self-contained by design so grading is robust; they can be swapped for
repo-specific ones in a larger run.

## Reproduce

```sh
./run.sh            # full sweep -> results/results.jsonl
./run.sh m1-clamp   # single task, all tiers (append; good for validation)
./router.sh         # what deadeye's router picks -> results/router.jsonl
./judge-probe.sh    # guards the judge against collapsing to one tier
python3 summarize.py  # -> results/summary.md
```

Requires `claude` on PATH, `go`, and `python3`. Each run spends real tokens.

## Two arms, and why the second one exists

`run.sh` measures each **tier**: what it costs and whether it passes. From that,
`summarize.py` derives the **oracle** — perfect hindsight, the cheapest tier that
actually passed. That is a *ceiling*: it says what routing is worth to someone
who already knows the answer, not what deadeye achieves.

`router.sh` closes that gap. It asks the real router (`deadeye route` — the same
`kernel.Decide` path a live Agent call takes, plus the AI judge on unsure cases)
what it would pick, without hindsight, then joins that against the per-tier grid
to report **agreement with the oracle** and **realized savings**. A wrong-cheap
route is charged the re-run on the next tier up, so a bad guess costs money here
rather than quietly scoring as a saving.

Quote the **realized** number for "what does deadeye save." The oracle is the
target it's aiming at.

It builds the binary from the current tree rather than using an installed one —
a stale `~/.deadeye/bin/deadeye` would silently measure an old router. Three
trials per task by default (`TRIALS=n` to change): `claude -p` exposes no
temperature or seed control, so a single sample can hide a judge that answers
the same prompt differently between runs.

`judge-probe.sh` is the counterweight to both. All six benchmark tasks are
self-contained, fully-specified work that *should* route to tier 0 — which
makes this set blind to the one failure mode that would ace it: a judge
collapsed to "always 0". The probe feeds in work that must **not** come back
tier 0 (a cross-file refactor, an under-specified integration, subtle
debugging, security-critical work, an architecture decision) and fails if any
of them route down. Run it after any change to the judge prompt; a
recalibration that buys a better benchmark number by giving up discrimination
gets caught there, not in production.

## Findings (6 tasks × 3 tiers, re-run 2026-09-06; router arm 5 trials/task)

See `results/summary.md` for the generated tables. What the numbers actually say:

- **Over-provisioning is expensive.** For the *same* task, done correctly (hidden
  test passed), opus billed **3.3–10.0x** haiku (median ~5.7x) and sonnet 2.0–6.6x
  haiku. That ratio is a per-task measured fact, independent of task mix.
- **Well-scoped subtasks rarely need the top tier.** 5 of 6 tasks — including
  SemVer precedence with the numeric-vs-lexical trap and a concurrent counter
  under `-race` — passed on **haiku**. Subagent work is usually well-scoped, so
  this is the common case, and downshifting it is nearly free in quality.
- **The frontier is real.** `h5-expr` (a recursive expression evaluator with
  precedence, unary minus, and error handling) failed on **all three tiers, in
  both full sweeps**. The hidden test was re-validated against an independently
  written correct implementation and passes, so those are genuine model failures,
  not a broken fixture. An earlier note claimed opus passed it on a manual
  re-run; two sweeps have since contradicted that, and it has been retired.
- **The oracle is not the product.** Oracle routing (perfect hindsight, cheapest
  tier that passed) cut model cost **66%** vs all-opus here. deadeye's actual
  router captures **48%** — 74% of the available ceiling, agreeing with the oracle
  on 5 of 6 tasks. Quote the realized number; the oracle is the target.
- **Recalibrating the judge was worth ~19 points.** The first router measurement
  scored 29% realized / 2-of-6 agreement, and flapped between tiers on 4 of 6
  tasks across trials. The cause was the judge prompt, not model noise: it called
  tier 1 "most real tasks" (a standing prior toward sonnet) and defined tier 0 by
  edit size, leaving no home for "write one self-contained function from a
  complete spec". It was grading difficulty by how advanced the *topic* sounded —
  sending SemVer precedence and a race-safe counter to sonnet while haiku passed
  both. Rewriting it around scope-and-specification took the router to 48%
  realized, 5-of-6 agreement, and the same tier on 5/5 trials for every task.

**Known limits:** 6 tasks is a small set and the % is mix-dependent — the cost
*ratio* is the robust claim. Tasks are small and self-contained, so they
under-count the tier gap on large, context-heavy, multi-file real work. The
judge has no temperature or seed control, so per-run stability must be
re-measured rather than assumed, and the 5/5 stability above is one sample.

**The 48% is tuned and graded on the same set — read it as optimistic.** The
judge prompt was rewritten *after* this benchmark showed haiku passing SemVer
and the concurrent counter, then scored on those same six tasks. Two things
survive that objection, and they're what the claim actually rests on: the fix
was directional rather than fitted (judge scope and specification instead of
subject matter — no task-specific wording went into the prompt), and
`judge-probe.sh` is held out from this set, so a classifier that bought score
by routing everything down would fail it. Neither makes 48% an unbiased
estimate. A fresh, unseen task set is the next rigor step, and the number
should be expected to come in lower there.

## Honesty boundaries (load-bearing)

- Tokens and dollars are **measured**, never estimated.
- Every tier's pass/fail is reported, including failures.
- No saving is claimed on a task the cheaper tier failed.
- The oracle % is a **ceiling** (perfect hindsight), never quoted as the
  product's result. `router.sh` measures what the real router achieves, and a
  wrong-cheap route is charged the re-run on the next tier up — a bad guess
  costs money in this arm rather than quietly scoring as a saving.
- The judge is recalibrated against **measured ground truth**, never tuned until
  the number looks good. `judge-probe.sh` exists so a recalibration can't buy
  benchmark score by giving up discrimination.
- Claude Code's cache-heavy system-prompt cost is present in every run and is
  similar across tiers, so it *dilutes* the headline %. The model-priced delta
  is the real lever; a subagent-heavy real workload sees a larger effect than
  these single-shot tasks.
