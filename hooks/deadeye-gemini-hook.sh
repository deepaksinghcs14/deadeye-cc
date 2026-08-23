#!/usr/bin/env bash
# deadeye hook adapter for Gemini CLI. Installed by `deadeye init gemini`
# to ~/.deadeye/hooks/ and referenced from the deadeye Gemini extension's
# hooks/hooks.json. Gemini passes the hook payload as JSON on stdin and
# reads the response as JSON on stdout -- the binary handles both; the
# --host gemini flag selects Gemini's output dialect (hookSpecificOutput.
# tool_input, decision:deny, etc.). The event name is passed as $1 (the
# canonical Claude event the daemon switches on), mapped from Gemini's own
# event name in hooks.json.
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

# Capture rather than exec: if the binary dies without output, Gemini
# still gets valid JSON (fail open, INV-5).
out="$("$BIN" hook "$EVENT" --host gemini 2>/dev/null)" || true
[ -n "$out" ] || out="{}"
printf '%s' "$out"
