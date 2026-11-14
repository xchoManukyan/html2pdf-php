// Package render turns an HTML document into a PDF: it resolves styles,
// lays out text, lists, tables and images, and paginates the result.
package render

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/css"
	"github.com/xchoManukyan/html2pdf/converter/internal/fonts"
	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
	"github.com/xchoManukyan/html2pdf/converter/internal/pdf"
	"github.com/xchoManukyan/html2pdf/converter/internal/resource"
	"github.com/xchoManukyan/html2pdf/converter/internal/ttf"
)

// Options controls the page setup. CSS @page rules in the document take
// precedence over PageSize/Landscape/MarginMM, as in browsers.
type Options struct {
	PageSize    string  // A3, A4, A5, Letter, Legal (default A4)
	Landscape   bool    // swap width and height
	MarginMM    float64 // page margin on every side, in millimetres (default 15)
	PageNumbers bool    // print "Page X of Y" in the bottom margin
	// ViewportWidth lays the page out as if the browser window were this many
	// CSS pixels wide, then scales it to fit the paper (like "fit to page
	// width" in a browser): 1280 prints a site's desktop layout, 375 its
	// mobile layout. 0: the paper width itself, as when printing.
	ViewportWidth float64
	NoSystemFonts bool // use only the built-in PDF fonts (and @font-face)
	// CSSLib picks the CSS frameworks. "" or "auto": detect them from the
	// page (scripts, links and class names; several can be found). A list
	// such as "bootstrap5,tailwind": apply exactly those, even when the page
	// does not load them (the chosen Bootstrap replaces the page's Bootstrap
	// links). "none": no bundled framework.
	CSSLib string
	// Info, when set, receives facts about the conversion, such as the CSS
	// framework that was detected.
	Info *Info

	// Loader fetches stylesheets, images and fonts the document refers to
	// (nil: only data: URIs). BaseURL locates the document itself, e.g.
	// "local:/index.html" or "https://example.com/page.html".
	Loader  *resource.Loader
	BaseURL string

	// Encrypt protects the PDF with passwords and permissions (nil: none).
	Encrypt *pdf.Encryption
}

// ErrEmpty is returned for documents without any content.
var ErrEmpty = errors.New("the HTML document is empty")

type pageSetup struct {
	w, h   float64
	margin [4]float64
}

// fitViewport returns the virtual page on which a viewport of vw CSS pixels
// fills the content width, and the factor that maps it back to the paper.
func (p pageSetup) fitViewport(vw float64) (pageSetup, float64) {
	content := p.w - p.margin[1] - p.margin[3]
	if vw <= 0 || content <= 0 {
		return p, 1
	}
	s := content / (vw * 0.75)
	v := pageSetup{h: p.h / s}
	for i, m := range p.margin {
		v.margin[i] = m / s
	}
	// exact content width, so "(min-width: 1280px)" holds at a 1280px viewport
	v.w = vw*0.75 + v.margin[1] + v.margin[3]
	return v, s
}

func (p pageSetup) env() css.Env {
	return css.Env{Width: p.w - p.margin[1] - p.margin[3], Height: p.h - p.margin[0] - p.margin[2]}
}

