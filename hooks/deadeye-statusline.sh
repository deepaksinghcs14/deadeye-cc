#!/usr/bin/env bash
# Statusline badge for deadeye's coder mode. Claude Code pipes session JSON
# on stdin; the session_id in it selects this session's own mode file
# (~/.deadeye/coder-mode.<id>), so concurrent sessions each show their own
# badge. No session_id on stdin -> the global ~/.deadeye/coder-mode
# fallback (last writer wins). Silent when the mode is off/absent -- an
# empty statusline segment, not an error.
set -u

MODE_FILE="$HOME/.deadeye/coder-mode"
sid="$(cat 2>/dev/null | sed -n 's/.*"session_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
if [ -n "$sid" ]; then
  # Mirror the daemon's sanitization: anything outside [A-Za-z0-9-] -> _
  sid_safe="$(printf '%s' "$sid" | tr -c 'A-Za-z0-9-' '_')"
  [ -f "$HOME/.deadeye/coder-mode.$sid_safe" ] && MODE_FILE="$HOME/.deadeye/coder-mode.$sid_safe"
fi
[ -f "$MODE_FILE" ] || exit 0
level="$(tr -d '[:space:]' < "$MODE_FILE")"
[ -n "$level" ] || exit 0

case "$level" in
  marksman) printf '\033[38;5;108m[DEADEYE]\033[0m' ;;          # green -- the default discipline
  spotter)  printf '\033[38;5;65m[DEADEYE:SPOTTER]\033[0m' ;;   # muted green -- light touch
  sniper)   printf '\033[38;5;173m[DEADEYE:SNIPER]\033[0m' ;;   # amber -- maximum minimalism
  review)   printf '\033[38;5;109m[DEADEYE:REVIEW]\033[0m' ;;   # steel blue -- review pass
  *) exit 0 ;;
esac
