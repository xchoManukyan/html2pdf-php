package render

import (
	"strconv"
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/css"
	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
	"github.com/xchoManukyan/html2pdf/converter/internal/pdf"
)

// userAgentCSS gives every element its browser-like default look
// (modelled on the HTML standard's rendering section).
const userAgentCSS = `
html, body, div, section, article, header, footer, nav, main, aside, figure,
figcaption, form, fieldset, address, details, summary, center, dl, dt, legend,
hgroup, caption, picture, search { display: block }
head, script, style, title, meta, link, noscript, template, base, canvas,
video, audio, iframe, object, embed, map, option, optgroup, datalist,
area, param, source, track, dialog:not([open]), [hidden], [popover] { display: none }
p { display: block; margin: 1em 0 }
h1 { display: block; font-size: 2em; font-weight: bold; margin: 0.67em 0 }
h2 { display: block; font-size: 1.5em; font-weight: bold; margin: 0.83em 0 }
h3 { display: block; font-size: 1.17em; font-weight: bold; margin: 1em 0 }
h4 { display: block; font-size: 1em; font-weight: bold; margin: 1.33em 0 }
h5 { display: block; font-size: 0.83em; font-weight: bold; margin: 1.67em 0 }
h6 { display: block; font-size: 0.67em; font-weight: bold; margin: 2.33em 0 }
h1, h2, h3, h4, h5, h6 { break-after: avoid }
tr, img, figure { break-inside: avoid }
:is(article, aside, nav, section) h1 { font-size: 1.5em; margin: 0.83em 0 }
ul, ol, menu { display: block; margin: 1em 0; padding-left: 40px }
ul, menu { list-style-type: disc }
ol { list-style-type: decimal }
li { display: list-item }
:is(ul, ol, menu) :is(ul, ol, menu) { margin-top: 0; margin-bottom: 0 }
:is(ul, ol, menu) ul { list-style-type: circle }
:is(ul, ol, menu) :is(ul, ol, menu) ul { list-style-type: square }
dl { margin: 1em 0 }
dd { display: block; margin-left: 40px }
blockquote { display: block; margin: 1em 40px }
figure { margin: 1em 40px }
pre, listing, xmp, plaintext { display: block; font-family: monospace; white-space: pre; margin: 1em 0 }
textarea { display: block; font-family: monospace; white-space: pre-wrap; border: 1px solid #767676; padding: 2px }
code, kbd, samp, tt { font-family: monospace }
b, strong, th, dt, legend, summary { font-weight: bold }
i, em, cite, var, dfn, address { font-style: italic }
u, ins { text-decoration: underline }
s, strike, del { text-decoration: line-through }
small { font-size: smaller }
big { font-size: larger }
sub { vertical-align: sub; font-size: smaller }
sup { vertical-align: super; font-size: smaller }
mark { background-color: yellow; color: black }
q::before { content: open-quote }
q::after { content: close-quote }
a:any-link { color: #0000ee; text-decoration: underline }
hr { display: block; margin: 0.5em 0; border-style: inset; border-width: 1px; color: gray; border-color: #c0c0c0 }
table { display: table; border-color: gray; border-collapse: separate; border-spacing: 2px; box-sizing: border-box }
tr { display: table-row }
td, th { display: table-cell }
thead { display: table-header-group }
tbody { display: table-row-group }
tfoot { display: table-footer-group }
caption { text-align: center }
td, th { padding: 1px }
th { text-align: center }
center { text-align: center }
fieldset { border: 2px groove #c0c0c0; padding: 0.35em 0.75em 0.625em; margin: 0 2px }
input, select, button { display: inline-block; font-family: Arial, sans-serif; font-size: 13.333px; font-weight: normal;
  font-style: normal; color: black; border: 1px solid #767676; border-radius: 2px; padding: 1px 2px;
  background-color: white; line-height: normal; text-indent: 0; text-align: left }
input, select { white-space: pre }
input[type=hidden] { display: none }
button, input[type=submit], input[type=button], input[type=reset] { background-color: #efefef; padding: 1px 6px; text-align: center }
input[type=checkbox], input[type=radio] { width: 13px; height: 13px; padding: 0; margin: 3px 3px 3px 4px;
  text-align: center; line-height: 1; font-size: 10px }
input[type=radio] { border-radius: 50% }
textarea { display: inline-block; border-radius: 2px }
`

func init() {
	css.RegisterSupported("color", "background", "background-color", "font", "font-size", "font-family",
		"font-weight", "font-style", "text-align", "text-decoration", "text-transform", "text-indent",
		"line-height", "letter-spacing", "word-spacing", "white-space", "list-style", "list-style-type",
		"display", "margin", "padding", "border", "width", "height", "min-width", "max-width",
		"vertical-align", "visibility", "content", "counter-reset", "counter-increment", "page-break-before",
		"page-break-after", "break-before", "break-after")
}

type style struct {
	// inherited properties
	family        string
	weight        int
	italic        bool
	size          float64
	color         pdf.Color
	align         string
	valign        string
	listType      string
	noAppearance  bool // appearance: none (the page draws the control itself)
	transform     string
	link          string
	lineHeight    float64 // multiple of font size; 0 = "normal"
	ws            string  // white-space: normal, nowrap, pre, pre-wrap, pre-line
	underline     bool
	strike        bool
	letterSpacing float64
	wordSpacing   float64
	textIndent    float64
	visible       bool
	shift         float64 // baseline shift (vertical-align sub/super), points
	vars          map[string]string

	// box properties (not inherited)
	display      string
	margin       [4]float64 // top right bottom left
	padding      [4]float64
	borderWidth  [4]float64
	borderStyle  [4]string
	borderColor  [4]pdf.Color
	borderColSet [4]bool
	bg           *pdf.Color
	widthExpr    string
	minWidthExpr string
	maxWidthExpr string
	heightExpr   string
	breakBefore  bool
	breakAfter   bool
	borderBox    bool    // box-sizing: border-box
	autoMargin   [4]bool // margin: auto per side

	// painting
	alpha          float64   // effective opacity (product of ancestors' opacity)
	radiusExpr     [4]string // border-*-radius: tl, tr, br, bl
	bgImages       []string  // url(...) or gradient functions, top layer first
	bgRepeat       []string
	bgPos          []string
	bgSize         []string
	shadows        []boxShadow
	textShadow     *boxShadow
	outlineW       float64
	outlineStyle   string
	outlineColor   pdf.Color
	outlineColSet  bool
	outlineOffset  float64
	overflowHidden bool
	ellipsis       bool // text-overflow: ellipsis

	// floats and positioning
	float      string    // "left", "right" or ""
	clear      string    // "left", "right", "both" or ""
	position   string    // static, relative, absolute, fixed, sticky
	offsetExpr [4]string // top, right, bottom, left
	zIndex     int
	relX       float64 // horizontal offset of a relatively positioned inline (inherited by its content)

	// tables (inherited like in CSS)
	borderCollapse bool
	spacingExpr    string // border-spacing
	captionSide    string
	tableFixed     bool // table-layout: fixed (not inherited)

	// fragmentation and transforms
	avoidInside     bool   // break-inside: avoid
	avoidAfter      bool   // break-after: avoid
	orphans, widows int    // inherited, default 2
	xform           string // CSS transform (not inherited)
	xformOrigin     string

	// flexbox (container)
	flexDir      string // row, row-reverse, column, column-reverse
	flexWrap     string // nowrap, wrap, wrap-reverse
	justify      string // start, end, center, space-between, space-around, space-evenly
	alignItems   string // stretch, start, end, center
	alignContent string
	rowGapExpr   string
	colGapExpr   string
	// flexbox (item)
	grow, shrink float64
	basisExpr    string
	alignSelf    string // auto or an alignItems value
	// grid (container)
	gridCols, gridRows         string   // grid-template-columns / -rows
	gridAreas                  []string // grid-template-areas rows
	gridAutoCols, gridAutoRows string
	gridAutoFlow               string
	justifyItems               string
	// grid (item)
	gridRowStart, gridRowEnd string
	gridColStart, gridColEnd string
	justifySelf              string
	order                    int

	listImage string // list-style-image url(...), inherited

	inlineBox *style // nearest inline ancestor-or-self with a background/border

	content    string // generated content of ::before / ::after
	hasContent bool
}

