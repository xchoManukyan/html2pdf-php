# html2pdf converter

An HTML → PDF converter with a web upload page, written in Go using **only the
standard library** (no third-party modules, no headless browser, no wkhtmltopdf).

This is the engine behind the [PHP package](../README.md). It also works on its own: as a command-line tool, as a
web service (`-addr :8080`, used by the PHP package's `HttpTransport`), or with its built-in upload page. Prebuilt
binaries for Linux, macOS and Windows are attached to every
[release](https://github.com/xchoManukyan/html2pdf/releases).

## Run

```bash
go run .                      # web UI at http://localhost:8080
go run . -addr :9000          # another port
go build -o html2pdf.exe .    # single self-contained binary
```

CLI mode (no server):

```bash
go run . -in samples/invoice.html -out invoice.pdf -size Letter -landscape -margin 20 -numbers
```

More CLI options:

```bash
go run . -in samples/bundle/index.html            # relative CSS, images, fonts load from the file's folder
go run . -in site.zip                              # a ZIP with the HTML and its assets
go run . -in page.html -remote                     # also download http(s) resources
go run . -in page.html -user-password secret -owner-password admin -no-copy -no-print
go run . -allow-remote                             # web server: let users enable remote resources
go run . -in site/html/page.html -root site         # page may load ../css, ../img (anything inside -root)
go run . -in page.html -viewport 1280               # desktop layout (CSS width 1280px) scaled to the paper
go run . -in page.html -size 210x5000               # custom paper size in mm (width x height)
go run . -in page.html -css bootstrap5,tailwind     # apply these CSS libraries (default: detect them from the page)
```

For programs (the PHP package uses this): `-in -` reads the document from standard input (`-root dir -doc
pages/x.html` say where it is, for relative links), `-out -` writes the PDF to standard output, and `-json` prints a
report (`ok`, `error`, `bytes`, `tookMs`, `cssLibs`, `missing`, `failures`; on standard error when the PDF goes to
standard output). Exit code 1 on errors. Passwords can be given as `HTML2PDF_USER_PASSWORD` /
`HTML2PDF_OWNER_PASSWORD` environment variables instead of flags, so they don't appear in the process list.
`-version` prints the version.

## Web interface

Open the page, then either **upload** or **paste** HTML. A page that uses its own stylesheets, images or
fonts needs those files too: select the `.html` together with them, drop them all at once, drop the whole folder
(only the files the page and its CSS mention are sent; with several HTML files you pick which one to convert),
or upload a `.zip`. Files the page refers to but that were not uploaded are listed in a warning after conversion.
Pick a page size (A4, Letter, Legal, A3, A5), orientation, margin, page numbers and the layout width (paper, or a
desktop/laptop/tablet/mobile browser width scaled to the paper) and CSS libraries (Tailwind, Bootstrap 5, Bootstrap 4
or no library; leave all unchecked to detect them from the page). The PDF shows in a
preview next to the form and can be downloaded.

HTTP API: `POST /api/convert` (multipart) with one or more `file` parts (optional `path` field per file
for folder structure, `main` to pick the document) *or* `html`, plus optional `pageSize`,
`orientation` (`portrait`|`landscape`), `margin` (mm, 5–50), `pageNumbers` (`true`), `viewport` (CSS px) and
`cssLib` (repeat it or give a list of `tailwind`, `bootstrap5`, `bootstrap4`; `none`; omitted: detect).
It answers with `application/pdf` (add `?inline=1` to display rather than download), or with
`{"error": "..."}` JSON and a 4xx status. Response headers: `X-Result-URL` (the same PDF under
`/api/result/{id}/{name}.pdf` for 30 minutes, so a browser saves it under its name) and `X-Missing-Resources`
(URL-encoded JSON list of resources that could not be loaded). `GET /healthz` returns `ok`.

## Project layout

| Path | What it does |
|---|---|
| `main.go` | Flags, HTTP server with graceful shutdown, CLI mode |
| `internal/server` | HTTP handlers, upload limits, embedded `static/index.html` UI |
| `internal/converter` | Business rules: validation, 10 MB limit, charset detection (UTF-8/16 BOM, Windows-1252 fallback), option normalisation, safe output file name |
| `internal/htmlparse` | Forgiving HTML tokenizer + tree builder (void/raw-text elements, implicit `p`/`li`/`td`/`tr` closing, entities) |
| `internal/css` | Stylesheet parser and cascade input: Selectors Level 4 subset, `@media`/`@supports`/`@page`/`@font-face`, nesting, `!important`, colors, lengths with `calc()` |
| `internal/render` | Style cascade and layout engine: line breaking, justification, margins, pagination, lists, tables, images, generated content |
| `internal/bootstrap` | Bundled Bootstrap 5.3.9 and 4.6.2 CSS (MIT) for pages that link Bootstrap from a CDN |
| `internal/tailwind` | Tailwind CSS v3 utility generator for pages that load Tailwind's browser script (no JavaScript is run) |
| `internal/webp` | WebP decoder: VP8 (lossy), VP8L (lossless), ALPH alpha, VP8X/animation containers; registers with `image.Decode` |
| `internal/ttf` | TrueType/OpenType (TrueType outlines) and TTC parser, glyph subsetter, WOFF 1.0 decoder |
| `internal/fonts` | Installed-font index, CSS font matching (family/weight/style/width), per-character fallback |
| `internal/resource` | Resource loader: `data:` URIs, ZIP bundles, local files confined to a base folder (chroot), opt-in remote http(s) with SSRF protection, size limits and timeouts |
| `internal/pdf` | PDF writer: embedded subset fonts (Type0/CIDFontType2 + ToUnicode), base-14 fallback, Flate, images, links |

## Fonts

Text is set with real TrueType fonts found on the machine (`C:\Windows\Fonts`, `/usr/share/fonts`,
`/Library/Fonts`, a `fonts/` folder next to the program, or folders listed in `HTML2PDF_FONTS`).
Only the glyphs actually used are embedded, so PDFs stay small. Any script the installed fonts cover
works, including Armenian, Cyrillic, Greek, Georgian and CJK. When a font lacks a character, the next
font in `font-family` is tried, then common fallback fonts, then every installed font. Missing bold or
italic faces are synthesised. `@font-face` works with `data:` URIs (TTF, OTF with TrueType outlines,
WOFF 1.0) and `local()`. The CSS default family is `serif` (Times New Roman), as in browsers.

On a server without any fonts, the built-in PDF fonts (Helvetica/Courier, Western European only) are used.

## Supported HTML/CSS

- **Selectors:** type, `*`, `.class`, `#id`, all attribute operators (`[a]`, `=`, `~=`, `|=`, `^=`, `$=`, `*=`, `i` flag),
  combinators (descendant, `>`, `+`, `~`), `:first/last/only-child`, `:nth-child()`, `:nth-last-child()`,
  `:*-of-type`, `:not()`, `:is()`, `:where()`, `:has()`, `:empty`, `:root`, `:link`, `::before`, `::after`, `::marker`.
  Interactive states (`:hover`, `:focus`, …) never match, as when printing.
- **Cascade:** specificity, source order, `!important`, `inherit`/`initial`/`unset`, custom properties with `var()`
  (fallbacks, nesting), CSS nesting (`&`).
- **At-rules:** `@media` (evaluated as print, with width/height/orientation and range syntax), `@supports`,
  `@page` (size, margins), `@font-face`, `@layer`/`@container` (contents applied).
- **Values:** all 148 named colors, hex with alpha, `rgb()`/`hsl()` (alpha composited on white), `currentColor`;
  `px pt pc in cm mm Q em rem ex ch % vw vh vmin vmax`; `calc()`, `min()`, `max()`, `clamp()`.
- **Text:** `font` shorthand, `font-family/size/weight (100–900)/style`, `line-height` (incl. `normal` from font metrics),
  `text-align` (justify), `text-indent`, `text-transform`, `text-decoration`, `letter-spacing`, `word-spacing`,
  `white-space` (`normal`, `nowrap`, `pre`, `pre-wrap`, `pre-line`), `vertical-align: sub/super`, `visibility`.
- **Generated content:** `content` with strings, `attr()`, `counter()`, quotes; `counter-reset/increment/set`.
- **Boxes:** `margin` (incl. `auto` centring), `padding`, `border*` (incl. logical properties), backgrounds,
  `width`/`min-width`/`max-width`, `height`/`min-height`, `box-sizing`; inline elements get horizontal margins,
  padding, borders and backgrounds.
- **Flexbox:** `display: flex | inline-flex`, `flex-direction` (incl. reverse), `flex-wrap`, `flex-flow`, `flex`,
  `flex-grow/shrink/basis` (with min/max clamping), `justify-content` (all values), `align-items`, `align-self`,
  `align-content`, `gap`/`row-gap`/`column-gap`, `order`, auto margins. A flex row taller than a page flows
  across pages item by item; rows that fit are moved whole to the next page.
- **Visual effects:** `border-radius` (all corners, %), border styles `solid dashed dotted double groove ridge inset outset`,
  `outline` (+ `outline-offset`), `box-shadow` (multiple, blur, spread, `inset`), `text-shadow`, `opacity` and rgba/hsla
  transparency (real PDF transparency), `overflow: hidden` clipping (rounded), rounded images.
- **Backgrounds:** multiple layers, `linear-gradient`, `radial-gradient` (circle/ellipse, sizes, `at` position),
  `repeating-*-gradient`, `url(data:...)` images with `background-size` (cover, contain, lengths),
  `background-position` and `background-repeat`.
- **Pagination control:** `break-inside`/`page-break-inside: avoid` (blocks, cards, figures, images, table rows),
  `break-after: avoid` (headings `h1`–`h6` by default, so they are never stranded at a page bottom),
  `break-before/after: page`, `orphans` and `widows` (default 2, set per element; paragraphs are laid out twice
  near page ends to place the break), `` size and margins.
- **Transforms:** `transform` with `translate(X/Y)`, `scale(X/Y)`, `rotate`, `skew(X/Y)`, `matrix`, composed lists,
  `transform-origin` (keywords, %, lengths), and the individual `translate`/`rotate`/`scale` properties. Transforms do
  not affect layout (like in CSS) and are applied as PDF vector transforms. 3D transforms are ignored.
- **CSS Grid:** `display: grid | inline-grid`; `grid-template-columns/rows` with lengths, %, `fr`, `auto`,
  `min-content`, `max-content`, `minmax()`, `fit-content()`, `repeat(n | auto-fill | auto-fit, …)`, line names (ignored);
  `grid-template-areas`, `grid-template` and `grid` shorthands; `grid-auto-rows/columns`, `grid-auto-flow` (row,
  column, dense); placement with `grid-row/column(-start/-end)`, `grid-area`, negative lines, `span`, area names;
  `gap`; `justify-items/self`, `align-items/self`, `place-*`, `justify-content`/`align-content`; intrinsic sizing
  (spanning items, flexible tracks, stretch of auto tracks); rows paginate and tall rows flow across pages.
- **Tables:** real cell grid with `colspan` and `rowspan` (incl. `rowspan="0"`); `border-collapse: separate` (the
  default, with `border-spacing` / `cellspacing`) and `collapse` (borders centred on grid lines, table border included);
  automatic column widths from content, `width` on cells, `<col>`/`<colgroup>` widths, spans and backgrounds;
  `table-layout: fixed`; `caption-side: top | bottom`; backgrounds painted in CSS order (column, row group, row,
  cell); `thead` and `tfoot` repeated on every page; rows tied by `rowspan` kept together; rows taller than a page
  split across pages; `align`/`margin: auto` centring.
- **Floats:** `float: left | right` (images, figures, pull quotes, drop caps, floated card rows); line boxes shrink
  around floats and move below them when too narrow; `clear`, the clearfix `::after` pattern, boxes with `overflow:
  hidden` / `display: flow-root` / flex sit beside floats and contain their own floats.
- **Positioning:** `position: relative | sticky` (offsets), `absolute` (against the nearest positioned ancestor,
  `top/right/bottom/left/inset`, stretched or shrink-to-fit sizes, % heights), `fixed` (relative to the page area and
  repeated on every page when anchored with `top`/`bottom`: running headers and footers), `z-index` for stacking.
- **Inline-level boxes:** `inline-block`, `inline-flex`, `inline-table` and images sit on the text baseline.
  `display: contents` is supported.
- **Lists:** all common `list-style-type`s and custom string markers; **tables:** auto column widths, `colspan`,
  repeated `thead`, zebra striping through selectors (see Tables below); **images:** PNG (alpha), JPEG, GIF, BMP, WebP and SVG from any source (see below); block images taller than a
  page continue across pages at full width (scaled to one page only where breaking is impossible);
  clickable links; page breaks (`break-before/after`).

## Tailwind, Bootstrap, viewport and web fonts

- **Tailwind CSS:** pages that style themselves with Tailwind's in-browser script (`cdn.tailwindcss.com`, `@tailwindcss/browser`
  or a local `tailwind.js`) print styled: the converter generates the CSS for the classes the page uses, with Tailwind 3.4's
  default theme, preflight, responsive and structural variants, `!important`, negative and arbitrary values. Hover/focus
  variants never apply on paper; a custom `tailwind.config` is not read.
- **Bootstrap:** pages linking Bootstrap 5 or 4 from a public CDN (jsdelivr, cdnjs, unpkg, stackpath, maxcdn) use a
  bundled copy, so they print styled offline and with remote loading off; a self-hosted `bootstrap.min.css` is used when
  it exists, the bundle otherwise. Bootstrap JavaScript is not run: components print in the state their classes give
  (`.show`, `.active`, `checked`). Bootstrap Icons need the icon font (`-remote`). Bootstrap 3 is not bundled.
- **CSS libraries:** `-css` (API field `cssLib`, the "CSS libraries" checkboxes in the web UI) chooses them.
  - **Nothing chosen (`auto`, the default):** every library the page uses is detected and applied, several at once if
    the page mixes them, so the PDF looks as it does in the browser. A library is found by the Tailwind script or
    Bootstrap link (as above), or, when the page doesn't load it, by class names only it uses: `btn-primary`,
    `col-md-6`, `justify-content-between`, `data-bs-*` for Bootstrap (4 and 5 told apart by `ml-2`/`ms-2`,
    `badge-primary` ...), and `md:flex`, `w-[300px]`, `text-gray-800`, `px-6` for Tailwind. Classes the page's own CSS
    defines, generic names (`container`, `row`, `card`) and names both libraries have (`ml-2`, `w-100`, `shadow-sm`)
    don't count; each library needs a few distinct classes and at least an eighth of the other's count, so a few
    look-alikes on a page of the other library are ignored.
  - **One or more chosen** (`-css bootstrap5,tailwind`, or one `cssLib` field per checked box): exactly those are
    applied, even if the page doesn't load them, and detection is off. Bootstrap loads before the page's own styles
    (so they still override it) and replaces any Bootstrap link the page has, whatever its version; with both versions
    chosen, 5 comes after 4. Tailwind is generated for the page's classes, after the page's `<head>` styles.
  - **`none`:** no bundled library; CDN links are then downloaded only with `-remote`.
  - The libraries applied, and how each was chosen (`selected`, `script`, `link`, `classes`), are reported in the
    `X-CSS-Lib` response header (`bootstrap5; classes, tailwind; script`), in the web UI and by the CLI.
- **Viewport width:** `-viewport 1280` (or the API field `viewport`) lays the page out as in a 1280px browser window and
  scales it onto the paper, so `md:`/`lg:` media queries apply. Without it the layout width is the paper width, as when
  printing from a browser.
- **Web fonts and kerning:** `@font-face` fonts (including Google Fonts with `-remote`) are downloaded only when used;
  TrueType kerning (GPOS pair adjustment or `kern` table) is applied to measuring and drawing text, as browsers do.
  Variable fonts are used at their default instance (Chromium applies optical sizing, so line breaks can differ).

## Features matching dompdf

- **External resources:** `<link rel="stylesheet">`, `@import`, `<base href>`, and `url()` in CSS (relative to the
  stylesheet), `<img src>`, `@font-face src`, `list-style-image`. Sources: `data:` URIs, an uploaded ZIP bundle, local files
  below the document's folder (CLI; never outside it), and http(s) when enabled (`-remote` / `-allow-remote` plus the
  checkbox in the web UI). Remote fetching refuses private, loopback and link-local addresses (checked after DNS),
  limits size (10 MB), time and redirects. External images are downloaded in parallel (8 at a time); `srcset` and
  `<picture><source>` pick the sharpest supported candidate. Images that cannot be used (404, unknown host, an HTML
  page or AVIF where an image was expected) are listed with the reason in the CLI output and in the web page.
  Remote loading is off unless enabled.
