#!/usr/bin/env sh
# Builds the converter binaries from converter/ into bin/ and records their
# SHA-256 checksums in resources/binaries.json, which the PHP package uses to
# verify downloaded binaries.
#
#   scripts/build-binaries.sh            build all platforms, update the manifest
#   scripts/build-binaries.sh --host     build only this machine's platform (for development and CI tests)
#   scripts/build-binaries.sh --check    build all platforms and fail if any differs from the manifest
#                                        (the release workflow uses this before publishing)
#
# Builds are reproducible: the same Go toolchain (pinned in converter/go.mod)
# produces byte-identical binaries on any machine, so the checksums committed
# with a release match the binaries the release workflow publishes.
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
src="$here/converter"
out="$here/bin"
manifest="$here/resources/binaries.json"
mode=${1:-all}
mkdir -p "$out" "$here/resources"

version=$(sed -n 's/^const version = "\(.*\)"$/\1/p' "$src/cli.go")
[ -n "$version" ] || { echo "could not read the version from converter/cli.go" >&2; exit 1; }

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

if [ "$mode" = --host ]; then
  os=$(cd "$src" && go env GOOS)
  arch=$(cd "$src" && go env GOARCH)
  targets="$os/$arch"
else
  targets="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"
fi

entries=""
failed=0
for target in $targets; do
  os=${target%/*}
  arch=${target#*/}
  name="html2pdf-$os-$arch"
  [ "$os" = windows ] && name="$name.exe"
  (cd "$src" && CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -buildvcs=false -ldflags "-s -w" -o "$out/$name" .)
  chmod +x "$out/$name"
  sum=$(sha256 "$out/$name")
  size=$(wc -c < "$out/$name" | tr -d ' ')
  echo "$name  $sum  $size bytes"
  if [ "$mode" = --check ]; then
    expected=$(grep "\"$name\"" "$manifest" | sed -n 's/.*"sha256": *"\([0-9a-f]*\)".*/\1/p')
    if [ "$sum" != "$expected" ]; then
      echo "  MISMATCH: resources/binaries.json has ${expected:-nothing}" >&2
      failed=1
    fi
  fi
  entries="$entries${entries:+,
}        \"$name\": { \"sha256\": \"$sum\", \"size\": $size }"
done

if [ "$mode" = --check ]; then
  grep -q "\"version\": \"$version\"" "$manifest" || { echo "resources/binaries.json is not for version $version" >&2; failed=1; }
  [ $failed = 0 ] && echo "all binaries match resources/binaries.json" || exit 1
elif [ "$mode" = all ]; then
  printf '{\n    "version": "%s",\n    "files": {\n%s\n    }\n}\n' "$version" "$entries" > "$manifest"
  echo "wrote resources/binaries.json for version $version"
fi