func (s *style) pre() bool { return s.ws == "pre" || s.ws == "pre-wrap" || s.ws == "break-spaces" }

// bw returns the effective border width of side i (0 top, 1 right, 2 bottom, 3 left).
func (s *style) bw(i int) float64 {
	switch s.borderStyle[i] {
	case "", "none", "hidden":
		return 0
	}
	return s.borderWidth[i]
}

func (s *style) hasBorder() bool {
	return s.bw(0) > 0 || s.bw(1) > 0 || s.bw(2) > 0 || s.bw(3) > 0
}

func (s *style) isBlock() bool {
	switch s.display {
	case "block", "list-item", "table", "flex", "grid", "table-caption", "flow-root":
		return true
	}
	return false
}

// isAtomicInline reports inline-level boxes laid out as one unit in a line.
func (s *style) isAtomicInline() bool {
	switch s.display {
	case "inline-block", "inline-flex", "inline-table", "inline-grid":
		return true
	}
	return false
}

func (s *style) isFlex() bool { return s.display == "flex" || s.display == "inline-flex" }

func (s *style) isGrid() bool { return s.display == "grid" || s.display == "inline-grid" }

// extrasH is the horizontal padding + border; extrasV the vertical one.
func (s *style) extrasH() float64 { return s.padding[1] + s.padding[3] + s.bw(1) + s.bw(3) }
func (s *style) extrasV() float64 { return s.padding[0] + s.padding[2] + s.bw(0) + s.bw(2) }

func rootStyle() *style {
	return &style{family: "serif", weight: 400, size: 12, align: "left", ws: "normal",
		display: "block", visible: true, vars: map[string]string{}, alpha: 1, orphans: 2, widows: 2}
}

var inheritedProps = map[string]bool{
	"color": true, "font": true, "font-family": true, "font-size": true, "font-weight": true,
	"font-style": true, "line-height": true, "text-align": true, "text-indent": true,
	"text-transform": true, "letter-spacing": true, "word-spacing": true, "white-space": true,
	"list-style": true, "list-style-type": true, "visibility": true, "quotes": true,
}

func (e *engine) lengthCtx(s *style) css.LengthCtx {
	return css.LengthCtx{Em: s.size, Rem: e.rootSize, Percent: e.contentWidth,
		VW: e.contentWidth, VH: e.bottom - e.top}
}

// cascade returns the declarations for an element (or pseudo-element) in
// increasing priority: UA, HTML hints, author, inline style, then the
// !important author and inline declarations.
func (e *engine) cascade(n *htmlparse.Node, pseudo string) []css.Decl {
	var normal, important []css.Decl
	add := func(ds []css.Decl, canImportant bool) {
		for _, d := range ds {
			if d.Important && canImportant {
				important = append(important, d)
			} else {
				normal = append(normal, d)
			}
		}
	}
	add(e.ua.Match(n, pseudo), false)
	if pseudo == "" {
		add(presentational(n), false)
	}
	add(e.author.Match(n, pseudo), true)
	if pseudo == "" {
		add(css.ParseDecls(n.Attr("style")), true)
	}
	return append(normal, important...)
}

func inheritFrom(parent *style) *style {
	s := &style{
		family: parent.family, weight: parent.weight, italic: parent.italic,
		size: parent.size, color: parent.color, align: parent.align, valign: parent.valign,
		listType: parent.listType, transform: parent.transform, link: parent.link,
		lineHeight: parent.lineHeight, ws: parent.ws, underline: parent.underline, strike: parent.strike,
		letterSpacing: parent.letterSpacing, wordSpacing: parent.wordSpacing, textIndent: parent.textIndent,
		visible: parent.visible, shift: parent.shift, vars: parent.vars, display: "inline",
		flexDir: "row", flexWrap: "nowrap", justify: "normal", alignItems: "stretch", alignContent: "stretch",
		justifyItems: "stretch", justifySelf: "auto",
		shrink: 1, basisExpr: "auto", alignSelf: "auto",
		alpha:     parent.alpha,
		listImage: parent.listImage,
		position:  "static", relX: parent.relX,
		borderCollapse: parent.borderCollapse, spacingExpr: parent.spacingExpr, captionSide: parent.captionSide,
		orphans: parent.orphans, widows: parent.widows,
	}
	for i := range s.borderWidth {
		s.borderWidth[i] = 2.25 // CSS "medium"
	}
	return s
}

// computeStyle resolves the style of an element from its parent style.
func (e *engine) computeStyle(n *htmlparse.Node, parent *style) *style {
	if s, ok := e.styles[n]; ok {
		return s
	}
	s := inheritFrom(parent)
	if n.Tag == "a" {
		if href := n.Attr("href"); href != "" {
			s.link = href
		}
	}
	e.applyCascade(s, parent, e.cascade(n, ""))
	setInlineBox(s, parent)
	if n.Tag == "html" {
		e.rootSize = s.size
	}
	if n.Tag == "textarea" && s.heightExpr == "" {
		textareaHeight(n, s)
	}
	e.styles[n] = s
	return s
}

