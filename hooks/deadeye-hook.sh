#!/usr/bin/env bash
# Resolves the deadeye binary and forwards this hook invocation to it.
# Fail-open per INV-5: any resolution or execution failure prints {} and
# exits 0 -- a broken policy layer must never block the user's work.
set -u

EVENT="${1:-}"
BIN=""

if command -v deadeye >/dev/null 2>&1; then
  BIN="$(command -v deadeye)"
elif [ -x "$HOME/.deadeye/bin/deadeye" ]; then
  BIN="$HOME/.deadeye/bin/deadeye"
  if [ "$EVENT" = "SessionStart" ]; then
    # Once per session: check whether the self-bootstrapped binary is
    # behind plugin.json's version and update it in the background for
    # next time. Never touches a PATH-installed (user-managed) binary.
    ( "${CLAUDE_PLUGIN_ROOT}/hooks/bootstrap.sh" >/dev/null 2>&1 & ) 2>/dev/null
  fi
fi

if [ -z "$BIN" ]; then
  # Not installed yet: kick off a background, best-effort self-bootstrap for
  # future invocations. This call still returns {} immediately.
  ( "${CLAUDE_PLUGIN_ROOT}/hooks/bootstrap.sh" >/dev/null 2>&1 & ) 2>/dev/null
  echo '{}'
  exit 0
fi

OUT="$("$BIN" hook "$EVENT" 2>/dev/null)"
if [ -z "$OUT" ]; then
  echo '{}'
else
  echo "$OUT"
fi
