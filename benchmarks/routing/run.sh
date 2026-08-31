#!/usr/bin/env bash
# Routing-savings benchmark. For each task x tier: isolate a clean source tree,
# run a real headless coding session on that model, grade with a HIDDEN test the
# model never saw, and record real billed cost + tokens. Deadeye is disabled
# during runs (DEADEYE=off) so we measure raw per-tier model cost, not
# deadeye-injected behavior. Permission mode acceptEdits: file edits are
# auto-approved, everything else auto-denied (no interactive hang).
#
# Usage:
#   ./run.sh            # full sweep (all tasks x all tiers)
#   ./run.sh m1-clamp   # single task, all tiers (validation)
set -uo pipefail
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
BR="$REPO/benchmarks/routing"
ONLY="${1:-}"
ONLYTIER="${2:-}"
OUT="$BR/results/results.jsonl"
[ -n "$ONLY" ] || : > "$OUT"

# task: id band pkg [race]
tasks=(
  "m1-clamp mechanical internal/mathutil"
  "s2-wordwrap standard internal/wordwrap"
  "s3-csv standard internal/csvx"
  "h4-semver hard internal/semver"
  "h5-expr hard internal/expr"
  "h6-counter hard internal/counter -race"
)
tiers=("haiku 0" "sonnet 1" "opus 2")

for t in "${tasks[@]}"; do
  read -r id band pkg race <<<"$t"
  [ -z "$ONLY" ] || [ "$ONLY" = "$id" ] || continue
  prompt="$(cat "$BR/tasks/$id/prompt.txt")"
  for tr in "${tiers[@]}"; do
    read -r tier idx <<<"$tr"
    [ -z "$ONLYTIER" ] || [ "$ONLYTIER" = "$tier" ] || continue
    work="$(mktemp -d)"
    git -C "$REPO" archive HEAD | tar -x -C "$work"
    echo ">> $id / $tier ..." >&2
    start=$(date +%s)
    ( cd "$work" && DEADEYE=off claude -p --permission-mode acceptEdits \
        --model "$tier" --output-format json "$prompt" ) > "$work/run.json" 2> "$work/claude.err"
    end=$(date +%s)
    verdict="$( cd "$work" && HIDDEN="$BR/tasks/$id/hidden" bash "$BR/check.sh" "$pkg" "$race" )"
    dur=$((end-start))
    python3 - "$id" "$band" "$tier" "$idx" "$verdict" "$dur" "$work/run.json" >> "$OUT" <<'PY'
import json,sys
id,band,tier,idx,verdict,dur,runpath=sys.argv[1:8]
try: d=json.load(open(runpath))
except Exception: d={}
u=d.get("usage",{}) or {}
rec=dict(task=id,band=band,tier=tier,tier_idx=int(idx),
         pass_=verdict.strip().startswith("PASS"),verdict=verdict.strip(),
         cost_usd=d.get("total_cost_usd"),
         in_tokens=u.get("input_tokens"),out_tokens=u.get("output_tokens"),
         cache_read=u.get("cache_read_input_tokens"),cache_creation=u.get("cache_creation_input_tokens"),
         num_turns=d.get("num_turns"),is_error=d.get("is_error"),dur_s=int(dur),
         models=list((d.get("modelUsage") or {}).keys()))
print(json.dumps(rec))
PY
    cost=$(python3 -c "import json;print(round(json.load(open('$work/run.json')).get('total_cost_usd') or 0,4))" 2>/dev/null)
    echo "   -> $verdict  \$$cost  ${dur}s" >&2
    rm -rf "$work"
  done
done
echo "DONE -> $OUT" >&2
