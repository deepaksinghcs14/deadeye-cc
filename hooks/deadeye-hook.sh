#!/usr/bin/env bash
# Resolves the deadeye binary and forwards this hook invocation to it.
# Fail-open per INV-5: any resolution or execution failure prints {} and
# exits 0 -- a broken policy layer must never block the user's work.
#
# Binary resolution keeps auto-update RELIABLE. A `deadeye` on PATH is used
# only when it is at least the plugin's version. A STALE PATH binary (an old
# `go install`ed one) is bypassed in favor of the managed, self-updating
# ~/.deadeye/bin/deadeye -- otherwise it would shadow the managed binary
# forever and never see a plugin update. A current-or-newer PATH build still
# wins, so a dev's own build stays in charge. Version checks are only paid
# when a PATH binary actually exists (the ambiguous case); the common
# plugin-only install resolves straight to the managed binary.
set -u

# Recursion guard: deadeye's own AI routing judge (mode.routing_judge) spawns a
# nested `claude -p` session to classify a task. That session must run no
# deadeye hooks -- no re-judging, no cost from the judge session itself.
[ -n "${DEADEYE_JUDGE:-}" ] && { echo '{}'; exit 0; }

EVENT="${1:-}"
MANAGED="$HOME/.deadeye/bin/deadeye"

plugin_version() {
  [ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -f "$CLAUDE_PLUGIN_ROOT/.claude-plugin/plugin.json" ] || return 0
  grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' "$CLAUDE_PLUGIN_ROOT/.claude-plugin/plugin.json" | head -1 | sed -E 's/.*"([^"]+)"$/\1/'
}

bin_version() { "$1" version 2>/dev/null | awk '{print $2}'; }

# ver_ge A B: exit 0 if A >= B as x.y.z. Strips a leading v and any -dev /
# prerelease suffix; a missing or unparseable version compares as 0.0.0, so an
# unknowable PATH binary is treated as behind (safe: defer to the managed one).
ver_ge() {
  awk -v a="$1" -v b="$2" 'BEGIN{
    sub(/^v/,"",a); sub(/^v/,"",b);
    gsub(/[^0-9.].*$/,"",a); gsub(/[^0-9.].*$/,"",b);
    na=split(a,x,"."); nb=split(b,y,".");
    for(i=1;i<=3;i++){ai=(i<=na?x[i]+0:0); bi=(i<=nb?y[i]+0:0); if(ai>bi)exit 0; if(ai<bi)exit 1}
    exit 0
  }'
}

PV="$(plugin_version)"
PATH_BIN="$(command -v deadeye 2>/dev/null || true)"

BIN=""
if [ -n "$PATH_BIN" ]; then
  if [ -z "$PV" ] || ver_ge "$(bin_version "$PATH_BIN")" "$PV"; then
    BIN="$PATH_BIN"          # no plugin context, or a current/newer build -- stays in charge
  elif [ -x "$MANAGED" ]; then
    BIN="$MANAGED"           # PATH binary is STALE -> the self-updating managed binary takes over
  else
    BIN="$PATH_BIN"          # stale, but nothing managed yet; the bootstrap below fixes next session
  fi
elif [ -x "$MANAGED" ]; then
  BIN="$MANAGED"
fi

# Keep the managed binary converging to the plugin version: on SessionStart,
# bootstrap it if it's missing or behind. Never touches a PATH binary.
if [ "$EVENT" = "SessionStart" ] && [ -n "${CLAUDE_PLUGIN_ROOT:-}" ]; then
  if [ ! -x "$MANAGED" ] || { [ -n "$PV" ] && ! ver_ge "$(bin_version "$MANAGED")" "$PV"; }; then
    ( "${CLAUDE_PLUGIN_ROOT}/hooks/bootstrap.sh" >/dev/null 2>&1 & ) 2>/dev/null
  fi
fi

if [ -z "$BIN" ]; then
  echo '{}'
  exit 0
fi

OUT="$("$BIN" hook "$EVENT" 2>/dev/null)"
if [ -z "$OUT" ]; then
  echo '{}'
else
  echo "$OUT"
fi
