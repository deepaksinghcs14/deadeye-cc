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
  # future invocations. This call still returns {} immediately. Gated to
  # SessionStart -- the first event of every session, so nothing is lost --
  # rather than every hook call: verified live, a first-ever session's
  # burst of PreToolUse/PostToolUse calls across the opening tool uses fired
  # roughly one concurrent download attempt per call, all racing to install
  # the same destination file. bootstrap.sh also has its own lock now, but
  # there's no reason to spawn 20 of them when one per session does the job.
  if [ "$EVENT" = "SessionStart" ]; then
    ( "${CLAUDE_PLUGIN_ROOT}/hooks/bootstrap.sh" >/dev/null 2>&1 & ) 2>/dev/null
  fi
  echo '{}'
  exit 0
fi

OUT="$("$BIN" hook "$EVENT" 2>/dev/null)"
if [ -z "$OUT" ]; then
  echo '{}'
else
  echo "$OUT"
fi