// textareaHeight gives a textarea without a CSS height the height of its
// rows attribute (default 2), as browsers do.
func textareaHeight(n *htmlparse.Node, s *style) {
	rows, err := strconv.Atoi(strings.TrimSpace(n.Attr("rows")))
	if err != nil || rows < 1 {
		rows = 2
	}
	lh := s.lineHeight
	if lh <= 0 {
		lh = 1.2 // "normal"
	}
	h := float64(rows) * lh * s.size
	if s.borderBox {
		h += s.extrasV()
	}
	s.heightExpr = strconv.FormatFloat(h, 'f', 3, 64) + "pt"
}

func setInlineBox(s, parent *style) {
	if s.position == "relative" || s.position == "sticky" {
		if !s.isBlock() {
			dx, dy := s.relOffset(nil)
			s.shift = parent.shift + dy
			s.relX = parent.relX + dx
		}
	}
	if s.isBlock() || s.isAtomicInline() {
		s.relX = 0
	}
	switch {
	case s.isAtomicInline():
		s.inlineBox = nil
	case s.isBlock() || s.display == "table-cell" || s.display == "table-row":
		s.shift = 0
		s.inlineBox = nil
	case s.bg != nil || s.hasBorder():
		s.inlineBox = s
	default:
		s.inlineBox = parent.inlineBox
	}
}

// pseudoStyle returns the style of n::before / n::after, or nil when the
// pseudo-element generates nothing.
func (e *engine) pseudoStyle(n *htmlparse.Node, parent *style, which string) *style {
	if n.Type != htmlparse.ElementNode || !e.hasPseudo[which] {
		return nil
	}
	key := pseudoKey{n, which}
	if s, ok := e.pseudoStyles[key]; ok {
		return s
	}
	var s *style
	if decls := e.cascade(n, which); len(decls) > 0 {
		s = inheritFrom(parent)
		e.applyCascade(s, parent, decls)
		setInlineBox(s, parent)
		if !s.hasContent || s.display == "none" {
			s = nil
		} else {
			s.content = e.evalContent(s.content, n, s)
		}
	}
	e.pseudoStyles[key] = s
	return s
}

func (e *engine) applyCascade(s, parent *style, decls []css.Decl) {
	// Custom properties first: they are inherited and used by var().
	ownVars := false
	for _, d := range decls {
		if strings.HasPrefix(d.Prop, "--") {
			if !ownVars {
				m := make(map[string]string, len(s.vars)+4)
				for k, v := range s.vars {
					m[k] = v
				}
				s.vars, ownVars = m, true
			}
			s.vars[d.Prop] = d.Value
		}
	}
	resolved := decls[:0:0]
	for _, d := range decls {
		if strings.HasPrefix(d.Prop, "--") {
			continue
		}
		if strings.Contains(d.Value, "var(") {
			v, ok := substituteVars(d.Value, s.vars, 0)
			if !ok {
				continue // invalid at computed-value time: behaves like unset
			}
			d.Value = v
		}
		resolved = append(resolved, d)
	}
	// Font size first: other em lengths depend on it.
	for _, d := range resolved {
		switch d.Prop {
		case "font-size":
			if strings.EqualFold(d.Value, "inherit") || strings.EqualFold(d.Value, "unset") {
				s.size = parent.size
			} else if strings.EqualFold(d.Value, "initial") {
				s.size = 12
			} else if sz, ok := e.parseFontSize(strings.ToLower(d.Value), parent); ok {
				s.size = sz
			}
		case "font":
			e.applyFontShorthand(s, d.Value, parent, true)
		}
	}
	for _, d := range resolved {
		if d.Prop == "font-size" {
			continue
		}
		e.applyDecl(s, d, parent)
	}
	for i := range s.borderColor {
		if !s.borderColSet[i] {
			s.borderColor[i] = s.color
		}
	}
}

// substituteVars replaces var(--name, fallback) references.
func substituteVars(v string, vars map[string]string, depth int) (string, bool) {
	if depth > 16 {
		return "", false
	}
	for {
		i := strings.Index(v, "var(")
		if i < 0 {
			return v, true
		}
		depthP, end := 0, -1
		for j := i + 3; j < len(v); j++ {
			if v[j] == '(' {
				depthP++
			} else if v[j] == ')' {
				depthP--
				if depthP == 0 {
					end = j
					break
				}
			}
		}
		if end < 0 {
			return "", false
		}
		inner := v[i+4 : end]
		name, fallback, hasFallback := strings.Cut(inner, ",")
		name = strings.TrimSpace(name)
		val, ok := vars[name]
		if !ok || strings.TrimSpace(val) == "" {
			if !hasFallback {
				return "", false
			}
			val = strings.TrimSpace(fallback)
		}
		val, ok = substituteVars(val, vars, depth+1)
		if !ok {
			return "", false
		}
		v = v[:i] + val + v[end+1:]
	}
}

var fontSizeKeywords = map[string]float64{
	"xx-small": 6.75, "x-small": 7.5, "small": 9.75, "medium": 12,
	"large": 13.5, "x-large": 18, "xx-large": 24, "xxx-large": 36,
}

func (e *engine) parseFontSize(v string, parent *style) (float64, bool) {
	if sz, ok := fontSizeKeywords[v]; ok {
		return sz, true
	}
	switch v {
	case "smaller":
		return parent.size / 1.2, true
	case "larger":
		return parent.size * 1.2, true
	}
	ctx := e.lengthCtx(parent)
	ctx.Percent = parent.size
	sz, ok := css.ParseLength(v, ctx)
	if !ok || sz <= 0 {
		return 0, false
	}
	return min(sz, 1000), true
}

