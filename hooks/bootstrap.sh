#!/usr/bin/env bash
# Best-effort background download of the deadeye release binary, sha256
# checksum-verified against the release's checksums.txt. Silent on any
# failure -- by the time this runs, the hook that spawned it has already
# returned {} to Claude Code (INV-5).
#
# Also doubles as the update path: an install is only skipped if it's
# already at the version plugin.json declares. Comparing against a local
# file (rather than querying GitHub for "latest") avoids a network round
# trip on every check -- deadeye-hook.sh calls this at most once per
# session, but it's still cheap to keep cheap.
set -u

REPO="deepaksinghcs14/deadeye-cc"
DEST_DIR="$HOME/.deadeye/bin"
DEST="$DEST_DIR/deadeye"

PLUGIN_VERSION=""
if [ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -f "$CLAUDE_PLUGIN_ROOT/.claude-plugin/plugin.json" ]; then
  PLUGIN_VERSION="$(grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' "$CLAUDE_PLUGIN_ROOT/.claude-plugin/plugin.json" | head -1 | sed -E 's/.*"([^"]+)"$/\1/')"
fi

if [ -x "$DEST" ]; then
  # Can't tell whether it's stale without a version to compare against --
  # leave a working binary alone rather than guess.
  [ -z "$PLUGIN_VERSION" ] && exit 0
  INSTALLED_VERSION="$("$DEST" version 2>/dev/null | awk '{print $2}')"
  [ "$INSTALLED_VERSION" = "$PLUGIN_VERSION" ] && exit 0
fi
command -v curl >/dev/null 2>&1 || exit 0

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) exit 0 ;;
esac

BASE_URL="https://github.com/${REPO}/releases/latest/download"
ASSET="deadeye_${OS}_${ARCH}"
TMP="$(mktemp -d)" || exit 0
trap 'rm -rf "$TMP"' EXIT

curl -fsSL -o "$TMP/deadeye" "$BASE_URL/$ASSET" || exit 0
curl -fsSL -o "$TMP/checksums.txt" "$BASE_URL/checksums.txt" || exit 0

WANT="$(grep " $ASSET\$" "$TMP/checksums.txt" | awk '{print $1}')"
[ -n "$WANT" ] || exit 0

if command -v sha256sum >/dev/null 2>&1; then
  GOT="$(sha256sum "$TMP/deadeye" | awk '{print $1}')"
else
  GOT="$(shasum -a 256 "$TMP/deadeye" | awk '{print $1}')"
fi
[ "$WANT" = "$GOT" ] || exit 0

mkdir -p "$DEST_DIR"
mv "$TMP/deadeye" "$DEST"
chmod +x "$DEST"
