#!/usr/bin/env sh
# Checks the package version: src/Html2Pdf.php (VERSION) and CHANGELOG.md
# agree, and resources/binaries.json pins a converter release with a
# checksum for all six platforms.
#
#   scripts/check-versions.sh [expected-version]   e.g. the release tag without "v"
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
php=$(sed -n "s/.*const VERSION = '\(.*\)';.*/\1/p" "$here/src/Html2Pdf.php")
core=$(sed -n 's/.*"version": *"\(.*\)".*/\1/p' "$here/resources/binaries.json")
expected=${1:-$php}
status=0
[ "$php" = "$expected" ] || { echo "src/Html2Pdf.php has version $php, expected $expected" >&2; status=1; }
grep -q "^## \[$expected\]" "$here/CHANGELOG.md" || { echo "CHANGELOG.md has no section for $expected" >&2; status=1; }
for p in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64.exe windows-arm64.exe; do
  grep -q "\"html2pdf-$p\": { \"sha256\": \"[0-9a-f]\{64\}\"" "$here/resources/binaries.json" || { echo "resources/binaries.json has no checksum for html2pdf-$p" >&2; status=1; }
done
[ $status = 0 ] && echo "package version $expected; converter $core"
exit $status
