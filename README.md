# html2pdf for PHP

[![CI](https://github.com/xchoManukyan/html2pdf/actions/workflows/ci.yml/badge.svg)](https://github.com/xchoManukyan/html2pdf/actions/workflows/ci.yml)
[![Packagist](https://img.shields.io/packagist/v/xchomanukyan/html2pdf)](https://packagist.org/packages/xchomanukyan/html2pdf)
[![PHP](https://img.shields.io/packagist/php-v/xchomanukyan/html2pdf)](composer.json)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

HTML to PDF with a dompdf-style API, backed by a fast self-contained converter. It lays out flexbox and grid, applies
Tailwind and Bootstrap (detected automatically), and handles SVG, WebP and web fonts. No Chrome, no PHP extensions
beyond `json`, and no system packages: the converter for Linux, macOS or Windows (x64 and ARM64) is fetched once and
verified against checksums shipped with the package.

```php
use Html2Pdf\Html2Pdf;

$pdf = new Html2Pdf();
$pdf->loadHtml('<h1 class="text-3xl font-bold">Invoice 1042</h1>');
$pdf->setPaper('A4', 'portrait');
$pdf->render();
$pdf->stream('invoice.pdf');                 // download; ['Attachment' => false] shows it in the browser
```

## Requirements and installation

PHP 7.4 or 8.x with `proc_open` allowed, or any PHP that can make HTTP requests when you use a converter server.

```bash
composer require xchomanukyan/html2pdf
vendor/bin/html2pdf-install        # optional: fetch the converter now instead of on the first conversion
```

### The converter binary

The rendering is done by a converter written in Go (source in [`converter/`](converter/)). Its binary, about 9 MB, is
not part of the Composer download; the package gets the one for your machine from the
[GitHub release](https://github.com/xchoManukyan/html2pdf/releases) of the same version and checks its SHA-256
checksum against `resources/binaries.json` before using it. A file that doesn't match is never run.

- **On the first conversion** it is downloaded into the package's `bin/` folder, or into the system's temporary folder
  if that isn't writable (the PHP `curl` or `openssl` extension is needed for the download).
- **At deploy time**, run `vendor/bin/html2pdf-install` so no web request ever waits for it. For a Linux image built
  on a Mac: `vendor/bin/html2pdf-install --platform linux-amd64`.
- **Without internet access** on the server, install it in the build step as above, or copy the file and set
  `HTML2PDF_BINARY=/path/to/html2pdf-linux-amd64`. `HTML2PDF_NO_DOWNLOAD=1` makes sure nothing is ever downloaded.
- `HTML2PDF_BINARY_DIR` chooses the folder for downloads, and `HTML2PDF_DOWNLOAD_URL` a mirror
  (`https://mirror.example.com/html2pdf/v{version}/{file}`).

You can also build the converter yourself with Go: `sh scripts/build-binaries.sh --host` in a clone of the repository.

## Usage

```php
use Html2Pdf\Html2Pdf;
use Html2Pdf\Options;

// one call
$bytes = Html2Pdf::convert($html, ['paperSize' => 'Letter', 'pageNumbers' => true]);

// a file with its stylesheets and images (its folder becomes the base path)
$pdf = new Html2Pdf();
$result = $pdf->loadHtmlFile('/var/www/templates/invoice.html')->render();
$result->save('/tmp/invoice.pdf');

// options
$options = (new Options())
    ->setPaperSize('A4')                     // A3, A4, A5, Letter, Legal, [80, 200] or '80x200' (mm)
    ->setOrientation('landscape')
    ->setMarginMm(10)                        // 5–50; CSS @page rules in the document win
    ->setPageNumbers(true)                   // "Page X of Y" at the bottom
    ->setViewportWidth(1280)                 // lay out like a 1280px browser window, scaled to the paper
    ->setCssLibraries(['bootstrap5'])        // default []: detect from the page
    ->setBasePath('/var/www/templates')      // local files the document may read (dompdf: chroot)
    ->setRemoteEnabled(true)                 // download http(s) images, CSS, fonts
    ->setPassword('open-me', 'owner-pass')   // password to open the PDF
    ->setPermissions(true, false, false)     // print yes, copy no, modify no
    ->setTimeout(30);                        // seconds
$pdf = new Html2Pdf($options);
```

### What happened during the conversion

`render()` returns a `Result`, also available later from `getResult()`:

```php
$result = $pdf->render();
$result->getPdf();                // the PDF bytes
$result->getMissingResources();   // ['img/missing.png']
$result->getFailures();           // [['ref' => 'img/missing.png', 'reason' => 'resource not found']]
$result->getCssLibraries();       // [['lib' => 'bootstrap5', 'source' => 'classes']]
$result->getRenderTimeMs();
```

Errors throw `Html2Pdf\Exception\Html2PdfException`. Its subclasses are `ConversionException` (the document was
rejected: empty, not HTML, too large) and `TimeoutException`.

### CSS libraries

By default every library the page uses is detected and applied: a Tailwind script, a Bootstrap link (served from a
bundled copy, so it works offline), or, if the page loads neither, the class names it uses. A page mixing both gets
both, as in the browser. `setCssLibraries(['tailwind', 'bootstrap5'])` applies exactly those, even if the page doesn't
load them; `setCssLibraries('none')` turns the bundled libraries off.

## Converter server instead of the binary

When `proc_open` is disabled, or several applications should share one converter, run the binary as a service and
point the package at it:

```bash
vendor/xchomanukyan/html2pdf/bin/html2pdf-linux-amd64 -addr :8080   # after html2pdf-install; add -allow-remote for remote resources
```

```php
use Html2Pdf\Transport\HttpTransport;

$pdf = new Html2Pdf($options, new HttpTransport('http://pdf-service:8080'));
```

The server cannot read your files, so with a base path set the package uploads the stylesheets, images and fonts the
document refers to (inside the base path only). Keep the server on an internal network: it has no authentication.

## Migrating from dompdf

| dompdf | html2pdf |
|---|---|
| `new Dompdf($options)` | `new Html2Pdf($options)` |
| `$options->set('isRemoteEnabled', true)` | same, or `setRemoteEnabled(true)` |
| `$options->set('chroot', $dir)` | same, or `setBasePath($dir)` |
| `defaultPaperSize`, `defaultPaperOrientation` | same names work with `set()` |
| `loadHtml($html, $encoding)` | same |
| `loadHtmlFile($file)` | same (local files only) |
| `setPaper('A4', 'landscape')`, `setPaper([0, 0, w, h])` in points | same |
| `render()`, `output()`, `stream($name, ['Attachment' => 0])` | same; `render()` also returns the `Result` |
| `<script type="text/php">` for page numbers | CSS: `@page { @bottom-right { content: counter(page) " / " counter(pages) } }` or `setPageNumbers(true)` |
| `getCanvas()->page_text(...)` | CSS `@page` margin boxes or fixed headers and footers with `counter(page)` |

Not supported: right-to-left scripts (Arabic, Hebrew) and other complex scripts, hyphenation, WOFF2 fonts, JavaScript,
and options for dompdf internals (font cache, DPI, backends). Run your own templates through both converters before
switching, and keep dompdf for documents that need these.

## Development

```bash
composer install
composer build-binary      # the converter for this machine, from converter/ (needs Go)
composer test
composer analyse
```

The tests convert real documents with the converter binary and through the converter started as a server. See
[CONTRIBUTING.md](CONTRIBUTING.md) for the project layout and the release process, and [SECURITY.md](SECURITY.md)
to report a vulnerability.

## Licence

MIT, see [LICENSE](LICENSE). The converter contains code from Bootstrap and Tailwind CSS (both MIT), listed in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
