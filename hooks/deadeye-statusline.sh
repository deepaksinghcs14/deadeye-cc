#!/usr/bin/env bash
# Statusline badge for deadeye's coder mode: reads the level the daemon
# mirrors to ~/.deadeye/coder-mode and prints a colored [DEADEYE] tag.
# Silent when the mode is off/absent -- an empty statusline segment, not
# an error.
set -u

MODE_FILE="$HOME/.deadeye/coder-mode"
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
