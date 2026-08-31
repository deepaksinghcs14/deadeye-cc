# Routing-savings benchmark -- results

Each task ran on all three tiers in an isolated clean tree, graded by a hidden test the model never saw. Cost is real billed `total_cost_usd`. Deadeye was disabled during runs so this measures raw per-tier model cost.

## Per-task: cost (pass/fail) by tier

| Task | Band | haiku | sonnet | opus | Cheapest that passed |
|---|---|---|---|---|---|
| h4-semver | hard | $0.055 PASS | $0.279 PASS | $0.265 PASS | **haiku** |
| h5-expr | hard | $0.050 FAIL | $0.294 FAIL | $0.407 FAIL | **none** |
| h6-counter | hard | $0.034 PASS | $0.186 PASS | $0.311 PASS | **haiku** |
| m1-clamp | mechanical | $0.026 PASS | $0.110 PASS | $0.160 PASS | **haiku** |
| s2-wordwrap | standard | $0.046 PASS | $0.230 PASS | $0.331 PASS | **haiku** |
| s3-csv | standard | $0.060 PASS | $0.329 PASS | $0.204 PASS | **haiku** |

## Pass rate by band x tier

| Band | haiku | sonnet | opus |
|---|---|---|---|
| mechanical | 1/1 | 1/1 | 1/1 |
| standard | 2/2 | 2/2 | 2/2 |
| hard | 2/3 | 2/3 | 2/3 |

## Cost roll-up (oracle routing = cheapest tier that passed)

- Baseline (every task on opus): **$1.678**
- Routed (cheapest passing tier per task): **$0.629**
- Saved: **$1.049**  (**63%**)

## Honesty notes

- 5/6 tasks were solvable below opus (where the saving is real -- the cheaper tier actually passed).
- 1 task(s) failed on every tier in this SINGLE-TRIAL run: h5-expr. A single trial is noisy near a model's capability frontier -- do not read this as 'no tier can do it' (e.g. opus passed h5-expr on a manual re-run). It is charged at opus in both arms, so it claims no savings -- routing correctly keeps a frontier task on opus. Multi-trial pass-rates are needed to place these precisely.
- Savings above are the ORACLE ceiling (perfect tier choice). deadeye's router approximates this from cheap signals; a follow-up should measure router-vs-oracle agreement. Cache-heavy Claude Code system-prompt cost is included in every run and is similar across tiers, so it dilutes the headline %; the model-priced delta is the real lever.