func parseWeight(v string, parent int) (int, bool) {
	switch v {
	case "normal":
		return 400, true
	case "bold":
		return 700, true
	case "bolder":
		switch {
		case parent < 350:
			return 400, true
		case parent < 550:
			return 700, true
		}
		return 900, true
	case "lighter":
		switch {
		case parent < 550:
			return 100, true
		case parent < 750:
			return 400, true
		}
		return 700, true
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 1000 {
		return n, true
	}
	return 0, false
}

// applyFontShorthand handles "font: italic bold 12px/1.5 Arial, sans-serif".
func (e *engine) applyFontShorthand(s *style, raw string, parent *style, sizeOnly bool) {
	tokens := strings.Fields(raw)
	italic, weight := false, 400
	for i, t := range tokens {
		lt := strings.ToLower(t)
		switch {
		case lt == "italic" || lt == "oblique":
			italic = true
		case lt == "normal" || lt == "small-caps" || strings.HasSuffix(lt, "condensed") || strings.HasSuffix(lt, "expanded"):
		case func() bool { _, ok := parseWeight(lt, parent.weight); return ok }():
			weight, _ = parseWeight(lt, parent.weight)
		default:
			sizePart, lh, hasLH := strings.Cut(lt, "/")
			sz, ok := e.parseFontSize(sizePart, parent)
			if !ok {
				return // invalid shorthand
			}
			s.size = sz
			if sizeOnly {
				return
			}
			s.italic, s.weight = italic, weight
			s.lineHeight = 0
			if hasLH {
				s.lineHeight = e.parseLineHeight(lh, s)
			}
			if fam := strings.TrimSpace(strings.Join(tokens[i+1:], " ")); fam != "" {
				s.family = fam
			}
			return
		}
	}
}

func (e *engine) parseLineHeight(v string, s *style) float64 {
	v = strings.TrimSpace(v)
	if v == "normal" {
		return 0
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
		return f
	}
	ctx := e.lengthCtx(s)
	ctx.Percent = s.size
	if l, ok := css.ParseLength(v, ctx); ok && l >= 0 && s.size > 0 {
		return l / s.size
	}
	return s.lineHeight
}

// color resolves a CSS color. Fully transparent colors return ok=false;
// partial transparency is kept in A and painted with PDF transparency.
func (e *engine) color(v string, s *style) (pdf.Color, bool) {
	if strings.EqualFold(strings.TrimSpace(v), "currentcolor") {
		return s.color, true
	}
	c, ok := css.ParseColor(v)
	if !ok || c.A == 0 {
		return pdf.Color{}, false
	}
	out := pdf.Color{R: c.R, G: c.G, B: c.B}
	if c.A < 1 {
		out.A = c.A
	}
	return out, true
}

// paint applies the element's opacity to a color.
func (s *style) paint(c pdf.Color) pdf.Color {
	a := c.Alpha() * s.alpha
	if a >= 0.999 {
		c.A = 0
	} else {
		c.A = max(a, 0.001)
	}
	return c
}

var listTypes = map[string]bool{
	"disc": true, "circle": true, "square": true, "decimal": true, "none": true,
	"lower-alpha": true, "upper-alpha": true, "lower-latin": true, "upper-latin": true,
	"lower-roman": true, "upper-roman": true, "decimal-leading-zero": true, "lower-greek": true,
	"armenian": true, "upper-armenian": true, "lower-armenian": true, "georgian": true,
}

func (e *engine) applyDecl(s *style, d css.Decl, parent *style) {
	raw := strings.TrimSpace(d.Value)
	v := strings.ToLower(raw)
	switch v {
	case "inherit":
		copyProp(s, parent, d.Prop)
		return
	case "initial":
		copyProp(s, rootStyle(), d.Prop)
		if d.Prop == "display" {
			s.display = "inline"
		}
		return
	case "unset", "revert", "revert-layer":
		if inheritedProps[d.Prop] {
			copyProp(s, parent, d.Prop)
		} else {
			copyProp(s, rootStyle(), d.Prop)
		}
		return
	}
	ctx := e.lengthCtx(s)
	length := func(val string) (float64, bool) { return css.ParseLength(val, ctx) }
	if e.applyPaint(s, d.Prop, raw, v, parent) {
		return
	}

	switch d.Prop {
	case "color":
		if c, ok := e.color(v, parent); ok {
			s.color = c
		}
	case "background-color":
		if v == "none" || v == "transparent" {
			s.bg = nil
			return
		}
		for _, tok := range splitValue(v) {
			if c, ok := e.color(tok, s); ok {
				s.bg = &c
				return
			} else if _, isColor := css.ParseColor(tok); isColor {
				s.bg = nil // fully transparent
				return
			}
		}
	case "font":
		e.applyFontShorthand(s, raw, parent, false)
	case "font-weight":
		if w, ok := parseWeight(v, parent.weight); ok {
			s.weight = w
		}
	case "font-style":
		s.italic = v == "italic" || strings.HasPrefix(v, "oblique")
	case "font-family":
		s.family = raw
	case "text-align":
		switch v {
		case "left", "start", "-webkit-left":
			s.align = "left"
		case "right", "end", "-webkit-right":
			s.align = "right"
		case "center", "-webkit-center":
			s.align = "center"
		case "justify":
			s.align = v
		}
	case "vertical-align":
		switch v {
		case "top", "middle", "bottom":
			s.valign = v
		case "text-top":
			s.valign = "top"
		case "text-bottom":
			s.valign = "bottom"
		case "baseline":
			s.valign = "top"
			s.shift = parent.shift
		case "sub":
			s.shift = parent.shift + parent.size*0.2
		case "super":
			s.shift = parent.shift - parent.size*0.35
		default:
			lctx := ctx
			lctx.Percent = s.size * max(s.lineHeight, 1.2)
			if l, ok := css.ParseLength(v, lctx); ok {
				s.shift = parent.shift - l
			}
		}
	case "text-decoration", "text-decoration-line":
		if strings.Contains(v, "none") {
			s.underline, s.strike = false, false
		}
		if strings.Contains(v, "underline") {
			s.underline = true
		}
		if strings.Contains(v, "line-through") {
			s.strike = true
		}
	case "text-transform":
		s.transform = v
	case "text-indent":
		if l, ok := length(strings.Fields(v + " ")[0]); ok {
			s.textIndent = l
		}
	case "letter-spacing":
		if v == "normal" {
			s.letterSpacing = 0
		} else if l, ok := length(v); ok {
			s.letterSpacing = l
		}
	case "word-spacing":
		if v == "normal" {
			s.wordSpacing = 0
		} else if l, ok := length(v); ok {
			s.wordSpacing = l
		}
	case "white-space", "white-space-collapse":
		switch v {
		case "normal", "nowrap", "pre", "pre-wrap", "pre-line", "break-spaces":
			s.ws = v
		case "preserve":
			s.ws = "pre-wrap"
		case "collapse":
			s.ws = "normal"
		}
	case "text-wrap", "text-wrap-mode":
		if v == "nowrap" && s.ws == "normal" {
			s.ws = "nowrap"
		}
	case "line-height":
		s.lineHeight = e.parseLineHeight(v, s)
	case "list-style-type", "list-style":
		for _, tok := range splitValue(raw) {
			lt := strings.ToLower(tok)
			if listTypes[lt] {
				s.listType = lt
			} else if len(tok) >= 2 && (tok[0] == '"' || tok[0] == '\'') {
				s.listType = tok // custom marker string
			}
		}
	case "visibility":
		s.visible = v != "hidden" && v != "collapse"
	case "display":
		fields := strings.Fields(v)
		switch {
		case len(fields) == 0:
		case v == "contents":
			s.display = "contents"
		case strings.Contains(v, "list-item"):
			s.display = "list-item"
		case fields[0] == "inline" && len(fields) > 1 && fields[1] == "flow-root":
			s.display = "inline-block"
		case fields[0] == "block" && len(fields) > 1:
			s.display = map[string]string{"flex": "flex", "grid": "grid", "table": "table", "flow-root": "flow-root"}[fields[1]]
			if s.display == "" {
				s.display = "block"
			}
		default:
			s.display = fields[0]
		}
	case "margin":
		setBox(&s.margin, v, length)
		for i, val := range expandBox(splitValue(v)) {
			s.autoMargin[i] = val == "auto"
		}
	case "margin-top", "margin-right", "margin-bottom", "margin-left":
		setSide(&s.margin, d.Prop[len("margin-"):], v, length)
		s.autoMargin[sideIndex(d.Prop)] = v == "auto"
	case "margin-block":
		setPair(&s.margin, 0, 2, v, length)
	case "margin-inline":
		setPair(&s.margin, 3, 1, v, length)
		vals := splitValue(v)
		if len(vals) > 0 {
			s.autoMargin[3] = vals[0] == "auto"
			s.autoMargin[1] = vals[len(vals)-1] == "auto"
		}
	case "margin-block-start", "margin-block-end", "margin-inline-start", "margin-inline-end":
		side := logicalSide(d.Prop)
		setSide(&s.margin, side, v, length)
		s.autoMargin[map[string]int{"top": 0, "right": 1, "bottom": 2, "left": 3}[side]] = v == "auto"
	case "appearance", "-webkit-appearance", "-moz-appearance":
		s.noAppearance = v == "none"
	case "float":
		switch v {
		case "left", "inline-start":
			s.float = "left"
		case "right", "inline-end":
			s.float = "right"
		default:
			s.float = ""
		}
	case "clear":
		switch v {
		case "left", "right", "both":
			s.clear = v
		case "inline-start":
			s.clear = "left"
		case "inline-end":
			s.clear = "right"
		default:
			s.clear = ""
		}
	case "position":
		switch v {
		case "relative", "absolute", "fixed", "sticky", "static":
			s.position = v
		case "-webkit-sticky":
			s.position = "sticky"
		}
	case "top", "right", "bottom", "left":
		s.offsetExpr[sideIndex(d.Prop)] = v
	case "inset":
		if vals := expandBox(splitValue(v)); vals != nil {
			copy(s.offsetExpr[:], vals)
		}
	case "inset-block":
		f := splitValue(v)
		if len(f) > 0 {
			s.offsetExpr[0], s.offsetExpr[2] = f[0], f[len(f)-1]
		}
	case "inset-inline":
		f := splitValue(v)
		if len(f) > 0 {
			s.offsetExpr[3], s.offsetExpr[1] = f[0], f[len(f)-1]
		}
	case "border-collapse":
		s.borderCollapse = v == "collapse"
	case "border-spacing":
		s.spacingExpr = v
	case "table-layout":
		s.tableFixed = v == "fixed"
	case "caption-side":
		s.captionSide = v
	case "z-index":
		if n, err := strconv.Atoi(v); err == nil {
			s.zIndex = n
		}
	case "box-sizing":
		s.borderBox = v == "border-box"
	case "flex-direction":
		s.flexDir = v
	case "flex-wrap":
		s.flexWrap = v
	case "flex-flow":
		for _, tok := range strings.Fields(v) {
			if strings.HasPrefix(tok, "row") || strings.HasPrefix(tok, "column") {
				s.flexDir = tok
			} else {
				s.flexWrap = tok
			}
		}
	case "justify-content":
		s.justify = normalizeAlign(v, "normal")
	case "align-items":
		s.alignItems = normalizeAlign(v, "stretch")
	case "align-self":
		if v == "auto" {
			s.alignSelf = "auto"
		} else {
			s.alignSelf = normalizeAlign(v, "stretch")
		}
	case "align-content":
		s.alignContent = normalizeAlign(v, "stretch")
	case "place-content":
		f := strings.Fields(v)
		s.alignContent = normalizeAlign(f[0], "stretch")
		s.justify = normalizeAlign(f[len(f)-1], "normal")
	case "place-items":
		f := strings.Fields(v)
		s.alignItems = normalizeAlign(f[0], "stretch")
		s.justifyItems = normalizeAlign(f[len(f)-1], "stretch")
	case "justify-items":
		s.justifyItems = normalizeAlign(strings.TrimPrefix(v, "legacy "), "stretch")
	case "justify-self":
		if v == "auto" {
			s.justifySelf = "auto"
		} else {
			s.justifySelf = normalizeAlign(v, "stretch")
		}
	case "place-self":
		f := strings.Fields(v)
		if f[0] == "auto" {
			s.alignSelf = "auto"
		} else {
			s.alignSelf = normalizeAlign(f[0], "stretch")
		}
		if last := f[len(f)-1]; last == "auto" {
			s.justifySelf = "auto"
		} else {
			s.justifySelf = normalizeAlign(last, "stretch")
		}
	case "grid-template-columns":
		s.gridCols = v
	case "grid-template-rows":
		s.gridRows = v
	case "grid-template-areas":
		s.gridAreas = parseAreaStrings(raw)
	case "grid-template", "grid":
		parseGridTemplate(s, raw)
	case "grid-auto-columns":
		s.gridAutoCols = v
	case "grid-auto-rows":
		s.gridAutoRows = v
	case "grid-auto-flow":
		s.gridAutoFlow = v
	case "grid-column":
		s.gridColStart, s.gridColEnd = splitSlash(v)
	case "grid-row":
		s.gridRowStart, s.gridRowEnd = splitSlash(v)
	case "grid-column-start":
		s.gridColStart = v
	case "grid-column-end":
		s.gridColEnd = v
	case "grid-row-start":
		s.gridRowStart = v
	case "grid-row-end":
		s.gridRowEnd = v
	case "grid-area":
		parts := strings.Split(v, "/")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		switch len(parts) {
		case 1:
			if isGridIdent(parts[0]) {
				s.gridRowStart, s.gridColStart, s.gridRowEnd, s.gridColEnd = parts[0], parts[0], parts[0], parts[0]
			} else {
				s.gridRowStart, s.gridColStart, s.gridRowEnd, s.gridColEnd = parts[0], "", "", ""
			}
		case 2:
			s.gridRowStart, s.gridColStart, s.gridRowEnd, s.gridColEnd = parts[0], parts[1], "", ""
		case 3:
			s.gridRowStart, s.gridColStart, s.gridRowEnd, s.gridColEnd = parts[0], parts[1], parts[2], ""
		default:
			s.gridRowStart, s.gridColStart, s.gridRowEnd, s.gridColEnd = parts[0], parts[1], parts[2], parts[3]
		}
	case "gap", "grid-gap":
		f := splitValue(v)
		if len(f) > 0 {
			s.rowGapExpr, s.colGapExpr = f[0], f[len(f)-1]
		}
	case "row-gap", "grid-row-gap":
		s.rowGapExpr = v
	case "column-gap", "grid-column-gap":
		s.colGapExpr = v
	case "flex":
		s.grow, s.shrink, s.basisExpr = parseFlexShorthand(v)
	case "flex-grow":
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			s.grow = f
		}
	case "flex-shrink":
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			s.shrink = f
		}
	case "flex-basis":
		s.basisExpr = v
	case "order":
		if n, err := strconv.Atoi(v); err == nil {
			s.order = n
		}
	case "padding":
		setBox(&s.padding, v, length)
	case "padding-top", "padding-right", "padding-bottom", "padding-left":
		setSide(&s.padding, d.Prop[len("padding-"):], v, length)
	case "padding-block":
		setPair(&s.padding, 0, 2, v, length)
	case "padding-inline":
		setPair(&s.padding, 3, 1, v, length)
	case "padding-block-start", "padding-block-end", "padding-inline-start", "padding-inline-end":
		setSide(&s.padding, logicalSide(d.Prop), v, length)
	case "border":
		e.applyBorder(s, []int{0, 1, 2, 3}, v)
	case "border-top", "border-block-start":
		e.applyBorder(s, []int{0}, v)
	case "border-right", "border-inline-end":
		e.applyBorder(s, []int{1}, v)
	case "border-bottom", "border-block-end":
		e.applyBorder(s, []int{2}, v)
	case "border-left", "border-inline-start":
		e.applyBorder(s, []int{3}, v)
	case "border-block":
		e.applyBorder(s, []int{0, 2}, v)
	case "border-inline":
		e.applyBorder(s, []int{1, 3}, v)
	case "border-width":
		var w [4]float64
		copy(w[:], s.borderWidth[:])
		setBox(&w, v, borderLength(length))
		s.borderWidth = w
	case "border-style":
		if vals := expandBox(strings.Fields(v)); vals != nil {
			copy(s.borderStyle[:], vals)
		}
	case "border-color":
		for i, val := range expandBox(splitValue(v)) {
			if c, ok := e.color(val, s); ok {
				s.borderColor[i] = c
				s.borderColSet[i] = true
			}
		}
	case "border-top-width", "border-right-width", "border-bottom-width", "border-left-width":
		if l, ok := borderLength(length)(v); ok {
			s.borderWidth[sideIndex(d.Prop)] = l
		}
	case "border-top-style", "border-right-style", "border-bottom-style", "border-left-style":
		s.borderStyle[sideIndex(d.Prop)] = v
	case "border-top-color", "border-right-color", "border-bottom-color", "border-left-color":
		if c, ok := e.color(v, s); ok {
			i := sideIndex(d.Prop)
			s.borderColor[i], s.borderColSet[i] = c, true
		}
	case "width", "inline-size":
		s.widthExpr = v
	case "min-width", "min-inline-size":
		s.minWidthExpr = v
	case "max-width", "max-inline-size":
		s.maxWidthExpr = v
	case "height", "min-height", "block-size", "min-block-size":
		if l, ok := length(v); ok && !strings.Contains(v, "%") && (d.Prop == "height" || d.Prop == "block-size" || l > 0) {
			s.heightExpr = v
		}
	case "page-break-before", "break-before":
		s.breakBefore = v == "always" || v == "page" || v == "left" || v == "right" || v == "recto" || v == "verso"
	case "page-break-after", "break-after":
		s.breakAfter = v == "always" || v == "page" || v == "left" || v == "right" || v == "recto" || v == "verso"
		s.avoidAfter = v == "avoid" || v == "avoid-page"
	case "page-break-inside", "break-inside":
		s.avoidInside = v == "avoid" || v == "avoid-page"
	case "orphans":
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s.orphans = n
		}
	case "widows":
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s.widows = n
		}
	case "transform", "-webkit-transform":
		s.xform = v
	case "transform-origin", "-webkit-transform-origin":
		s.xformOrigin = v
	case "translate", "rotate", "scale":
		if v != "none" {
			s.xform = strings.TrimSpace(s.xform + " " + d.Prop + "(" + strings.Join(strings.Fields(v), ",") + ")")
		}
	case "content":
		s.content, s.hasContent = raw, v != "none" && v != "normal"
	case "counter-reset", "counter-set":
		e.counterOp(raw, 0, d.Prop == "counter-set")
	case "counter-increment":
		e.counterOp(raw, 1, false)
	}
}

