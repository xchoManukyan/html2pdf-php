#!/usr/bin/env sh
# Pins the converter (html2pdf-core) version this package uses: writes its
# release checksums to resources/binaries.json, which the installer verifies
# every downloaded binary against.
#
#   scripts/update-core.sh 0.2.0                  checksums.json of the core release v0.2.0
#   scripts/update-core.sh --local ../html2pdf-core
#        builds every platform from a core checkout (reproducible, so the checksums equal
#        the release's), pins them, and puts this machine's binary into bin/ (development)
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
manifest="$here/resources/binaries.json"

if [ "${1:-}" = --local ]; then
  core=$(cd "${2:?usage: update-core.sh --local CORE_DIR}" && pwd)
  sh "$core/scripts/build.sh"
  cp "$core/dist/checksums.json" "$manifest"
  os=$(cd "$core" && go env GOOS)
  arch=$(cd "$core" && go env GOARCH)
  name="html2pdf-$os-$arch"
  [ "$os" = windows ] && name="$name.exe"
  mkdir -p "$here/bin"
  cp "$core/dist/$name" "$here/bin/$name"
  echo "pinned the local core build and installed bin/$name"
else
  version=${1:?usage: update-core.sh VERSION | --local CORE_DIR}
  url="https://github.com/xchoManukyan/html2pdf-core/releases/download/v$version/checksums.json"
  tmp="$manifest.tmp"
  curl -fsSL "$url" -o "$tmp"
  grep -q "\"version\": \"$version\"" "$tmp" || { rm -f "$tmp"; echo "$url is not for version $version" >&2; exit 1; }
  mv "$tmp" "$manifest"
fi
sed -n 's/.*"version": *"\(.*\)".*/converter version \1 pinned in resources\/binaries.json/p' "$manifest"
