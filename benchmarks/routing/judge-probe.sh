#!/usr/bin/env bash
# Discrimination probe for the AI routing judge.
#
# router.sh measures the judge against the 6 benchmark tasks -- all of which
# are self-contained, fully-specified work that SHOULD route to tier 0. That
# makes it blind to the one failure mode that would score perfectly on it: a
# judge that has collapsed to "always 0". This probe is the counterweight --
# tasks that must NOT come back tier 0, so a recalibration that trades
# discrimination for a better benchmark number gets caught here.
#
# Lives in benchmarks/ rather than a Go test on purpose: it makes real
# `claude -p` calls, so it costs tokens and needs the network. The package's
# unit tests stub judgeFunc and stay hermetic; live measurement belongs here.
#
# Usage: ./judge-probe.sh          (exit 0 = all probes routed as expected)
set -uo pipefail
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
BIN="$(mktemp -d)/deadeye"

echo ">> building router from current tree ..." >&2
( cd "$REPO" && go build -o "$BIN" ./cmd/deadeye ) || { echo "build failed" >&2; exit 1; }
echo "   $("$BIN" version)" >&2
echo

fails=0

# want|label|prompt -- `want` is the model family the tier must resolve to.
probes=(
  "haiku|trivial rename (tier 0)|Rename the variable foo to bar in internal/utils/utils.go"
  "haiku|specified single file (tier 0)|Create a new Go package at internal/queue/queue.go with func Reverse(xs []int) []int returning a reversed copy. Add a one-line package doc comment."
  "sonnet|cross-file refactor (tier 1)|Refactor the authentication middleware across the api/ package to use the new session store, update every call site, and keep backwards compatibility with existing tokens"
  "sonnet|underspecified integration (tier 1)|Integrate our billing service with the existing Stripe webhook handler; requirements are not fully specified, infer them from the current code"
  "opus|subtle debugging (tier 2)|Users report intermittent double-charging under concurrent retries in the payment reconciliation job. Find and fix the root cause."
  "opus|security-critical (tier 2)|Audit the OAuth token refresh flow for privilege escalation and fix any vulnerability you find"
  "opus|architecture (tier 2)|Design the sharding and consistency model for our multi-region write path"
)

for p in "${probes[@]}"; do
  IFS='|' read -r want label prompt <<<"$p"
  got="$("$BIN" route "$prompt" 2>/dev/null | awk '/^[[:space:]]+model:/{print $2}')"
  if [[ "$got" == *"$want"* ]]; then
    printf '  ok    %-38s -> %s\n' "$label" "$got"
  else
    printf '  FAIL  %-38s -> %s (want %s)\n' "$label" "${got:-<none>}" "$want"
    fails=$((fails+1))
  fi
done

echo
if [ "$fails" -eq 0 ]; then
  echo "all ${#probes[@]} probes routed as expected"
else
  echo "$fails/${#probes[@]} probe(s) misrouted -- the judge lost discrimination" >&2
fi
# One sample per probe: the judge has no temperature/seed control, so a single
# disagreement is a signal to re-run and look at the distribution, not proof of
# a regression on its own.
exit "$fails"
