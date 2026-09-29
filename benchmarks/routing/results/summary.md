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

## Classification cost (what it costs to DECIDE, not to execute)

Measured over 6 real judge calls (`judge-cost.sh`): **$0.05081 per call**, mean latency **2082ms** (max 4260ms) -- and that latency blocks the hook, which is the only thing in deadeye that does.

Laya's per-call classification cost is **$0.00** (local inference), at a measured p50 of 71ms. That asymmetry is the case for the integration, and the router arm above omits it entirely.

## Laya arm (optional local classifier)

On the 6 benchmark tasks, a `mode.laya=authoritative` router would have spent **$1.4061** against **$1.7962** for all-opus (**21.7%** saved), and the tier it picked passed its hidden test on **5/6**. Same accounting as the router arm above: a wrong-cheap route pays for the re-run.

| task | laya | passed? | realized cost |
|---|---|---|---|
| h4-semver | sonnet | yes | $0.1661 |
| h5-expr | sonnet | **NO** | $0.6457 |
| h6-counter | sonnet | yes | $0.2506 |
| m1-clamp | sonnet | yes | $0.0704 |
| s2-wordwrap | sonnet | yes | $0.1316 |
| s3-csv | sonnet | yes | $0.1417 |

### Discrimination probes

| probe | want | got | |
|---|---|---|---|
| architecture | 2 | 2 | ok |
| cross-file refactor | 1 | 1 | ok |
| security-critical | 2 | 1 | **miss** |
| specified single file | 0 | 1 | **miss** |
| subtle debugging | 2 | 2 | ok |
| trivial rename | 0 | 0 | ok |
| underspecified integration | 1 | 1 | ok |

**5/7** probes classified as expected.

### Held-out probes

Never used to tune anything. Report against these, not the set above.

| probe | want | got | |
|---|---|---|---|
| ORM migration | 1 | 1 | ok |
| b64 encoder from spec | 0 | 0 | ok |
| consensus design | 2 | 2 | ok |
| crypto review | 2 | 1 | **miss** |
| heisenbug | 2 | 2 | ok |
| new endpoint | 1 | 1 | ok |
| reverse a list | 0 | 0 | ok |
| table formatter | 0 | 0 | ok |
| vague improvement | 1 | 1 | ok |

**8/9** classified as expected -- markedly better than on the six benchmark tasks, which are a worst case for this classifier: fiddly-but-fully-specified single-file work is exactly the shape it over-rates.

Latency over 66 local calls (checkpoint already resident): p50 **71ms**, p95 **104ms**, min 67ms, max 126ms.

### Head to head, classification included

| arm | execute | classify | total | vs all-opus |
|---|---|---|---|---|
| deadeye router (judge) | $0.9310 | $0.3049 | **$1.2359** | 31.2% |
| laya authoritative | $1.4061 | $0.0000 | **$1.4061** | 21.7% |

Per task, Laya's worse routing costs **$0.0792** more to execute, while saving **$0.0508** in classification -- a net **$+0.0284** per task on this set. Break-even is a task mix where Laya mis-rates less often than it does on fully-specified work; these six are the worst case for it, being exactly the shape it over-rates.

- Every item picked the same tier on every trial. Laya is a single forward pass with no sampling, so this is expected rather than lucky -- it means a wrong answer is wrong the same way every time, not noise to average out.
- This is six tasks and seven probes. It shows a DIRECTION, not an accuracy rate. The grid's ground truth is real (hidden tests the model never saw), but no sample this size supports a percentage anyone should act on.
