#!/usr/bin/env sh
# Prints the CHANGELOG.md section of a version (for the GitHub release text).
#   scripts/release-notes.sh 0.1.0
set -eu
awk -v v="$1" '
  $0 ~ "^## \[" v "\]" { on = 1; next }
  on && /^## \[/ { exit }
  on { print }
' "$(dirname "$0")/../CHANGELOG.md"