// normalizeAlign maps the CSS alignment keywords onto start/end/center/
// stretch/space-*.
func normalizeAlign(v, normal string) string {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(v, "safe "), "unsafe "))
	switch v {
	case "normal":
		return normal
	case "legacy":
		return normal
	case "flex-start", "start", "self-start", "left", "baseline", "first baseline", "last baseline":
		return "start"
	case "flex-end", "end", "self-end", "right":
		return "end"
	case "center", "stretch", "space-between", "space-around", "space-evenly":
		return v
	}
	return normal
}

// parseFlexShorthand handles "flex: none | auto | <grow> [<shrink>] [<basis>]".
func parseFlexShorthand(v string) (grow, shrink float64, basis string) {
	switch v {
	case "none":
		return 0, 0, "auto"
	case "auto":
		return 1, 1, "auto"
	case "initial":
		return 0, 1, "auto"
	}
	grow, shrink, basis = 1, 1, "0%"
	nums := 0
	for _, tok := range strings.Fields(v) {
		if f, err := strconv.ParseFloat(tok, 64); err == nil && tok != "0px" {
			if nums == 0 {
				grow = f
			} else {
				shrink = f
			}
			nums++
		} else {
			basis = tok
		}
	}
	if nums == 0 { // "flex: 200px" -> 1 1 200px
		grow = 1
	}
	return
}

