#!/usr/bin/env bash
# deadeye hook adapter for Codex CLI. Installed by `deadeye init codex`
# to ~/.deadeye/hooks/ and referenced from ~/.codex/hooks.json. Same
# contract as the Claude Code adapter minus the plugin bootstrap: Codex
# installs have no marketplace, so the binary that ran `init codex` is
# the binary; updates are manual.
set -u
EVENT="${1:-}"

BIN="$(command -v deadeye 2>/dev/null || true)"
if [ -z "$BIN" ] || [ ! -x "$BIN" ]; then
  BIN="$HOME/.deadeye/bin/deadeye"
fi
if [ ! -x "$BIN" ]; then
  cat > /dev/null 2>&1 || true
  printf '{}'
  exit 0
fi

# Capture rather than exec: if the binary dies without output, Codex
# still gets valid JSON (fail open, INV-5).
out="$("$BIN" hook "$EVENT" --host codex 2>/dev/null)" || true
[ -n "$out" ] || out="{}"
printf '%s' "$out"
