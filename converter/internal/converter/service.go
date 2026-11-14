// Package converter holds the business rules of the HTML -> PDF service:
// input validation, character-set detection, option normalisation and
// output naming. It is independent of HTTP so it can be reused by the CLI.
package converter

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xchoManukyan/html2pdf/converter/internal/pdf"
	"github.com/xchoManukyan/html2pdf/converter/internal/render"
	"github.com/xchoManukyan/html2pdf/converter/internal/resource"
)

// MaxInputSize is the largest HTML document accepted (10 MB).
const MaxInputSize = 10 << 20

// MaxBundleSize is the largest ZIP bundle (HTML + assets) accepted.
const MaxBundleSize = 50 << 20

var (
	ErrTooLarge      = fmt.Errorf("the file is larger than %d MB", MaxInputSize>>20)
	ErrBadExtension  = errors.New("only .html, .htm and .zip files can be converted")
	ErrNotHTML       = errors.New("the file does not look like HTML")
	ErrEmptyDocument = errors.New("the document is empty")
)

// Request is one conversion job.
type Request struct {
	FileName string // original file name, used to name the PDF
	HTML     []byte // the document, or a ZIP bundle when FileName ends in .zip
	// Assets are files uploaded next to the document (stylesheets, images,
	// fonts), keyed by their path relative to it, e.g. "styles.css".
	Assets      map[string][]byte
	PageSize    string
	Landscape   bool
	MarginMM    float64
	PageNumbers bool
	// ViewportWidth (CSS px) lays the page out like a browser window of that
	// width, scaled to the paper. 0 = the paper width (print layout).
	ViewportWidth float64
	// CSSLib: empty or "auto" (detect), "none", or a comma separated list of
	// "tailwind", "bootstrap5", "bootstrap4".
	CSSLib string

	// BaseDir lets the document load local files below this directory
	// (CLI use). Empty: only bundled files and data: URIs.
	BaseDir string
	// DocPath is the document's path inside BaseDir ("html/page.html"), so
	// "../css/x.css" can reach files above its folder. Empty: BaseDir itself.
	DocPath string
	// AllowRemote enables downloading http(s) resources.
	AllowRemote bool

	// Password protection (empty passwords and all permissions: none).
	UserPassword  string
	OwnerPassword string
	DenyPrint     bool
	DenyCopy      bool
	DenyModify    bool
}

// Result is the converted document.
type Result struct {
	FileName string
	PDF      []byte
	Took     time.Duration
	// Missing lists referenced resources that could not be loaded.
	Missing []string
	// Failures gives the reason for each missing resource.
	Failures []resource.Failure
	// CSSLibs are the CSS frameworks applied and how each was chosen
	// ("selected", "script", "link" or "classes").
	CSSLibs []render.Detection
}

var allowedSizes = map[string]bool{"A3": true, "A4": true, "A5": true, "LETTER": true, "LEGAL": true}

// customSize matches "WIDTHxHEIGHT" in millimetres, e.g. "80x200" for receipts.
var customSize = regexp.MustCompile(`^\d{2,4}(\.\d+)?X\d{2,4}(\.\d+)?$`)

