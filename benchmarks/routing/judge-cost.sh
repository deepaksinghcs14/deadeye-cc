#!/usr/bin/env bash
# What the AI routing judge COSTS to run, per call.
#
# The router arm charges only task execution, which quietly treats
# classification as free. It isn't: every first-seen subtask spends a real
# `claude -p --model sonnet` call, and that call carries Claude Code's whole
# system prompt, not just the ~250-token judge prompt -- so it costs orders of
# magnitude more than the prompt's own token count suggests. Leaving it out
# compares a paid classifier against a free one on the paid one's terms.
#
# The Laya arm has no equivalent line because local inference genuinely costs
# nothing per call. That asymmetry is the whole point of measuring this.
#
# Sends the judge's real prompt (read out of judge.go, so it cannot drift) with
# --output-format json to get the billed figure back, one call per benchmark
# task. DEADEYE_JUDGE=1 keeps the nested session's own hooks from firing, same
# as production.
#
# Usage: ./judge-cost.sh          (costs ~$0.05 per task; 6 tasks ~= $0.30)
set -uo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
OUT="$HERE/results/judge-cost.jsonl"

HDR="$(sed -n '/^const judgePrompt/,/^Subtask: `$/p' "$REPO/cmd/deadeye/judge.go" \
       | sed '1s/^const judgePrompt = `//' | sed '$s/`$//')"
[ -n "$HDR" ] || { echo "could not read judgePrompt from judge.go" >&2; exit 1; }

mkdir -p "$HERE/results"
: > "$OUT"
echo ">> measuring judge cost, one call per task (real spend) ..." >&2

for d in "$HERE"/tasks/*/; do
  id="$(basename "$d")"
  [ -f "$d/prompt.txt" ] || continue
  (
    printf '%s%s' "$HDR" "$(cat "$d/prompt.txt")" \
      | DEADEYE_JUDGE=1 claude -p --model sonnet --output-format json 2>/dev/null \
      | python3 -c "
import json,sys
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
print(json.dumps({'task': '$id', 'cost_usd': d.get('total_cost_usd'),
                  'ms': d.get('duration_ms'),
                  'tier': (d.get('result') or '').strip()[:1]}))" >> "$OUT"
  ) &
done
wait

python3 - "$OUT" <<'PY'
import json, sys
rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
c = [r["cost_usd"] for r in rows if r.get("cost_usd")]
m = [r["ms"] for r in rows if r.get("ms")]
if not c:
    print("no judge calls measured", file=sys.stderr); raise SystemExit(1)
print(f"  {len(c)} calls: mean ${sum(c)/len(c):.5f}/call, total ${sum(c):.4f}", file=sys.stderr)
print(f"  latency: mean {sum(m)/len(m):.0f}ms, max {max(m)}ms  (this blocks the hook)", file=sys.stderr)
PY
echo "wrote results/judge-cost.jsonl -- rerun ./summarize.py" >&2
