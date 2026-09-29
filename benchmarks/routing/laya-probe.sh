#!/usr/bin/env bash
# Laya arm of the routing benchmark.
#
# router.sh measures deadeye's real router (six signals, then the `claude -p`
# judge on unsure cases). This script measures what the OPTIONAL local Laya
# classifier would have picked for the same work, so the two can be compared
# against the same ground truth -- run.sh's per-tier pass/cost grid, where a
# "pass" means the tier actually completed the task against a hidden test it
# never saw.
#
# Two arms, and the second is the one that matters:
#
#   benchmark tasks -- the 6 tasks in tasks/. All of them are self-contained,
#     fully-specified work that SHOULD route to tier 0, so scoring well here
#     proves almost nothing: a classifier that has collapsed to "always 0"
#     scores perfectly. Joined against results.jsonl by summarize.py to get a
#     realized-savings and did-it-actually-pass number.
#
#   discrimination probes -- the same list judge-probe.sh uses, spanning all
#     three tiers. This is where a classifier that under-rates hard work shows
#     up, which is the failure mode that would matter: routing genuinely hard
#     tasks to a cheap model is the expensive direction to be wrong in.
#
# Free to run: every call is local to laya-serve, no `claude -p`, no tokens.
# It measures latency per call, so it doubles as the latency arm.
#
# Requires a running laya-serve and mode.laya set to anything but off (the
# classify command reads laya.endpoint, not the ladder rung -- the rung
# governs whether PRODUCTION acts on answers, which a benchmark doesn't).
#
# Usage: ./laya-probe.sh [trials]      (default 3)
set -uo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
TRIALS="${1:-3}"
OUT="$HERE/results/laya.jsonl"

BIN="$(mktemp -d)/deadeye"
echo ">> building from current tree ..." >&2
( cd "$REPO" && go build -o "$BIN" ./cmd/deadeye ) || { echo "build failed" >&2; exit 1; }
echo "   $("$BIN" version)" >&2

if ! "$BIN" laya health >/dev/null 2>&1; then
  echo "laya-serve is not answering -- start it and set laya.endpoint first" >&2
  exit 1
fi
mkdir -p "$HERE/results"
: > "$OUT"

# Warm the checkpoint before timing anything. A cold load costs seconds and
# would land entirely in whichever task happened to run first, making that
# one task look pathological and every latency percentile meaningless.
"$BIN" laya classify --json "warmup" >/dev/null 2>&1

emit() { # kind id trial want json
  python3 -c '
import json,sys
kind,tid,trial,want,raw = sys.argv[1:6]
try:
    d = json.loads(raw)
except Exception:
    d = {"ok": False}
row = {"kind": kind, "id": tid, "trial": int(trial), "want": want,
       "ok": bool(d.get("ok")), "tier": d.get("tier"), "certainty": d.get("certainty"),
       "checkpoint": d.get("checkpoint"), "ms": d.get("ms")}
print(json.dumps(row))' "$1" "$2" "$3" "$4" "$5" >> "$OUT"
}

echo >&2
echo ">> benchmark tasks (all expected tier 0 -- see header) ..." >&2
for d in "$HERE"/tasks/*/; do
  id="$(basename "$d")"
  [ -f "$d/prompt.txt" ] || continue
  prompt="$(cat "$d/prompt.txt")"
  for t in $(seq 1 "$TRIALS"); do
    emit task "$id" "$t" "0" "$("$BIN" laya classify --json "$prompt" 2>/dev/null)"
  done
  last=$(python3 -c '
import json,sys
rows=[json.loads(l) for l in open(sys.argv[1]) if l.strip()]
m=[r for r in rows if r["id"]==sys.argv[2]]
print(",".join(str(r["tier"]) for r in m))' "$OUT" "$id")
  printf '  %-14s tiers: %s\n' "$id" "$last" >&2
done

echo >&2
echo ">> discrimination probes (must NOT all be tier 0) ..." >&2
# Same probes judge-probe.sh uses, so the two arms are directly comparable.
# want|label|prompt
probes=(
  "0|trivial rename|Rename the variable foo to bar in internal/utils/utils.go"
  "0|specified single file|Create a new Go package at internal/queue/queue.go with func Reverse(xs []int) []int returning a reversed copy. Add a one-line package doc comment."
  "1|cross-file refactor|Refactor the authentication middleware across the api/ package to use the new session store, update every call site, and keep backwards compatibility with existing tokens"
  "1|underspecified integration|Integrate our billing service with the existing Stripe webhook handler; requirements are not fully specified, infer them from the current code"
  "2|subtle debugging|Users report intermittent double-charging under concurrent retries in the payment reconciliation job. Find and fix the root cause."
  "2|security-critical|Audit the OAuth token refresh flow for privilege escalation and fix any vulnerability you find"
  "2|architecture|Design the sharding and consistency model for our multi-region write path"
)
for p in "${probes[@]}"; do
  IFS='|' read -r want label prompt <<<"$p"
  for t in $(seq 1 "$TRIALS"); do
    emit probe "$label" "$t" "$want" "$("$BIN" laya classify --json "$prompt" 2>/dev/null)"
  done
  got=$(python3 -c '
import json,sys
rows=[json.loads(l) for l in open(sys.argv[1]) if l.strip()]
m=[r for r in rows if r["kind"]=="probe" and r["id"]==sys.argv[2]]
print(",".join(str(r["tier"]) for r in m))' "$OUT" "$label")
  printf '  want %s  %-28s got: %s\n' "$want" "$label" "$got" >&2
done

echo >&2
echo "wrote $(wc -l < "$OUT" | tr -d ' ') rows to results/laya.jsonl" >&2
echo "run ./summarize.py for the joined report" >&2