// Convert renders an HTML document (UTF-8) to PDF bytes.
func Convert(src string, opt Options) (out []byte, err error) {
	if strings.TrimSpace(src) == "" {
		return nil, ErrEmpty
	}
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("render failed: %v", r)
		}
	}()

	size, ok := pdf.PageSizes[strings.ToUpper(opt.PageSize)]
	if !ok {
		size, ok = customSize(opt.PageSize)
	}
	if !ok {
		size = pdf.PageSizes["A4"]
	}
	setup := pageSetup{w: size[0], h: size[1]}
	if opt.Landscape {
		setup.w, setup.h = setup.h, setup.w
	}
	marginMM := opt.MarginMM
	if marginMM <= 0 || marginMM > 60 {
		marginMM = 15
	}
	m := marginMM * 72 / 25.4
	setup.margin = [4]float64{m, m, m, m}

	root := htmlparse.Parse(src)
	baseURL := opt.BaseURL
	if baseURL == "" {
		baseURL = "local:/document.html"
	}
	baseURL = documentBase(root, baseURL)
	fetch := func(abs string) ([]byte, string, error) {
		if opt.Loader == nil {
			return resource.DecodeDataURI(abs)
		}
		return opt.Loader.Fetch(abs)
	}
	paper := setup
	setup, scale := paper.fitViewport(opt.ViewportWidth)
	forget := func(abs string) {
		if opt.Loader != nil {
			opt.Loader.Forget(abs)
		}
	}
	cssText, detected := collectStylesheets(root, baseURL, fetch, forget, setup.env(), opt.CSSLib)
	if opt.Info != nil {
		opt.Info.CSS = detected
	}
	parseAuthor := func(env css.Env) *css.Stylesheet {
		ss := css.NewStylesheet()
		for _, t := range cssText {
			ss.Add(t, env)
		}
		return ss
	}
	author := parseAuthor(setup.env())
	if len(author.Page) > 0 {
		if applyPageRule(&paper, author.Page) {
			setup, scale = paper.fitViewport(opt.ViewportWidth)
			cssText, _ = collectStylesheets(root, baseURL, fetch, forget, setup.env(), opt.CSSLib) // media queries depend on the page size
			author = parseAuthor(setup.env())
		}
	}

	doc := pdf.New(setup.w, setup.h)
	doc.Scale = scale
	doc.Encrypt = opt.Encrypt
	documentMeta(root, doc)
	if t := root.Find("title"); t != nil {
		doc.Title = strings.Join(strings.Fields(t.TextContent()), " ")
	}

	e := &engine{
		doc:          doc,
		top:          setup.margin[0],
		bottom:       setup.h - setup.margin[2],
		left:         setup.margin[3],
		right:        setup.w - setup.margin[1],
		contentWidth: setup.w - setup.margin[1] - setup.margin[3],
		rootSize:     12,
		ua:           css.ParseStylesheet(userAgentCSS, setup.env()),
		author:       author,
		styles:       map[*htmlparse.Node]*style{},
		pseudoStyles: map[pseudoKey]*style{},
		hasPseudo:    map[string]bool{},
		counters:     map[string]int{},
		pictures:     map[string]*picture{},
		imgPictures:  map[*htmlparse.Node]*picture{},
		env:          setup.env(),
		inlineSVG:    map[*htmlparse.Node]*picture{},
		formNodes:    map[*htmlparse.Node]*htmlparse.Node{},
		loader:       opt.Loader,
		baseURL:      baseURL,
		docFonts:     fonts.NewDB(),
		chains:       map[string]*fontChain{},
	}
	if opt.NoSystemFonts {
		e.sysFonts = fonts.NewDB()
	} else {
		e.sysFonts = fonts.System()
	}
	for _, p := range []string{"before", "after", "marker"} {
		e.hasPseudo[p] = e.ua.HasPseudo(p) || e.author.HasPseudo(p)
	}
	for _, ff := range author.FontFaces {
		e.registerFontFace(ff)
	}

	if opt.Loader != nil && opt.Loader.AllowRemote {
		opt.Loader.Prefetch(e.imageRefs(root, cssText))
	}
	e.rootFrame = &cbFrame{}
	e.pages = []*pdf.Page{doc.AddPage()}
	e.y = e.top
	e.atTop = true

	base := rootStyle()
	e.layoutChildren(root, base, e.left, e.contentWidth)

	// Out-of-flow boxes: absolute boxes without a positioned ancestor use
	// the first page area; fixed boxes repeat on every page.
	for _, it := range e.rootFrame.pending {
		e.layoutAbsolute(it, cbRect{page: 0, x: e.left, y: e.top, w: e.contentWidth, h: e.bottom - e.top})
	}
	e.layoutFixed()
	if len(author.PageBoxes) > 0 {
		baseSt := base
		if h := root.Find("html"); h != nil {
			baseSt = e.computeStyle(h, base)
			if b := root.Find("body"); b != nil {
				baseSt = e.computeStyle(b, baseSt)
			}
		}
		e.layoutMarginBoxes(author.PageBoxes, setup.w, setup.h, setup.margin, baseSt)
	}

	// Page background from <html>/<body>.
	var pageBG *pdf.Color
	if h := root.Find("html"); h != nil {
		hst := e.computeStyle(h, base)
		pageBG = hst.bg
		if b := root.Find("body"); b != nil && pageBG == nil {
			pageBG = e.computeStyle(b, hst).bg
		}
	}
	for i, p := range e.pages {
		if pageBG != nil {
			p.Background(0, 0, setup.w, setup.h, *pageBG, -1)
		}
		if opt.PageNumbers {
			label := fmt.Sprintf("Page %d of %d", i+1, len(e.pages))
			f := doc.Std(pdf.Helvetica)
			w := f.Width(label, 9)
			p.Text((setup.w-w)/2, setup.h-setup.margin[2]/2, pdf.TextStyle{Font: f, Size: 9,
				Color: pdf.Color{R: 0.4, G: 0.4, B: 0.4}}, label)
		}
	}
	return doc.Bytes(), nil
}

// customSize parses "210x297" (millimetres, width x height), limited to the
// largest page PDF readers accept (5080 mm).
func customSize(s string) ([2]float64, bool) {
	w, h, ok := strings.Cut(strings.ToLower(strings.TrimSpace(s)), "x")
	if !ok {
		return [2]float64{}, false
	}
	wv, err1 := strconv.ParseFloat(strings.TrimSpace(w), 64)
	hv, err2 := strconv.ParseFloat(strings.TrimSpace(h), 64)
	if err1 != nil || err2 != nil || wv < 25 || hv < 25 || wv > 5080 || hv > 5080 {
		return [2]float64{}, false
	}
	return [2]float64{wv * 72 / 25.4, hv * 72 / 25.4}, true
}

