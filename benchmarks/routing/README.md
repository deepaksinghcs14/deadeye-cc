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
python3 summarize.py  # -> results/summary.md
```

Requires `claude` on PATH, `go`, and `python3`. Each run spends real tokens.

## Pilot findings (6 tasks, single trial per tier)

See `results/summary.md` for the generated tables. What the numbers actually say:

- **Over-provisioning is expensive.** For the *same* task, done correctly (hidden
  test passed), opus billed **3.4-9.1x** haiku (median ~6x) and sonnet ~4-5.5x
  haiku. That ratio is a per-task measured fact, independent of task mix.
- **Well-scoped subtasks rarely need the top tier.** 5 of 6 tasks — including
  SemVer precedence with the numeric-vs-lexical trap and a concurrent counter
  under `-race` — passed on **haiku**. Subagent work is usually well-scoped, so
  this is the common case, and downshifting it is nearly free in quality.
- **The frontier is real.** `h5-expr` (a full recursive expression evaluator with
  precedence, unary minus, and error handling) failed on haiku and sonnet, and
  opus was borderline — it failed one trial and passed on re-run. This is exactly
  the task routing must send *up*; the roll-up keeps it on opus and claims no
  saving for it.
- **Illustrative roll-up:** oracle routing (cheapest tier that passed) cut model
  cost **~63%** vs all-opus on this set — driven by downshifting the 5 well-scoped
  tasks while keeping the frontier task on opus. Treat this % as illustrative:
  it depends on the task mix and on single-trial noise. The **cost ratio** above
  is the robust, mix-independent claim.

**Known limits of the pilot:** single trial per (task, tier) is noisy at the
frontier (see `h5-expr`); tasks are small and self-contained, so they under-count
the tier gap that shows up on large, context-heavy, multi-file real work; and the
% is mix-dependent. Multi-trial pass-rates and larger tasks are the next rigor
step.

## Honesty boundaries (load-bearing)

- Tokens and dollars are **measured**, never estimated.
- Every tier's pass/fail is reported, including failures.
- No saving is claimed on a task the cheaper tier failed.
- The headline % is the **oracle ceiling** (perfect tier choice). deadeye's
  cheap-signal router approximates it; the gap between router and oracle is a
  separate measurement, not folded into this number.
- Claude Code's cache-heavy system-prompt cost is present in every run and is
  similar across tiers, so it *dilutes* the headline %. The model-priced delta
  is the real lever; a subagent-heavy real workload sees a larger effect than
  these single-shot tasks.
