#!/usr/bin/env sh
# Checks that every place naming the version agrees:
# converter/cli.go, src/Html2Pdf.php, resources/binaries.json, CHANGELOG.md,
# and that the Go toolchain pinned in converter/go.mod is the one the
# workflows use (binaries are only reproducible with the same toolchain).
#
#   scripts/check-versions.sh [expected-version]   e.g. the release tag without "v"
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
go=$(sed -n 's/^const version = "\(.*\)"$/\1/p' "$here/converter/cli.go")
php=$(sed -n "s/.*const VERSION = '\(.*\)';.*/\1/p" "$here/src/Html2Pdf.php")
manifest=$(sed -n 's/.*"version": *"\(.*\)".*/\1/p' "$here/resources/binaries.json")
toolchain=$(sed -n 's/^toolchain go\(.*\)$/\1/p' "$here/converter/go.mod")
expected=${1:-$go}
status=0
for pair in "converter/cli.go:$go" "src/Html2Pdf.php:$php" "resources/binaries.json:$manifest"; do
  [ "${pair#*:}" = "$expected" ] || { echo "${pair%%:*} has version ${pair#*:}, expected $expected" >&2; status=1; }
done
grep -q "^## \[$expected\]" "$here/CHANGELOG.md" || { echo "CHANGELOG.md has no section for $expected" >&2; status=1; }
for wf in "$here"/.github/workflows/*.yml; do
  pinned=$(sed -n "s/^ *GO_VERSION: *'\{0,1\}\([0-9.]*\)'\{0,1\}$/\1/p" "$wf")
  [ -z "$pinned" ] || [ "$pinned" = "$toolchain" ] || { echo "$(basename "$wf") pins Go $pinned, converter/go.mod has toolchain $toolchain" >&2; status=1; }
done
[ $status = 0 ] && echo "version $expected everywhere; Go toolchain $toolchain"
exit $status
