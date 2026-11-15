# Changelog

All notable changes are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0]

First public release. The converter is [html2pdf-core](https://github.com/xchoManukyan/html2pdf-core) 0.1.0; its
release binaries are installed on first use or with `vendor/bin/html2pdf-install`.

- dompdf-style API: `loadHtml`, `loadHtmlFile`, `setPaper` (also dompdf's sizes in points), `render`, `output`,
  `stream`, `save`, and the common dompdf option names.
- Layout: flexbox, grid, floats, positioning, tables across pages, `@page` rules and margin boxes, page numbers,
  web fonts with kerning, SVG, WebP, BMP, `srcset` and `<picture>`.
- Tailwind CSS 3 and Bootstrap 5/4 applied automatically when a page uses them (script, link or class names),
  or chosen with `setCssLibraries()`.
- Layout width (`setViewportWidth`) to print a site's desktop or mobile layout.
- Right-to-left text (Hebrew, Arabic, Persian, Urdu): Unicode Bidirectional Algorithm, Arabic shaping with lam-alef
  ligatures, `dir`/`direction`, and right-to-left flex rows, grid columns and tables.
- OpenType shaping (GSUB/GPOS) for Indic scripts (Devanagari, Bengali, Gurmukhi, Gujarati, Oriya, Tamil, Telugu,
  Kannada, Malayalam) and for Arabic in fonts without presentation forms; copied text keeps the original characters.
- Fillable forms (`setFillableForms`): inputs, checkboxes, radio groups, selects and textareas become PDF form fields.
- Digital signatures (`setSignature`): PAdES baseline (B-B) with an RSA or ECDSA certificate.
- `<barcode>` (mPDF syntax): QR codes, Code 128, EAN-13/8, UPC-A, ISBN, ISSN, Code 39, Interleaved 2 of 5, as vectors.
- Hyphenation: soft hyphens and `hyphens: auto` for 76 language variants (hyph-utf8 patterns).
- WOFF2 web fonts; CSS grid named lines and subgrid.
- PDF/A-2b output (`setPdfA`), validated with veraPDF.
- Password protection and permissions with AES-256 encryption (AES-128 and RC4 for old readers).
- A report of missing images, stylesheets and fonts with the reason, and of the CSS libraries applied.
- The converter binary for Linux, macOS and Windows (x64 and ARM64) is downloaded on first use and verified
  against SHA-256 checksums shipped with the package; `vendor/bin/html2pdf-install` installs it at deploy time.
- `HttpTransport` to use a shared converter server instead of the binary.

[Unreleased]: https://github.com/xchoManukyan/html2pdf/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/xchoManukyan/html2pdf/releases/tag/v0.1.0