- **SVG:** inline `<svg>` and SVG files in `<img>`, CSS backgrounds and `content: url()` — rendered as vectors: rect
  (rounded), circle, ellipse, line, polyline, polygon, path (all commands incl. arcs), groups, `transform`, nested `svg`,
  `viewBox` + `preserveAspectRatio`, `use`/`symbol`, linear and radial gradients, `clip-path`, text (`text-anchor`, `tspan`),
  embedded raster images, `<style>` rules, presentation attributes, opacity, dashes, caps and joins, markers
  (`marker-start|mid|end`, `orient="auto|auto-start-reverse"`, `markerUnits`, `viewBox`), patterns (`patternUnits`,
  `patternContentUnits`, `patternTransform`, `viewBox`), masks (luminance and `mask-type="alpha"`, as PDF soft masks) and
  filters: `feDropShadow`, `feGaussianBlur`, `feOffset`, `feFlood`, `feColorMatrix`, `feComponentTransfer` and `feMerge`
  (blur is approximated with layered translucent copies; other filter primitives are ignored).
- **Page numbers anywhere:** `counter(page)` and `counter(pages)` in generated content (e.g. fixed headers/footers) and in
  `@page` margin boxes (`@top-left|center|right`, `@bottom-*`, `@left-*`, `@right-*`, corners), which also accept
  `content: url(logo.svg)`.