func sideIndex(prop string) int {
	switch {
	case strings.Contains(prop, "top"):
		return 0
	case strings.Contains(prop, "right"):
		return 1
	case strings.Contains(prop, "bottom"):
		return 2
	}
	return 3
}

func logicalSide(prop string) string {
	switch {
	case strings.HasSuffix(prop, "block-start"):
		return "top"
	case strings.HasSuffix(prop, "block-end"):
		return "bottom"
	case strings.HasSuffix(prop, "inline-start"):
		return "left"
	}
	return "right"
}

// copyProp implements "inherit"/"initial" for the properties we model.
func copyProp(dst, src *style, prop string) {
	switch {
	case prop == "color":
		dst.color = src.color
	case prop == "font":
		dst.family, dst.weight, dst.italic, dst.size, dst.lineHeight = src.family, src.weight, src.italic, src.size, src.lineHeight
	case prop == "font-family":
		dst.family = src.family
	case prop == "font-weight":
		dst.weight = src.weight
	case prop == "font-style":
		dst.italic = src.italic
	case prop == "line-height":
		dst.lineHeight = src.lineHeight
	case prop == "text-align":
		dst.align = src.align
	case prop == "text-transform":
		dst.transform = src.transform
	case prop == "text-indent":
		dst.textIndent = src.textIndent
	case prop == "letter-spacing":
		dst.letterSpacing = src.letterSpacing
	case prop == "word-spacing":
		dst.wordSpacing = src.wordSpacing
	case prop == "white-space":
		dst.ws = src.ws
	case prop == "visibility":
		dst.visible = src.visible
	case strings.HasPrefix(prop, "list-style"):
		dst.listType = src.listType
	case prop == "text-decoration" || prop == "text-decoration-line":
		dst.underline, dst.strike = src.underline, src.strike
	case prop == "background" || prop == "background-color":
		dst.bg = src.bg
	case strings.HasPrefix(prop, "margin"):
		dst.margin = src.margin
	case strings.HasPrefix(prop, "padding"):
		dst.padding = src.padding
	case strings.HasPrefix(prop, "border"):
		dst.borderWidth, dst.borderStyle, dst.borderColor, dst.borderColSet = src.borderWidth, src.borderStyle, src.borderColor, src.borderColSet
	case prop == "width":
		dst.widthExpr = src.widthExpr
	case prop == "height":
		dst.heightExpr = src.heightExpr
	case prop == "display":
		dst.display = src.display
	}
}

