#!/usr/bin/env bash
# deadeye hook adapter for Codex CLI. Reached two ways: as a Codex plugin
# (hooks/codex-hooks.json runs it from the plugin root), or installed by
# `deadeye init codex` to ~/.deadeye/hooks/ and referenced from
# ~/.codex/hooks.json.
set -u
EVENT="${1:-}"

# Plugin install on a machine with no deadeye binary yet: fetch it in the
# background, the way the Claude Code adapter does. bootstrap.sh reads the
# version from CLAUDE_PLUGIN_ROOT, which Codex sets alongside PLUGIN_ROOT.
# Only when the binary is missing -- after that, `deadeye update` updates it.
if [ "$EVENT" = "SessionStart" ] && [ -n "${PLUGIN_ROOT:-}" ] && [ ! -x "$HOME/.deadeye/bin/deadeye" ] \
   && ! command -v deadeye >/dev/null 2>&1 && [ -x "$PLUGIN_ROOT/hooks/bootstrap.sh" ]; then
  ( CLAUDE_PLUGIN_ROOT="$PLUGIN_ROOT" "$PLUGIN_ROOT/hooks/bootstrap.sh" >/dev/null 2>&1 & ) 2>/dev/null
fi

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