- **Links and metadata:** `href="#id"` jumps to elements with that id or `<a name>`; `<meta name="author|description|
  keywords|generator">` and `<title>` fill the PDF properties.
- **Password protection:** open password, owner password, and print / copy / modify permissions (PDF standard security
  handler, 128-bit RC4, like dompdf's CPDF backend).
- **More images and controls:** BMP (1–32 bit), WebP (lossy, lossless, alpha, first frame of animations;
  decoded in pure Go, bit-exact with libwebp), `list-style-image`, and form controls printed as they look: text inputs
  (value or placeholder), passwords, checkboxes, radio buttons, selects, buttons, colour swatches.

## Roadmap / not yet supported

Not supported in Grid: subgrid, masonry, named lines in placement. Approximations: shadow blur is built from stacked translucent layers (PDF has no blur filter),
transparent gradient stops are blended towards white, `text-shadow` blur is drawn as a softer solid shadow.
Out of scope: JavaScript, WOFF2 (needs Brotli), PHP-style inline scripts, and shaping of complex scripts
(Arabic, Indic), whose letters render unjoined.

## Tests

```bash
go test ./...
```

A whole site can be checked too: every `.html` file below the folder is converted and must render (blank pages or
errors fail; missing local files are reported, and fail with `HTML2PDF_STRICT=1`):

```bash
HTML2PDF_SITE=path/to/site go test ./internal/converter -run TestSite -v
```
