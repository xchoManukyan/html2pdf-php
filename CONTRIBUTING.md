# Contributing

Thanks for helping! Bug reports with a small HTML example, fixes and new options are all welcome.

## Where things live

The rendering (HTML parser, CSS engine, layout, PDF writer) is the converter
[html2pdf-core](https://github.com/xchoManukyan/html2pdf-core), written in Go. **Rendering bugs and CSS support belong
there.** This repository is the PHP package around it:

```
src/            Html2Pdf, Options, Result, transports, BinaryInstaller
tests/          PHPUnit tests; they convert real documents with the binary and with the converter as a server
bin/            html2pdf-install, and the converter binary once installed (not committed)
resources/      binaries.json: the pinned converter version and the SHA-256 checksums of its release binaries
scripts/        pinning the converter, version checks, release notes
```

The package runs the converter as a child process (command line with a JSON report) or talks to its HTTP API. Both
interfaces are described in the core README ("Interface for the packages").

## Setting up

You need PHP 7.4+ with Composer.

```bash
composer install
php bin/html2pdf-install       # downloads the pinned converter release into bin/ and verifies it
composer test
composer analyse               # PHPStan
```

To work against an unreleased converter, clone html2pdf-core next to this folder (with Go installed) and run
`composer build-binary`: it builds every platform from `../html2pdf-core`, pins those checksums in
`resources/binaries.json` and installs this machine's binary. Don't commit a pin to an unreleased converter.

## Making changes

- Add a test that fails without your change.
- Keep the package free of required extensions beyond `json`.
- PHP code must run on PHP 7.4 (no PHP 8-only syntax); the CI tests 7.4, 8.1 and 8.4 on Linux, macOS and Windows.
- New converter options: add them to the core first, then to `Options` (arguments and form fields) with tests.
- Add a line to `CHANGELOG.md` under "Unreleased".

## Releasing (maintainers)

1. If the release needs a newer converter, release html2pdf-core first, then pin it: `sh scripts/update-core.sh X.Y.Z`
   (copies the checksums of that release into `resources/binaries.json`).
2. Set the version in `src/Html2Pdf.php` (`VERSION`) and `CHANGELOG.md` (rename "Unreleased" to `## [x.y.z]` and add
   the link at the bottom). `sh scripts/check-versions.sh` must pass.
3. Commit, then tag and push: `git tag vX.Y.Z && git push origin main vX.Y.Z`.
4. The release workflow checks the versions and that `resources/binaries.json` equals the converter release's
   `checksums.json`, then publishes the GitHub release. Packagist picks up the tag through its GitHub hook.

The package version and the converter version are independent: a PHP-only fix is released without a new converter.