// applyPageRule applies @page { size; margin }. It reports whether the page
// geometry changed.
func applyPageRule(p *pageSetup, decls []css.Decl) bool {
	before := *p
	ctx := css.LengthCtx{Em: 12, Rem: 12}
	for _, d := range decls {
		v := strings.ToLower(strings.TrimSpace(d.Value))
		switch d.Prop {
		case "size":
			fields := strings.Fields(v)
			var lengths []float64
			orient := ""
			for _, f := range fields {
				if s, ok := pdf.PageSizes[strings.ToUpper(f)]; ok {
					p.w, p.h = s[0], s[1]
				} else if f == "landscape" || f == "portrait" {
					orient = f
				} else if l, ok := css.ParseLength(f, ctx); ok && l > 0 {
					lengths = append(lengths, l)
				}
			}
			switch len(lengths) {
			case 1:
				p.w, p.h = lengths[0], lengths[0]
			case 2:
				p.w, p.h = lengths[0], lengths[1]
			}
			if orient == "landscape" && p.w < p.h || orient == "portrait" && p.w > p.h {
				p.w, p.h = p.h, p.w
			}
		case "margin":
			vals := strings.Fields(v)
			switch len(vals) {
			case 1:
				vals = []string{vals[0], vals[0], vals[0], vals[0]}
			case 2:
				vals = []string{vals[0], vals[1], vals[0], vals[1]}
			case 3:
				vals = []string{vals[0], vals[1], vals[2], vals[1]}
			}
			if len(vals) == 4 {
				for i, val := range vals {
					ctx.Percent = p.w
					if l, ok := css.ParseLength(val, ctx); ok && l >= 0 {
						p.margin[i] = l
					}
				}
			}
		case "margin-top", "margin-right", "margin-bottom", "margin-left":
			i := map[string]int{"margin-top": 0, "margin-right": 1, "margin-bottom": 2, "margin-left": 3}[d.Prop]
			if l, ok := css.ParseLength(v, ctx); ok && l >= 0 {
				p.margin[i] = l
			}
		}
	}
	// keep at least 1 inch of content in both directions
	if p.w-p.margin[1]-p.margin[3] < 72 || p.h-p.margin[0]-p.margin[2] < 72 {
		p.margin = before.margin
	}
	return *p != before
}

// registerFontFace loads an @font-face rule. Fonts are taken from data:
// URIs (TrueType, OpenType-TrueType, WOFF) or local() system fonts.
func (e *engine) registerFontFace(decls []css.Decl) {
	var family, src string
	weight, italic := 400, false
	for _, d := range decls {
		v := strings.TrimSpace(d.Value)
		switch d.Prop {
		case "font-family":
			family = strings.Trim(v, `"' `)
		case "src":
			src = v
		case "font-weight":
			if w, ok := parseWeight(strings.ToLower(strings.Fields(v + " ")[0]), 400); ok {
				weight = w
			}
		case "font-style":
			italic = strings.Contains(strings.ToLower(v), "italic") || strings.Contains(strings.ToLower(v), "oblique")
		}
	}
	if family == "" || src == "" {
		return
	}
	var urls []string
	for _, part := range splitTopLevelComma(src) {
		part = strings.TrimSpace(part)
		lower := strings.ToLower(part)
		switch {
		case strings.HasPrefix(lower, "local(") && len(urls) == 0:
			end := strings.IndexByte(part, ')')
			if end < 0 {
				continue
			}
			name := strings.Trim(part[6:end], `"' `)
			if face := e.sysFonts.Match(name, weight, italic); face != nil {
				e.docFonts.Add(&fonts.Face{Family: family, Weight: weight, Italic: italic, Path: face.Path, Index: face.Index, Data: face.Data})
				return
			}
		case strings.HasPrefix(lower, "url("):
			if uri, ok := cssURL(part); ok {
				urls = append(urls, uri)
			}
		}
	}
	if len(urls) == 0 {
		return
	}
	// Downloaded on first use: a family often declares many weights and
	// styles of which a page uses a few.
	e.docFonts.Add(&fonts.Face{Family: family, Weight: weight, Italic: italic, Stretch: 5, Fetch: func() ([]byte, error) {
		for _, uri := range urls {
			data, _, err := e.fetch(uri)
			if err != nil || len(data) == 0 {
				continue
			}
			if len(data) > 4 && string(data[:4]) == "wOFF" {
				if data, err = ttf.DecodeWOFF(data); err != nil {
					continue
				}
			}
			if _, err := ttf.Parse(data, 0); err == nil {
				return data, nil
			}
		}
		return nil, errors.New("no usable font source for " + family)
	}})
}

// cssURL returns the address in a leading url(...) token, quoted or not:
// `url("a.ttf") format('truetype')` -> a.ttf.
func cssURL(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 5 || !strings.EqualFold(s[:4], "url(") {
		return "", false
	}
	s = strings.TrimLeft(s[4:], " \t\n")
	if s != "" && (s[0] == '"' || s[0] == '\'') {
		end := strings.IndexByte(s[1:], s[0])
		if end < 0 {
			return "", false
		}
		return s[1 : end+1], true
	}
	end := strings.IndexByte(s, ')')
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(s[:end]), true
}

// splitTopLevelComma splits on commas outside parentheses and quotes.
func splitTopLevelComma(s string) []string {
	var out []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