func borderLength(length func(string) (float64, bool)) func(string) (float64, bool) {
	return func(v string) (float64, bool) {
		switch v {
		case "thin":
			return 0.75, true
		case "medium":
			return 2.25, true
		case "thick":
			return 3.75, true
		}
		return length(v)
	}
}

var borderStyles = map[string]bool{
	"none": true, "hidden": true, "solid": true, "dashed": true, "dotted": true,
	"double": true, "groove": true, "ridge": true, "inset": true, "outset": true,
}

func (e *engine) applyBorder(s *style, sides []int, v string) {
	length := borderLength(func(val string) (float64, bool) { return css.ParseLength(val, e.lengthCtx(s)) })
	width, styleName := -1.0, "none"
	var col *pdf.Color
	transparent := false
	for _, tok := range splitValue(v) {
		switch {
		case borderStyles[tok]:
			styleName = tok
		default:
			if c, ok := e.color(tok, s); ok {
				col = &c
			} else if _, isColor := css.ParseColor(tok); isColor {
				transparent = true
			} else if l, ok := length(tok); ok {
				width = l
			}
		}
	}
	for _, i := range sides {
		s.borderStyle[i] = styleName
		if transparent {
			s.borderStyle[i] = "none"
		}
		s.borderWidth[i] = 2.25
		if width >= 0 {
			s.borderWidth[i] = width
		}
		if col != nil {
			s.borderColor[i] = *col
			s.borderColSet[i] = true
		} else {
			s.borderColSet[i] = false
		}
	}
}

// splitValue splits a CSS value on spaces, keeping (...) groups and quoted
// strings whole.
func splitValue(v string) []string {
	var out []string
	depth := 0
	start := -1
	var quote rune
	for i, r := range v {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '(':
			depth++
		case r == ')':
			depth--
		case (r == ' ' || r == ',' || r == '\t' || r == '\n') && depth == 0:
			if start >= 0 {
				out = append(out, v[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, v[start:])
	}
	return out
}

// expandBox turns 1-4 CSS box values into top/right/bottom/left.
func expandBox(vals []string) []string {
	switch len(vals) {
	case 1:
		return []string{vals[0], vals[0], vals[0], vals[0]}
	case 2:
		return []string{vals[0], vals[1], vals[0], vals[1]}
	case 3:
		return []string{vals[0], vals[1], vals[2], vals[1]}
	case 4:
		return vals
	}
	return nil
}

func setBox(box *[4]float64, v string, length func(string) (float64, bool)) {
	for i, val := range expandBox(splitValue(v)) {
		if l, ok := length(val); ok {
			box[i] = l
		} else if val == "auto" || val == "0" {
			box[i] = 0
		}
	}
}

func setPair(box *[4]float64, a, b int, v string, length func(string) (float64, bool)) {
	vals := splitValue(v)
	if len(vals) == 0 {
		return
	}
	if len(vals) == 1 {
		vals = append(vals, vals[0])
	}
	for k, idx := range []int{a, b} {
		if l, ok := length(vals[k]); ok {
			box[idx] = l
		} else if vals[k] == "auto" {
			box[idx] = 0
		}
	}
}

func setSide(box *[4]float64, side, v string, length func(string) (float64, bool)) {
	idx := map[string]int{"top": 0, "right": 1, "bottom": 2, "left": 3}[side]
	if l, ok := length(v); ok {
		box[idx] = l
	} else if v == "auto" {
		box[idx] = 0
	}
}

// resolveLength evaluates a width-like expression against the containing
// block width. ok is false for "auto"/unset.
func (e *engine) resolveLength(expr string, s *style, containing float64) (float64, bool) {
	if expr == "" || expr == "auto" || expr == "none" || expr == "fit-content" || expr == "max-content" || expr == "min-content" {
		return 0, false
	}
	ctx := e.lengthCtx(s)
	ctx.Percent = containing
	return css.ParseLength(expr, ctx)
}

// boxWidth returns the CSS width of an element (content box) for a given
// containing width, applying min-width/max-width.
func (e *engine) boxWidth(s *style, containing float64) (float64, bool) {
	w, ok := e.resolveLength(s.widthExpr, s, containing)
	if mx, okMax := e.resolveLength(s.maxWidthExpr, s, containing); okMax {
		if !ok {
			// max-width only limits auto widths that would be wider
			if containing > mx {
				w, ok = mx, true
			}
		} else if w > mx {
			w = mx
		}
	}
	if mn, okMin := e.resolveLength(s.minWidthExpr, s, containing); okMin && ok && w < mn {
		w = mn
	}
	return w, ok
}

func (e *engine) boxHeight(s *style) float64 {
	h, _ := e.resolveLength(s.heightExpr, s, 0)
	return h
}

// ---------------------------------------------------------------------
// Generated content and counters

func (e *engine) counterOp(v string, def int, set bool) {
	if strings.EqualFold(strings.TrimSpace(v), "none") {
		return
	}
	fields := strings.Fields(v)
	for i := 0; i < len(fields); i++ {
		name := fields[i]
		val := def
		if i+1 < len(fields) {
			if n, err := strconv.Atoi(fields[i+1]); err == nil {
				val = n
				i++
			}
		}
		if def == 0 || set {
			e.counters[name] = val
		} else {
			e.counters[name] += val
		}
	}
}

// evalContent turns a `content` value into text.
func (e *engine) evalContent(v string, n *htmlparse.Node, s *style) string {
	var sb strings.Builder
	for _, tok := range splitValue(v) {
		lt := strings.ToLower(tok)
		switch {
		case len(tok) >= 2 && (tok[0] == '"' || tok[0] == '\''):
			sb.WriteString(unescapeCSSString(tok[1 : len(tok)-1]))
		case strings.HasPrefix(lt, "attr("):
			sb.WriteString(n.Attr(strings.ToLower(strings.TrimSpace(tok[5 : len(tok)-1]))))
		case strings.HasPrefix(lt, "url("):
			// an image in generated content (resolved by collectString)
			sb.WriteString("\x00img:" + urlOf(tok) + "\x00")
		case lt == "open-quote":
			sb.WriteString("“")
		case lt == "close-quote":
			sb.WriteString("”")
		case strings.HasPrefix(lt, "counter(") || strings.HasPrefix(lt, "counters("):
			args := strings.Split(tok[strings.IndexByte(tok, '(')+1:len(tok)-1], ",")
			name := strings.TrimSpace(args[0])
			styleName := "decimal"
			if strings.HasPrefix(lt, "counters(") {
				if len(args) > 2 {
					styleName = strings.TrimSpace(args[2])
				}
			} else if len(args) > 1 {
				styleName = strings.TrimSpace(args[1])
			}
			if name == "page" || name == "pages" {
				// resolved per page when the text is laid out
				sb.WriteString("\x00" + name + ":" + strings.ToLower(styleName) + "\x00")
			} else {
				sb.WriteString(formatCounter(e.counters[name], strings.ToLower(styleName)))
			}
		}
	}
	return sb.String()
}

func unescapeCSSString(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			sb.WriteByte(s[i])
			continue
		}
		j := i + 1
		for j < len(s) && j-i <= 6 && strings.IndexByte("0123456789abcdefABCDEF", s[j]) >= 0 {
			j++
		}
		if j > i+1 {
			v, _ := strconv.ParseUint(s[i+1:j], 16, 32)
			if v == 0xA {
				sb.WriteByte('\n')
			} else {
				sb.WriteRune(rune(v))
			}
			if j < len(s) && s[j] == ' ' {
				j++
			}
			i = j - 1
		} else {
			sb.WriteByte(s[i+1])
			i++
		}
	}
	return sb.String()
}

