# Contributing

Thanks for helping! Bug reports with a small HTML example, fixes and new CSS support are all welcome.

## How the project is built

```
src/            the PHP package (Html2Pdf, Options, Result, transports, BinaryInstaller)
tests/          PHPUnit tests; they convert real documents
bin/            html2pdf-install, and the converter binaries you build (not committed)
resources/      binaries.json: version and SHA-256 checksums of the published converter binaries
converter/      the converter in Go (standard library only): HTML parser, CSS engine, layout, PDF writer
scripts/        building binaries, version checks, release notes
```

The PHP package runs the converter as a child process (or talks to it over HTTP). Most rendering work happens in
`converter/`; see [`converter/README.md`](converter/README.md) for what it supports and how it is organised.

## Setting up

You need PHP 7.4+ with Composer, and Go (the version in `converter/go.mod`, line `toolchain`).

```bash
composer install
composer build-binary          # builds the converter for this machine into bin/
composer test
composer analyse               # PHPStan
cd converter && go test ./...  # converter tests
```

The converter also has a web UI for trying things out: `cd converter && go run .`, then open http://localhost:8080.

## Making changes

- Add a test that fails without your change. For layout fixes, the converter's render tests compare positions and
  colours in the PDF; compare your result with a browser rendering of the same HTML.
- Keep the converter free of third-party Go modules, and the PHP package free of required extensions beyond `json`.
- PHP code must run on PHP 7.4 (no PHP 8-only syntax); the CI tests 7.4, 8.1 and 8.4 on Linux, macOS and Windows.
- Run `gofmt` on Go code. Add a line to `CHANGELOG.md` under "Unreleased".
- You don't need to update `resources/binaries.json` in a pull request; it is regenerated when releasing.

## Releasing (maintainers)

The binaries are reproducible: the pinned Go toolchain builds byte-identical files on any machine. That is what lets
the package verify downloads against checksums committed before the release exists.

1. Set the new version in `converter/cli.go` (`const version`), `src/Html2Pdf.php` (`VERSION`) and `CHANGELOG.md`
   (rename "Unreleased" to `## [x.y.z]` and add the link at the bottom).
2. With the Go version from `converter/go.mod`, run `sh scripts/build-binaries.sh`. It builds all platforms and
   rewrites `resources/binaries.json`.
3. `sh scripts/check-versions.sh` must report the new version everywhere.
4. Commit, then tag and push: `git tag vX.Y.Z && git push origin main vX.Y.Z`.
5. The release workflow rebuilds every binary, fails if any differs from `resources/binaries.json`, and publishes
   the GitHub release with the binaries, `SHA256SUMS` and the third-party notices.
6. Packagist picks up the tag through its GitHub hook. Installs of the new version download binaries only after the
   release exists, a few minutes after the tag.
