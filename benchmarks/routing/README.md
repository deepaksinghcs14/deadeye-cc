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


## Laya arm (`laya-probe.sh`)

deadeye can optionally route through a local [Laya](https://github.com/NandhaKishorM/laya)
classifier instead of the `claude -p` judge (`mode.laya`). This arm measures
what that would pick, against the same ground truth and the same accounting as
the router arm — so the two numbers are directly comparable.

```bash
./laya-probe.sh 3        # needs a running laya-serve; free, no model spend
./summarize.py
```

It calls `deadeye laya classify`, the same code path production uses, rather
than re-stating the classifier's prompt in a shell script. A benchmark that
measures a slightly different prompt than the product sends is worse than no
benchmark, because it looks authoritative.

**Two arms, and the second is the one that matters.** All six benchmark tasks
are self-contained, fully-specified work that should route tier 0, so a
classifier collapsed to a single answer can score well on them. The
discrimination probes — the same list `judge-probe.sh` uses, spanning all three
tiers — are where that shows up.

**Judge cost is measured too** (`judge-cost.sh`), because the router arm
otherwise charges only task execution and treats classification as free. It
isn't: a `claude -p` judge call carries Claude Code's whole system prompt, so
it bills ~$0.05 per first-seen task, at ~2s of hook-blocking latency. Laya's
equivalent is $0.00 at ~71ms. Comparing the two without that line compares a
paid classifier to a free one on the paid one's terms.

**Two probe sets, and the second is not optional.** Tuning the classifier's
prompt against the discrimination probes and then reporting accuracy on those
same probes is circular. It is also not hypothetical: a criteria rewording
tried during 0.67.1 scored **1/9 -> 5/9** on the tuning probes and
**8/9 -> 6/9** on the held-out set — a regression wearing a win's clothes, and
it would have shipped without the second set. Tune against `probe`, report
against `heldout`.

**What it can and cannot establish.** It can report real cost and whether the
tier it picked actually passed, because both come from the pass/cost grid. It
cannot produce a trustworthy accuracy rate from six tasks and seven probes. The
direction of the error is the finding; the magnitude is not.


## Decomposition experiment (`corpus/`)

Laya answers deadeye's one 3-way "which tier" question badly: across a 66-case
labelled corpus it predicts tier 1 for most inputs, giving perfect tier-1
recall (22/22) and **tier-2 recall of 7/22**. Missing tier 2 means routing
security-critical and subtle-debugging work to a cheap model, which is the
expensive direction to be wrong in.

Asking six atomic yes/no questions in a single forward pass and combining them
with **fitted** weights does better:

```bash
./corpus/fit.py          # reads corpus/signals.jsonl, no model calls
```

| | baseline (1 question) | decomposed (6 + fitted) |
|---|---|---|
| accuracy, 30 folds | 63.2% (sd 11.6) | **71.2%** (sd 15.5) |
| tier 0 recall | 13/22 | 15/22 |
| tier 1 recall | 22/22 | 13/22 |
| tier 2 recall | 7/22 | **18/22** |

**The fitting is the point, not the decomposition.** An earlier attempt using
the same sub-answers with hand-set 0.5 thresholds was *worse* than the baseline
on held-out cases. Laya's probabilities sit in a compressed band — the server
warns at startup that this checkpoint ships invalid temperatures and its
confidence is uncalibrated — so arbitrary cutoffs carry far too much weight.
Two of the six questions (`spec`, `existing`) are close to noise on their own;
they earn their place only through the fitted combination.

**Why this is not shipped.** The corpus was written and labelled by one person
(see `corpus/tasks.jsonl`), so a classifier fitted on it may be learning that
person's phrasing rather than the underlying property — and no amount of
cross-validation detects that. n=66 against 21 fitted parameters is also thin,
and +8 points against a standard deviation of 11-15 is suggestive, not
conclusive. Before any of this reaches the product, the weights need refitting
on real traffic: `mode.laya=shadow` records exactly the `task -> actual tier`
pairs required, which is the second reason to leave shadow running.


### Dataset 2: the transfer test (`corpus/eval_wild.py`)

Dataset 1 above is synthetic — one person wrote both the task texts and the
labels — so a model fitted on it may be learning that person's phrasing rather
than the underlying property. Cross-validation cannot detect that. Dataset 2 is
the check: **70 real Agent tool prompts** harvested from Claude Code
transcripts across four projects, i.e. the actual production distribution,
written by nobody for this experiment. Two independent annotators labelled
them from the tier definitions alone.

```bash
./corpus/eval_wild.py
```

**Result: decomposition does not transfer.**

| | agreed cases (n=43) |
|---|---|
| baseline single-question | 23.3% |
| decomposed, fitted on dataset 1 | 27.9% |

Both are **below chance** (33% for three classes). Dataset 1's 71.2% was an
artifact of the corpus. Decomposition still beats the baseline and still wins
on tier 2 (3/4 vs 0/4), but beating a sub-chance baseline is not a result.

**The more important number is the annotator agreement: 43/70 (61%).** Two
annotators given identical definitions agreed on three-fifths of real tasks,
and disagreed *systematically* — one labelled almost everything lower. They
agreed perfectly on tier 0 and diverged on 1 vs 2.

That is a finding about the taxonomy, not about Laya. "Explore this repo and
report" is genuinely ambiguous between a lookup (tier 0) and spanning
unfamiliar code (tier 1), and the definitions do not settle it. 61% is
therefore the ceiling any classifier can honestly be scored against on this
distribution — and it raises a question upstream, because shadow mode scores
Laya against the `claude -p` judge, whose labels on these same boundaries may
be no less arbitrary.

Anyone tempted to ship a classifier on dataset-1 numbers should run this first.


### Does fine-tuning transfer? (`corpus/checkpoint_ab.py`)

Convai already ran the experiment we would be contemplating: `typed-decisions`
is `english` fine-tuned on ~2,000 decisions across four workflows, reported at
0.362 -> 0.766 on that domain. Running both checkpoints over our data answers,
for free, what would otherwise cost days.

| | english (base) | typed-decisions | delta |
|---|---|---|---|
| real prompts (n=43) | 27.9% | 23.3% | **-4.7** |
| synthetic corpus (n=66) | 57.6% | 63.6% | +6.1 |
| their domain (reported) | 36.2% | 76.6% | **+40.4** |

A large in-domain gain that inverts out of domain is evidence that fine-tuning
**specialises hard** — which is an argument *for* fine-tuning on our own
domain, not against it. It is also the only intervention tested anywhere in
this benchmark that has moved the number by more than a few points.

The constraint is unchanged and now sharper: their +40 came from four crisp
workflows (invoice, support, security incidents, agent traces) whose labels two
annotators would not fight about. Ours sit at 61% agreement. Fine-tuning on
labels that noisy teaches the noise, so **label quality gates this, not
compute or money.**

**Note on the shipped default.** `laya.checkpoint` defaults to
`typed-decisions`, argued from "every question deadeye asks is a typed
decision". That reasoning was never measured, and on real prompts `english`
edges it (27.9% vs 23.3%). The default is unchanged because n=43 and a
4.7-point gap is two cases with both below chance — but it is an argued
default, not a measured one, and should be revisited against any real
ground-truth set.
