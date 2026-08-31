#!/usr/bin/env bash
# Grades a completed run. Args: PKG [RACE]. Env: HIDDEN=dir of hidden *_test.go.
# cwd = the workdir the agent edited. Prints PASS or FAIL:<reason>; exit 0/1.
set -uo pipefail
PKG="$1"; RACE="${2:-}"
[ -d "$PKG" ] || { echo "FAIL:pkg-missing"; exit 1; }
cp "$HIDDEN"/*.go "$PKG"/ 2>/dev/null || { echo "FAIL:no-hidden-slot"; exit 1; }
go build ./... >build.log 2>&1 || { echo "FAIL:build"; exit 1; }
go test $RACE "./$PKG/" >test.log 2>&1 || { echo "FAIL:test"; exit 1; }
echo PASS
