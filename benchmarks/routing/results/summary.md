# Routing-savings benchmark -- results

Each task ran on all three tiers in an isolated clean tree, graded by a hidden test the model never saw. Cost is real billed `total_cost_usd`. Deadeye was disabled during runs so this measures raw per-tier model cost.

## Per-task: cost (pass/fail) by tier

| Task | Band | haiku | sonnet | opus | Cheapest that passed |
|---|---|---|---|---|---|
| h4-semver | hard | $0.044 PASS | $0.166 PASS | $0.441 PASS | **haiku** |
| h5-expr | hard | $0.058 FAIL | $0.253 FAIL | $0.392 FAIL | **none** |
| h6-counter | hard | $0.038 PASS | $0.251 PASS | $0.251 FAIL | **haiku** |
| m1-clamp | mechanical | $0.035 PASS | $0.070 PASS | $0.118 PASS | **haiku** |
| s2-wordwrap | standard | $0.042 PASS | $0.132 PASS | $0.279 PASS | **haiku** |
| s3-csv | standard | $0.067 PASS | $0.142 PASS | $0.314 PASS | **haiku** |

## Pass rate by band x tier

| Band | haiku | sonnet | opus |
|---|---|---|---|
| mechanical | 1/1 | 1/1 | 1/1 |
| standard | 2/2 | 2/2 | 2/2 |
| hard | 2/3 | 2/3 | 1/3 |

## Cost roll-up (oracle routing = cheapest tier that passed)

- Baseline (every task on opus): **$1.796**
- Routed (cheapest passing tier per task): **$0.619**
- Saved: **$1.177**  (**66%**)

## Honesty notes

- 5/6 tasks were solvable below opus (where the saving is real -- the cheaper tier actually passed).
- 1 task(s) failed on EVERY tier: h5-expr. Charged at opus in the oracle arm, so it claims no savings. The hidden test itself was re-validated against an independently written correct implementation and passes, so these are genuine model failures, not a broken fixture. (An earlier single-trial run's note claimed opus passed h5-expr on a manual re-run; two full sweeps have since failed it on all three tiers, so that claim is retired rather than repeated.)
- Savings above are the ORACLE ceiling (perfect tier choice), not what deadeye achieves -- see the router arm below for the realized number. Cache-heavy Claude Code system-prompt cost is included in every run and is similar across tiers, so it dilutes the headline %; the model-priced delta is the real lever.

## Router arm -- what deadeye ACTUALLY picks (not the oracle)

`router.sh`, 5 trial(s) per task, judge fired on 30/30 (100%) of calls.

| Task | Oracle (cheapest that passed) | deadeye picked | Agree | Realized cost | Oracle cost |
|---|---|---|---|---|---|
| h4-semver | haiku | haiku | yes | $0.044 | $0.044 |
| h5-expr | none | haiku | NO | $0.704 (never passed) | $0.392 |
| h6-counter | haiku | haiku | yes | $0.038 | $0.038 |
| m1-clamp | haiku | haiku | yes | $0.035 | $0.035 |
| s2-wordwrap | haiku | haiku | yes | $0.042 | $0.042 |
| s3-csv | haiku | haiku | yes | $0.067 | $0.067 |

- Agreement with the oracle: **5/6**
- All-opus baseline: **$1.796**
- Oracle ceiling: **$0.619** (66% saved)
- **deadeye, realized: $0.931** (48% saved)
- Share of the available ceiling captured: **74%**

- Every task picked the same tier on every trial in this run. That is one sample, not a guarantee: `claude -p` exposes no temperature/seed control, so stability has to be re-measured, never assumed.
- The realized number is what to quote for "what does deadeye save". The oracle is the ceiling it's trying to reach.
