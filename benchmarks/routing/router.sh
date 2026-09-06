#!/usr/bin/env bash
# Router-vs-oracle arm of the routing benchmark.
#
# run.sh measures what each TIER costs and whether it passes -- from which
# summarize.py derives the ORACLE (perfect hindsight: cheapest tier that
# actually passed). That's a ceiling, not a result: it says what routing is
# worth, not what deadeye's router achieves.
#
# This script closes that gap. For each task it asks the REAL router what it
# would pick -- `deadeye route`, the same code path a live Agent call takes
# (kernel.Decide over the six signal providers, then the AI judge on the
# unsure cases) -- and records the decision. summarize.py joins it against
# run.sh's per-tier cost/pass grid to report agreement and REALIZED savings.
#
# Builds the binary from the current tree rather than using an installed one:
# the number that matters is what THIS revision's router does, and a stale
# ~/.deadeye/bin/deadeye would silently measure an old one.
#
# Note: with mode.routing_judge=on (the default), each call spends a small
# real `claude -p` classification -- that IS the router being measured, not
# overhead to strip out. Runs 3 trials per task so a judge that answers
# inconsistently, or fail-opens to the heuristic, shows up as disagreement
# instead of hiding behind a single lucky sample.
#
# Usage:
#   ./router.sh            # all tasks, 3 trials each -> results/router.jsonl
#   ./router.sh m1-clamp   # single task (validation)
set -uo pipefail
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
BR="$REPO/benchmarks/routing"
ONLY="${1:-}"
TRIALS="${TRIALS:-3}"
OUT="$BR/results/router.jsonl"
BIN="$(mktemp -d)/deadeye"

tasks=(m1-clamp s2-wordwrap s3-csv h4-semver h5-expr h6-counter)

echo ">> building router from current tree ..." >&2
( cd "$REPO" && go build -o "$BIN" ./cmd/deadeye ) || { echo "build failed" >&2; exit 1; }
echo "   $("$BIN" version)" >&2

[ -n "$ONLY" ] || : > "$OUT"

for id in "${tasks[@]}"; do
  [ -z "$ONLY" ] || [ "$ONLY" = "$id" ] || continue
  prompt="$(cat "$BR/tasks/$id/prompt.txt")"
  for trial in $(seq 1 "$TRIALS"); do
    # ONE invocation per trial: model and reason must come from the SAME
    # decision. (Calling route twice and scraping one field from each gave a
    # contradictory model/reason pair the first time this was measured --
    # the judge can fail-open to the heuristic between two calls.)
    out="$("$BIN" route "$prompt" 2>/dev/null)"
    model="$(printf '%s' "$out" | awk '/^[[:space:]]+model:/{print $2; exit}')"
    reason="$(printf '%s' "$out" | sed -n 's/^[[:space:]]*reason:[[:space:]]*//p' | head -1)"
    python3 - "$id" "$trial" "$model" "$reason" >> "$OUT" <<'PY'
import json, sys
task, trial, model, reason = sys.argv[1:5]
# family -> tier index, mirroring the builtin catalog's ordering.
tier = {"haiku": 0, "sonnet": 1, "opus": 2, "fable": 3}
fam = next((f for f in tier if f in model), None)
print(json.dumps(dict(task=task, trial=int(trial), model=model,
                      tier=fam, tier_idx=tier.get(fam),
                      judge=("judge" in reason.lower()), reason=reason)))
PY
    echo "   $id trial$trial -> ${model:-<none>}" >&2
  done
done
echo "DONE -> $OUT" >&2