func formatCounter(n int, styleName string) string {
	switch styleName {
	case "none":
		return ""
	case "lower-alpha", "lower-latin":
		return alpha(n, 'a')
	case "upper-alpha", "upper-latin":
		return alpha(n, 'A')
	case "lower-roman":
		return strings.ToLower(roman(n))
	case "upper-roman":
		return roman(n)
	case "decimal-leading-zero":
		return leftPad(strconv.Itoa(n))
	case "lower-greek":
		if n >= 1 && n <= 24 {
			return string(rune('α' + n - 1 + boolInt(n >= 18)))
		}
	case "disc":
		return "•"
	case "circle":
		return "◦"
	case "square":
		return "▪"
	}
	return strconv.Itoa(n)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

var fontTagSizes = map[string]string{"1": "x-small", "2": "small", "3": "medium", "4": "large", "5": "x-large", "6": "xx-large", "7": "xxx-large"}

func pxOrPercent(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasSuffix(v, "%") {
		return v
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return v + "px"
	}
	return ""
}

// presentational maps legacy HTML attributes (align, bgcolor, border, ...)
// to CSS declarations.
func presentational(n *htmlparse.Node) []css.Decl {
	var d []css.Decl
	add := func(p, v string) {
		if v != "" {
			d = append(d, css.Decl{Prop: p, Value: v})
		}
	}
	switch n.Tag {
	case "font":
		add("color", n.Attr("color"))
		if sz, ok := fontTagSizes[strings.TrimPrefix(n.Attr("size"), "+")]; ok {
			add("font-size", sz)
		}
		add("font-family", n.Attr("face"))
	case "body":
		add("background-color", n.Attr("bgcolor"))
		add("color", n.Attr("text"))
	case "table":
		if b, err := strconv.Atoi(n.Attr("border")); err == nil && b > 0 {
			add("border", strconv.Itoa(b)+"px outset #808080")
		}
		add("background-color", n.Attr("bgcolor"))
		add("width", pxOrPercent(n.Attr("width")))
		add("border-spacing", pxOrPercent(n.Attr("cellspacing")))
		switch n.Attr("align") {
		case "center":
			add("margin-left", "auto")
			add("margin-right", "auto")
		case "right":
			add("margin-left", "auto")
		}
	case "col", "colgroup":
		add("width", pxOrPercent(n.Attr("width")))
		add("background-color", n.Attr("bgcolor"))
		add("width", pxOrPercent(n.Attr("width")))
	case "tr", "thead", "tbody", "tfoot":
		add("background-color", n.Attr("bgcolor"))
		add("text-align", n.Attr("align"))
		add("vertical-align", n.Attr("valign"))
	case "td", "th":
		if t := ancestor(n, "table"); t != nil {
			if b, err := strconv.Atoi(t.Attr("border")); err == nil && b > 0 {
				add("border", "1px inset #808080")
			}
			if p := t.Attr("cellpadding"); p != "" {
				add("padding", pxOrPercent(p))
			}
		}
		add("background-color", n.Attr("bgcolor"))
		add("text-align", n.Attr("align"))
		add("vertical-align", n.Attr("valign"))
		add("width", pxOrPercent(n.Attr("width")))
		add("height", pxOrPercent(n.Attr("height")))
		if n.HasAttr("nowrap") {
			add("white-space", "nowrap")
		}
	case "img":
		add("width", pxOrPercent(n.Attr("width")))
		add("height", pxOrPercent(n.Attr("height")))
	case "hr":
		add("color", n.Attr("color"))
		add("width", pxOrPercent(n.Attr("width")))
	case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "caption":
		add("text-align", n.Attr("align"))
	case "ol":
		switch n.Attr("type") {
		case "a":
			add("list-style-type", "lower-alpha")
		case "A":
			add("list-style-type", "upper-alpha")
		case "i":
			add("list-style-type", "lower-roman")
		case "I":
			add("list-style-type", "upper-roman")
		case "1":
			add("list-style-type", "decimal")
		}
	case "ul", "li":
		if t := n.Attr("type"); t != "" {
			add("list-style-type", strings.ToLower(t))
		}
	}
	return d
}

func ancestor(n *htmlparse.Node, tag string) *htmlparse.Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Tag == tag {
			return p
		}
	}
	return nil
}
