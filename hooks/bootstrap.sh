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
LOCK_DIR="$DEST_DIR/.bootstrap.lock"
INSTALL_TMP=""

mkdir -p "$DEST_DIR" 2>/dev/null || exit 0

# One install/update at a time. deadeye-hook.sh only fires this on
# SessionStart, but two Claude Code windows can start within the same
# second -- without this, both race to curl+mv onto the same destination.
# `mkdir` as a lock is atomic even across processes/machines sharing $HOME.
if ! mkdir "$LOCK_DIR" 2>/dev/null; then
  # Someone else is already installing. If that lock is old enough to be
  # from a run that was SIGKILLed mid-install rather than one still in
  # progress, clear it and take over; otherwise just exit -- the next
  # SessionStart retries.
  if [ -n "$(find "$LOCK_DIR" -maxdepth 0 -mmin +10 2>/dev/null)" ]; then
    rmdir "$LOCK_DIR" 2>/dev/null
    mkdir "$LOCK_DIR" 2>/dev/null || exit 0
  else
    exit 0
  fi
fi
trap 'rmdir "$LOCK_DIR" 2>/dev/null; rm -f "$INSTALL_TMP"' EXIT

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

LATEST_URL="https://github.com/${REPO}/releases/latest/download"
ASSET="deadeye_${OS}_${ARCH}"
TMP="$(mktemp -d)" || exit 0
trap 'rmdir "$LOCK_DIR" 2>/dev/null; rm -f "$INSTALL_TMP"; rm -rf "$TMP"' EXIT

# Pin to the checked-out plugin's own version rather than always pulling
# "latest" -- otherwise the version comparison above (against
# plugin.json) and what actually gets downloaded (always latest) can
# permanently disagree, re-downloading the full binary every single
# session without ever converging. Fall back to latest once if the
# pinned tag 404s (checkout ahead of the release, or an older plugin
# checkout with no matching release asset name).
if [ -n "$PLUGIN_VERSION" ]; then
  BASE_URL="https://github.com/${REPO}/releases/download/v${PLUGIN_VERSION}"
else
  BASE_URL="$LATEST_URL"
fi

if ! curl -fsSL -o "$TMP/deadeye" "$BASE_URL/$ASSET"; then
  [ "$BASE_URL" = "$LATEST_URL" ] && exit 0
  BASE_URL="$LATEST_URL"
  curl -fsSL -o "$TMP/deadeye" "$BASE_URL/$ASSET" || exit 0
fi
curl -fsSL -o "$TMP/checksums.txt" "$BASE_URL/checksums.txt" || exit 0

WANT="$(grep " $ASSET\$" "$TMP/checksums.txt" | awk '{print $1}')"
[ -n "$WANT" ] || exit 0

if command -v sha256sum >/dev/null 2>&1; then
  GOT="$(sha256sum "$TMP/deadeye" | awk '{print $1}')"
else
  GOT="$(shasum -a 256 "$TMP/deadeye" | awk '{print $1}')"
fi
[ "$WANT" = "$GOT" ] || exit 0

# Install atomically. $TMP (mktemp -d, usually $TMPDIR) is frequently a
# different filesystem from $HOME, so `mv` straight into $DEST would
# silently degrade to copy-then-unlink -- not atomic, and on an update it
# would truncate-and-rewrite $DEST in place while deadeye-hook.sh's
# `[ -x "$DEST" ]` check could see a half-written binary mid-copy.
# chmod BEFORE the same-filesystem rename, then rename within $DEST_DIR --
# that final step is what's actually atomic.
chmod +x "$TMP/deadeye"
INSTALL_TMP="$DEST_DIR/.deadeye.tmp.$$"
cp "$TMP/deadeye" "$INSTALL_TMP" && mv "$INSTALL_TMP" "$DEST"
