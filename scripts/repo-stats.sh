#!/usr/bin/env bash
# Maintainer-only adoption check -- reads GitHub's own repo/traffic APIs via
# `gh`, nothing runs on a user's machine and nothing is sent anywhere new.
# Owner/push access required for the traffic endpoint (14-day rolling window,
# that's GitHub's own retention limit, not something this script controls).
set -euo pipefail

repo="deepaksinghcs14/deadeye-cc"

stars=$(gh api "repos/$repo" --jq '.stargazers_count')
forks=$(gh api "repos/$repo" --jq '.forks_count')
downloads=$(gh api "repos/$repo/releases" --jq '[.[].assets[].download_count] | add // 0')
clones=$(gh api "repos/$repo/traffic/clones")
clone_uniques=$(echo "$clones" | jq '.uniques')
clone_total=$(echo "$clones" | jq '.count')
views=$(gh api "repos/$repo/traffic/views")
view_uniques=$(echo "$views" | jq '.uniques')
view_total=$(echo "$views" | jq '.count')

cat <<EOF
$repo -- adoption snapshot ($(date -u +%Y-%m-%dT%H:%MZ))

  stars              $stars
  forks              $forks
  release downloads  $downloads   (cumulative, all releases)

  last 14 days:
    unique cloners     $clone_uniques   ($clone_total total clones)
    unique visitors     $view_uniques   ($view_total total views)
EOF