// Convert validates the request and renders it.
func Convert(req Request) (*Result, error) {
	loader := &resource.Loader{AllowRemote: req.AllowRemote, BaseDir: req.BaseDir}
	baseURL := "local:/" + filepath.ToSlash(filepath.Base(orDefault(req.FileName, "document.html")))
	if req.DocPath != "" {
		baseURL = "local:/" + strings.TrimPrefix(path.Clean("/"+req.DocPath), "/")
	}
	ext := strings.ToLower(filepath.Ext(req.FileName))
	html := req.HTML
	switch {
	case ext == ".zip":
		if len(html) > MaxBundleSize {
			return nil, fmt.Errorf("the ZIP file is larger than %d MB", MaxBundleSize>>20)
		}
		files, main, err := resource.ReadZip(html)
		if err != nil {
			return nil, err
		}
		loader.Files = files
		html = files[main]
		baseURL = "local:/" + main
	case len(req.Assets) > 0:
		total := len(html)
		for _, b := range req.Assets {
			total += len(b)
		}
		if total > MaxBundleSize {
			return nil, fmt.Errorf("the files are larger than %d MB together", MaxBundleSize>>20)
		}
		if len(html) > MaxInputSize {
			return nil, ErrTooLarge
		}
		if ext != ".html" && ext != ".htm" && ext != ".xhtml" {
			return nil, ErrBadExtension
		}
		loader.Files = req.Assets
		baseURL = "local:/" + strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(req.FileName)), "/")
	case len(html) > MaxInputSize:
		return nil, ErrTooLarge
	case req.FileName != "" && ext != ".html" && ext != ".htm" && ext != ".xhtml":
		return nil, ErrBadExtension
	}
	text := DecodeText(html)
	if strings.TrimSpace(text) == "" {
		return nil, ErrEmptyDocument
	}
	if looksBinary(text) {
		return nil, ErrNotHTML
	}

	size := strings.ToUpper(strings.TrimSpace(req.PageSize))
	if !allowedSizes[size] && !customSize.MatchString(size) {
		size = "A4"
	}
	margin := req.MarginMM
	if margin < 5 || margin > 50 {
		margin = 15
	}
	info := &render.Info{}
	opts := render.Options{
		PageSize:      size,
		Landscape:     req.Landscape,
		MarginMM:      margin,
		PageNumbers:   req.PageNumbers,
		ViewportWidth: viewport(req.ViewportWidth),
		CSSLib:        req.CSSLib,
		Loader:        loader,
		BaseURL:       baseURL,
		Info:          info,
	}
	if req.UserPassword != "" || req.OwnerPassword != "" || req.DenyPrint || req.DenyCopy || req.DenyModify {
		opts.Encrypt = &pdf.Encryption{
			UserPassword:  req.UserPassword,
			OwnerPassword: req.OwnerPassword,
			AllowPrint:    !req.DenyPrint,
			AllowCopy:     !req.DenyCopy,
			AllowModify:   !req.DenyModify,
			AllowAnnotate: !req.DenyModify,
		}
	}

	start := time.Now()
	out, err := render.Convert(text, opts)
	if err != nil {
		return nil, err
	}
	return &Result{FileName: OutputName(req.FileName), PDF: out, Took: time.Since(start), Missing: loader.Missing(), Failures: loader.Failures(),
		CSSLibs: info.CSS}, nil
}

// viewport keeps the layout width within sensible screen sizes.
func viewport(w float64) float64 {
	if w <= 0 {
		return 0
	}
	return max(240, min(3840, w))
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// OutputName turns "Report (final).html" into "Report (final).pdf" and
// strips characters that are unsafe in a download header.
func OutputName(in string) string {
	base := strings.TrimSuffix(filepath.Base(in), filepath.Ext(in))
	clean := strings.Map(func(r rune) rune {
		switch {
		case r < 32 || r == 127 || strings.ContainsRune(`"\/:*?<>|;`, r):
			return -1
		case r > 126:
			return '_' // keep the header plain ASCII
		}
		return r
	}, base)
	clean = strings.TrimSpace(clean)
	if clean == "" || clean == "." {
		clean = "document"
	}
	return clean + ".pdf"
}

// DecodeText returns the document as UTF-8. It honours UTF-8/UTF-16 byte
// order marks and falls back to Windows-1252 for legacy files.
func DecodeText(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		return string(b[3:])
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}):
		return decodeUTF16(b[2:], false)
	case bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		return decodeUTF16(b[2:], true)
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return decode1252(b)
}

func decodeUTF16(b []byte, bigEndian bool) string {
	var sb strings.Builder
	for i := 0; i+1 < len(b); i += 2 {
		var u uint16
		if bigEndian {
			u = uint16(b[i])<<8 | uint16(b[i+1])
		} else {
			u = uint16(b[i+1])<<8 | uint16(b[i])
		}
		r := rune(u)
		if u >= 0xD800 && u < 0xDC00 && i+3 < len(b) {
			var lo uint16
			if bigEndian {
				lo = uint16(b[i+2])<<8 | uint16(b[i+3])
			} else {
				lo = uint16(b[i+3])<<8 | uint16(b[i+2])
			}
			if lo >= 0xDC00 && lo < 0xE000 {
				r = (rune(u)-0xD800)<<10 + (rune(lo) - 0xDC00) + 0x10000
				i += 2
			}
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

var win1252 = [32]rune{
	'€', 0xFFFD, '‚', 'ƒ', '„', '…', '†', '‡', 'ˆ', '‰', 'Š', '‹', 'Œ', 0xFFFD, 'Ž', 0xFFFD,
	0xFFFD, '‘', '’', '“', '”', '•', '–', '—', '˜', '™', 'š', '›', 'œ', 0xFFFD, 'ž', 'Ÿ',
}

func decode1252(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		switch {
		case c < 0x80:
			sb.WriteByte(c)
		case c < 0xA0:
			sb.WriteRune(win1252[c-0x80])
		default:
			sb.WriteRune(rune(c))
		}
	}
	return sb.String()
}

func looksBinary(s string) bool {
	sample := s
	if len(sample) > 4096 {
		sample = sample[:4096]
	}
	return strings.IndexByte(sample, 0) >= 0
}
