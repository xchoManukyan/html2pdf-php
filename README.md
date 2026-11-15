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

The rendering is done by [html2pdf-core](https://github.com/xchoManukyan/html2pdf-core), a converter written in Go (also
used by the [Node.js package](https://github.com/xchoManukyan/html2pdf-node)). Its binary, about 11 MB, is not part
of the Composer download: each package version pins a converter release, gets the binary for your machine from
[its GitHub release](https://github.com/xchoManukyan/html2pdf-core/releases) and checks its SHA-256 checksum against
`resources/binaries.json` before using it. A file that doesn't match is never run.

- **On the first conversion** it is downloaded into the package's `bin/` folder, or into the system's temporary folder
  if that isn't writable (the PHP `curl` or `openssl` extension is needed for the download).
- **At deploy time**, run `vendor/bin/html2pdf-install` so no web request ever waits for it. For a Linux image built
  on a Mac: `vendor/bin/html2pdf-install --platform linux-amd64`.
- **Without internet access** on the server, install it in the build step as above, or copy the file and set
  `HTML2PDF_BINARY=/path/to/html2pdf-linux-amd64`. `HTML2PDF_NO_DOWNLOAD=1` makes sure nothing is ever downloaded.
- `HTML2PDF_BINARY_DIR` chooses the folder for downloads, and `HTML2PDF_DOWNLOAD_URL` a mirror
  (`https://mirror.example.com/html2pdf-core/v{version}/{file}`).

You can also build the converter yourself with Go: `sh scripts/build.sh --host` in a clone of html2pdf-core (the build
is reproducible, so a build of the pinned version has the same checksum), then point `HTML2PDF_BINARY` at it.

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
    ->setPermissions(true, false, false)     // print yes, copy no, modify no (AES-256 encryption)
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

### Languages, barcodes, forms, signatures, archiving

These work from the HTML and CSS, with no PHP options:

- **Arabic, Hebrew, Persian, Urdu:** `<html dir="rtl">` (or `dir="auto"`, CSS `direction: rtl`). Letters are joined,
  numbers and Latin words inside sentences keep their order, text aligns right, and flex rows, grid columns and tables
  run from right to left.
- **Hindi, Bengali, Tamil and the other Indic scripts** (Devanagari, Bengali, Gurmukhi, Gujarati, Oriya, Tamil, Telugu,
  Kannada, Malayalam): conjuncts, reph, vowel signs and marks are shaped with the font's OpenType rules, as in
  browsers. Use a font that covers the script (`@font-face` with Noto Sans Devanagari and the like, or an installed
  one such as Nirmala UI). Text copied from the PDF stays the original text.
- **Barcodes and QR codes:** mPDF's element, so mPDF templates work unchanged:
  `<barcode code="https://example.com/t/42" type="QR" size="1.2" error="M">`,
  `<barcode code="4006381333931" type="EAN13">`, `<barcode code="ORDER-123" type="C128" text="1">`.
  Types: QR, C128, EAN13, EAN8, UPCA, ISBN, ISSN, C39, C39+, I25, I25+.
- **Hyphenation:** `&shy;` soft hyphens, and `hyphens: auto` with a `lang` attribute in 76 language variants
  (`lang="de"`, `fr`, `es`, `ru`, `hy`, `pl`, `hi`...).
- **WOFF2 web fonts**, CSS grid **named lines** and **subgrid**.

And these options:

```php
$options->setFillableForms(true);            // inputs, checkboxes, radio buttons, selects, textareas become PDF fields
$options->setSignature('/secure/cert.pem', '/secure/key.pem', ['reason' => 'Approved', 'location' => 'Head office']);
$options->setPdfA(true);                     // PDF/A-2b for archiving (needs installed or @font-face fonts; no encryption)
$options->setEncryption(Options::AES128);    // default AES-256; AES-128 or RC4-128 only for very old readers
```

**Signatures** are standard PAdES baseline signatures (CMS with SHA-256) over the whole file. The certificate and key
are PEM files (RSA or ECDSA; the key without a password, both may be in one file). To convert a `.p12`/`.pfx`:
`openssl pkcs12 -in signer.p12 -nodes -out signer.pem`. With `HttpTransport` the converter server signs with the
certificate it was started with (`-sign-cert cert.pem -sign-key key.pem`), so the key stays on that server; pass
`Options::SERVER_CERTIFICATE` instead of the file names. A self-signed certificate shows as "validity unknown" until
the reader trusts it; use a certificate from a CA in the Adobe Approved Trust List for documents others must verify.

**Fillable forms** keep the values, checked boxes, selected options, `readonly`, `required` and `maxlength` of the
HTML. They cannot be combined with PDF/A.

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

From mPDF: `<barcode>` keeps working, `dir="rtl"` pages and Indic text too, `setPdfA(true)` replaces `PDFA => true`
and `setFillableForms(true)` replaces `useActiveForms`.

Not supported: Thai, Khmer, Myanmar, Sinhala and Tibetan shaping, vertical writing, signature timestamps and visible
signature boxes, JavaScript, and options for dompdf internals (font cache, DPI, backends).
Run your own templates through both converters before switching.

## Development

```bash
composer install
php bin/html2pdf-install   # the pinned converter release for this machine
composer test
composer analyse
```

The tests convert real documents with the converter binary and through the converter started as a server. See
[CONTRIBUTING.md](CONTRIBUTING.md) for the project layout and the release process, and [SECURITY.md](SECURITY.md)
to report a vulnerability.

## Licence

MIT, see [LICENSE](LICENSE). The converter binary contains code and data from other projects (Bootstrap, Tailwind
CSS, the Brotli dictionary, hyphenation patterns), listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
