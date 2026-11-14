package render

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/xchoManukyan/html2pdf/converter/internal/css"
	"github.com/xchoManukyan/html2pdf/converter/internal/fonts"
	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
	"github.com/xchoManukyan/html2pdf/converter/internal/pdf"
	"github.com/xchoManukyan/html2pdf/converter/internal/resource"
)

// engine lays the document out top to bottom, breaking pages as needed.
type engine struct {
	doc     *pdf.Document
	pages   []*pdf.Page
	pageIdx int

	top, bottom, left, right float64 // content area of a page
	contentWidth             float64
	rootSize                 float64 // font size of <html>, for rem

	y             float64 // current vertical position (top-left origin)
	pendingMargin float64 // collapsed margin waiting to be applied
	marginPos     float64 // largest positive margin in pendingMargin
	marginNeg     float64 // most negative margin in pendingMargin
	atTop         bool    // at the start of a page or cell: margins are dropped
	breakPending  bool    // a CSS page break is requested before the next content
	noBreak       bool    // inside a table row: never break pages
	dry           bool    // measuring only: draw nothing
	depth         int     // nesting depth, used to order backgrounds

	markers []marker
	lists   []*listCtx

	ua, author   *css.Stylesheet
	styles       map[*htmlparse.Node]*style
	pseudoStyles map[pseudoKey]*style
	hasPseudo    map[string]bool
	counters     map[string]int
	pictures     map[string]*picture
	imgPictures  map[*htmlparse.Node]*picture // chosen source of each <img>
	env          css.Env                      // media query environment (for <source media>)
	inlineSVG    map[*htmlparse.Node]*picture
	formNodes    map[*htmlparse.Node]*htmlparse.Node
	loader       *resource.Loader
	baseURL      string

	sysFonts, docFonts *fonts.DB
	chains             map[string]*fontChain

	lastBaseline float64      // baseline of the most recent line (for inline-block baselines)
	lineLog      [][2]float64 // line tops and heights recorded by an orphans/widows dry run

	floats     []floatBox // floats of the current block formatting context
	cbStack    []*cbFrame // containing blocks of positioned boxes
	rootFrame  *cbFrame   // initial containing block
	fixedItems []absItem  // position: fixed boxes, painted at the end
}

type pseudoKey struct {
	n     *htmlparse.Node
	which string
}

type listCtx struct {
	ordered bool
	index   int
}

type marker struct {
	text  string   // for numbered / custom markers
	pic   *picture // list-style-image
	shape string   // disc, circle, square
	st    *style
	x     float64 // left edge of the list item's content
}

func (e *engine) page() *pdf.Page { return e.pages[e.pageIdx] }

func (e *engine) newPage() {
	e.pageIdx++
	if e.pageIdx >= len(e.pages) {
		e.pages = append(e.pages, e.doc.AddPage())
	}
	e.y = e.top
	e.resetMargin()
	e.atTop = true
}

// resetMargin drops the pending collapsed margin.
func (e *engine) resetMargin() {
	e.pendingMargin, e.marginPos, e.marginNeg = 0, 0, 0
}

// addMargin collapses margin m with the adjoining pending margins: the
// result is the largest positive margin plus the most negative one (CSS
// 2.1 8.3.1), so a -16px row margin cancels a 16px gutter.
func (e *engine) addMargin(m float64) {
	if e.atTop {
		return
	}
	if m > 0 {
		e.marginPos = max(e.marginPos, m)
	} else {
		e.marginNeg = min(e.marginNeg, m)
	}
	e.pendingMargin = e.marginPos + e.marginNeg
}

// applyMargin is called right before content is placed.
func (e *engine) applyMargin() {
	if e.breakPending && !e.noBreak && !e.dry {
		e.breakPending = false
		if e.y > e.top+0.5 {
			e.newPage()
		}
	}
	e.y += e.pendingMargin
	e.resetMargin()
	e.atTop = false
}

// ensureSpace starts a new page when h points do not fit any more.
func (e *engine) ensureSpace(h float64) {
	if e.noBreak || e.dry {
		return
	}
	if e.y+h > e.bottom+0.01 && e.y > e.top+0.5 {
		e.newPage()
	}
}

// ---------------------------------------------------------------------
// Inline content collection

type itemKind int

const (
	kWord itemKind = iota
	kSpace
	kBreak
	kBlock
	kGap    // horizontal margin/border/padding of an inline box
	kAtomic // inline-block, inline-flex, inline image ...
	kFloat  // a floated box
	kClear  // clearance requested by generated content
	kAbs    // an absolutely or fixed positioned box (out of flow)
)

type item struct {
	kind itemKind
	text string
	st   *style
	node *htmlparse.Node
	w    float64 // width of a kGap
}

func isWS(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' }

func transformText(s, t string) string {
	switch t {
	case "uppercase":
		return strings.ToUpper(s)
	case "lowercase":
		return strings.ToLower(s)
	case "capitalize":
		r, size := utf8.DecodeRuneInString(s)
		return string(unicode.ToUpper(r)) + s[size:]
	}
	return s
}

// collect flattens the inline content of n into words, spaces, breaks and
// nested block boxes, including ::before / ::after content.
func (e *engine) collect(n *htmlparse.Node, st *style, items *[]item) {
	if before := e.pseudoStyle(n, st, "before"); before != nil {
		e.collectGenerated(before, items)
	}
	for idx, c := range n.Children {
		switch c.Type {
		case htmlparse.TextNode:
			text := c.Text
			if idx == 0 && (n.Tag == "pre" || n.Tag == "textarea" || n.Tag == "listing") {
				text = strings.TrimPrefix(strings.TrimPrefix(text, "\r"), "\n")
			}
			e.collectString(text, st, items)
		case htmlparse.ElementNode:
			cst := e.computeStyle(c, st)
			switch {
			case cst.display == "none":
			case cst.position == "absolute" || cst.position == "fixed":
				*items = append(*items, item{kind: kAbs, st: cst, node: c})
			case cst.float != "":
				*items = append(*items, item{kind: kFloat, st: cst, node: c})
			case c.Tag == "br":
				*items = append(*items, item{kind: kBreak, st: cst})
			case cst.display == "contents":
				e.collect(c, cst, items)
			case cst.isAtomicInline() || isReplaced(c) && !cst.isBlock():
				if isReplaced(c) && e.pictureFor(c) == nil {
					if alt := strings.TrimSpace(c.Attr("alt")); alt != "" {
						e.collectText(alt, cst, items)
					}
					continue
				}
				if fn, fst := e.formNode(c, cst); fn != nil {
					*items = append(*items, item{kind: kAtomic, node: fn, st: fst})
				} else {
					*items = append(*items, item{kind: kAtomic, node: c, st: cst})
				}
			case cst.isBlock() || isReplaced(c) || c.Tag == "hr" || (c.Tag == "table" && !cst.isAtomicInline()):
				// display:block inputs and selects (Bootstrap's .form-control)
				if fn, fst := e.formNode(c, cst); fn != nil {
					*items = append(*items, item{kind: kBlock, st: fst, node: fn})
					continue
				}
				*items = append(*items, item{kind: kBlock, st: cst, node: c})
			default:
				e.anchor(c, e.y+e.pendingMargin)
				e.collectInline(c, cst, st, items)
			}
		}
	}
	if after := e.pseudoStyle(n, st, "after"); after != nil {
		e.collectGenerated(after, items)
	}
}

// collectInline adds an inline element: its left margin/border/padding,
// its content, then its right side.
func (e *engine) collectInline(c *htmlparse.Node, cst, parent *style, items *[]item) {
	gap := func(w float64, st *style) {
		if w > 0.01 {
			*items = append(*items, item{kind: kGap, w: w, st: st})
		}
	}
	gap(cst.margin[3], parent)
	gap(cst.bw(3)+cst.padding[3], cst)
	e.collect(c, cst, items)
	gap(cst.padding[1]+cst.bw(1), cst)
	gap(cst.margin[1], parent)
}

func (e *engine) collectGenerated(ps *style, items *[]item) {
	if ps.clear != "" {
		*items = append(*items, item{kind: kClear, st: ps})
	}
	if ps.content == "" {
		return
	}
	// a pseudo-element with a box of its own (float, inline-block, or
	// padding/border/background) is laid out like an element holding the
	// text: Bootstrap's breadcrumb divider is a padded, floated ::before
	if strings.IndexByte(ps.content, 0) < 0 && (ps.float != "" || ps.isAtomicInline() ||
		(!ps.isBlock() && (ps.extrasH() > 0.01 || ps.margin[1] != 0 || ps.margin[3] != 0 || ps.bg != nil))) {
		node := &htmlparse.Node{Type: htmlparse.ElementNode, Tag: "#pseudo",
			Children: []*htmlparse.Node{{Type: htmlparse.TextNode, Text: ps.content}}}
		node.Children[0].Parent = node
		e.styles[node] = ps
		switch {
		case ps.float != "":
			*items = append(*items, item{kind: kFloat, st: ps, node: node})
		case ps.isAtomicInline():
			*items = append(*items, item{kind: kAtomic, st: ps, node: node})
		default:
			e.collectInline(node, ps, ps, items)
		}
		return
	}
	block := ps.isBlock()
	if block {
		*items = append(*items, item{kind: kBreak, st: ps, text: "soft"})
	}
	e.collectString(ps.content, ps, items)
	if block {
		*items = append(*items, item{kind: kBreak, st: ps, text: "soft"})
	}
}

// collectString splits text according to the white-space mode.
func (e *engine) collectString(text string, st *style, items *[]item) {
	if i := strings.Index(text, "\x00img:"); i >= 0 {
		// content: url(...) — an image inside generated content
		if end := strings.IndexByte(text[i+5:], 0); end >= 0 {
			e.collectString(text[:i], st, items)
			ref := text[i+5 : i+5+end]
			if e.loadImage(ref) != nil {
				node := &htmlparse.Node{Type: htmlparse.ElementNode, Tag: "img", Attrs: map[string]string{"src": ref}}
				ist := inheritFrom(st)
				e.styles[node] = ist
				*items = append(*items, item{kind: kAtomic, node: node, st: ist})
			}
			e.collectString(text[i+5+end+1:], st, items)
			return
		}
	}
	if strings.IndexByte(text, 0) >= 0 {
		text = e.pageCounters(text)
	}
	switch st.ws {
	case "pre", "pre-wrap", "break-spaces":
		e.collectPre(text, st, items)
	case "pre-line":
		for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
			if i > 0 {
				*items = append(*items, item{kind: kBreak, st: st})
			}
			e.collectText(line, st, items)
		}
	case "nowrap":
		collapsed := strings.Join(strings.FieldsFunc(text, isWS), " ")
		if text != "" && isWS(rune(text[0])) {
			*items = append(*items, item{kind: kSpace, st: st})
		}
		if collapsed != "" {
			*items = append(*items, item{kind: kWord, text: transformText(collapsed, st.transform), st: st})
		}
		if text != "" && isWS(rune(text[len(text)-1])) && collapsed != "" {
			*items = append(*items, item{kind: kSpace, st: st})
		}
	default:
		e.collectText(text, st, items)
	}
}

func (e *engine) collectText(text string, st *style, items *[]item) {
	start := -1
	for i, r := range text {
		if isWS(r) {
			if start >= 0 {
				*items = append(*items, item{kind: kWord, text: transformWord(text[start:i], st.transform), st: st})
				start = -1
			}
			if k := len(*items); k == 0 || (*items)[k-1].kind != kSpace {
				*items = append(*items, item{kind: kSpace, st: st})
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		*items = append(*items, item{kind: kWord, text: transformWord(text[start:], st.transform), st: st})
	}
}

func transformWord(w, t string) string { return transformText(w, t) }

func (e *engine) collectPre(text string, st *style, items *[]item) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\t", "        ")
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			*items = append(*items, item{kind: kBreak, st: st})
		}
		if line != "" {
			*items = append(*items, item{kind: kWord, text: line, st: st})
		}
	}
}

// ---------------------------------------------------------------------
// Line breaking

type frag struct {
	text      string
	st        *style
	w         float64
	space     float64         // width of the space before this fragment
	atomic    *htmlparse.Node // set for atomic inline boxes
	asc, desc float64         // atomic box extent above/below the baseline
}

type lineBuilder struct {
	e       *engine
	x, w    float64 // containing block
	st      *style  // style of the containing block
	frags   []frag
	width   float64
	pending bool
	lines   int

	// the current line box, narrowed by floats
	started      bool
	lineX, lineW float64
	deferred     []item // floats met in the middle of a line

	log     bool // record line boxes (orphans/widows dry run)
	forceAt int  // line index that must start a new page, -1 for none
}

// ensureLine opens a new line box: it finds the space left by floats,
// moving the line down below floats when even `need` points do not fit.
func (lb *lineBuilder) ensureLine(need float64) {
	if lb.started {
		return
	}
	lb.started = true
	lb.lineX, lb.lineW = lb.x, lb.w
	e := lb.e
	if len(e.floats) == 0 {
		return
	}
	above, below := e.vmetrics(lb.st)
	h := above + below
	if lb.lines == 0 {
		need += lb.st.textIndent
	}
	for i := 0; i < 100; i++ {
		y := e.y + e.pendingMargin
		l, w := e.span(y, h, lb.x, lb.w)
		lb.lineX, lb.lineW = l, w
		if w >= min(need, lb.w)-0.01 || (l == lb.x && w == lb.w) {
			return
		}
		nb := e.nextFloatBottom(y, h)
		if nb <= y {
			return
		}
		e.applyMargin()
		e.y = nb
	}
}

func (lb *lineBuilder) avail() float64 {
	if lb.lines == 0 {
		return lb.lineW - lb.st.textIndent
	}
	return lb.lineW
}

func (lb *lineBuilder) space() { lb.pending = true }

// gap adds non-breaking horizontal space (inline margins/padding).
func (lb *lineBuilder) gap(w float64, st *style) {
	lb.ensureLine(w)
	sp := 0.0
	if lb.pending && len(lb.frags) > 0 {
		sp = lb.e.spaceWidth(st)
	}
	lb.pending = false
	if len(lb.frags) > 0 && lb.width+sp+w > lb.avail()+0.01 {
		lb.emit(true)
		lb.ensureLine(w)
		sp = 0
	}
	lb.frags = append(lb.frags, frag{st: st, w: w, space: sp})
	lb.width += sp + w
}

func (lb *lineBuilder) word(text string, st *style) {
	e := lb.e
	ww := e.textWidth(st, text)
	lb.ensureLine(ww)
	sp := 0.0
	if lb.pending && len(lb.frags) > 0 {
		sp = e.spaceWidth(st)
	}
	lb.pending = false
	if len(lb.frags) > 0 && lb.width+sp+ww > lb.avail()+0.01 {
		lb.emit(true)
		lb.ensureLine(ww)
		sp = 0
	}
	// nowrap and pre text overflows instead of breaking; a clipped box
	// with text-overflow: ellipsis cuts it to fit and ends it with "…"
	noWrap := st.ws == "nowrap" || st.ws == "pre"
	if noWrap && lb.st.ellipsis && lb.st.overflowHidden && lb.width+sp+ww > lb.avail()+0.01 {
		text = e.fitEllipsis(st, text, lb.avail()-lb.width-sp)
		ww = e.textWidth(st, text)
	}
	// A single word wider than the line: split it by characters.
	for !noWrap && len(lb.frags) == 0 && ww > lb.avail()+0.01 && utf8.RuneCountInString(text) > 1 {
		cut := 0
		for i := range text {
			if i > 0 && e.textWidth(st, text[:i]) > lb.avail() {
				break
			}
			cut = i
		}
		if cut == 0 {
			_, cut = utf8.DecodeRuneInString(text)
		}
		piece := text[:cut]
		lb.frags = append(lb.frags, frag{text: piece, st: st, w: e.textWidth(st, piece)})
		lb.emit(false)
		text = text[cut:]
		ww = e.textWidth(st, text)
		lb.ensureLine(ww)
	}
	lb.frags = append(lb.frags, frag{text: text, st: st, w: ww, space: sp})
	lb.width += sp + ww
}

// finish ends the current line. A forced break (<br>) emits an empty line
// when nothing is pending.
func (lb *lineBuilder) finish(forced bool, st *style) {
	if len(lb.frags) > 0 {
		lb.emit(false)
	} else if forced {
		lb.ensureLine(0)
		lb.forcedBreak()
		lb.e.emitLine(nil, lb.lineX, lb.lineW, lb.st, false, st, lb.logFlag())
		lb.started = false
		lb.lines++
	}
	lb.pending = false
	lb.placeDeferred()
}

func (lb *lineBuilder) emit(wrapped bool) {
	indent := 0.0
	if lb.lines == 0 {
		indent = lb.st.textIndent
	}
	lb.forcedBreak()
	lb.e.emitLine(lb.frags, lb.lineX+indent, lb.lineW-indent, lb.st, wrapped && lb.st.align == "justify", nil, lb.logFlag())
	lb.frags = nil
	lb.width = 0
	lb.lines++
	lb.started = false
	lb.placeDeferred()
}

// float places a float now, or after the current line when the line has
// content already.
func (lb *lineBuilder) float(it item) {
	if len(lb.frags) > 0 {
		// CSS places the float on the current line when it fits next to the
		// content already there; the line box then shrinks around it.
		fs := blockify(it.st)
		outer := lb.e.shrinkToFit(it.node, fs, lb.w) + fs.margin[1] + fs.margin[3]
		if lb.avail()-lb.width < outer-0.01 {
			lb.deferred = append(lb.deferred, it)
			return
		}
		lb.e.placeFloat(it.node, it.st, lb.x, lb.w)
		lb.started = false
		lb.ensureLine(0)
		return
	}
	lb.started = false
	lb.e.placeFloat(it.node, it.st, lb.x, lb.w)
}

func (lb *lineBuilder) placeDeferred() {
	for len(lb.deferred) > 0 {
		it := lb.deferred[0]
		lb.deferred = lb.deferred[1:]
		lb.e.placeFloat(it.node, it.st, lb.x, lb.w)
	}
}

// emitLine places one line of text at the current position.
func (e *engine) emitLine(frags []frag, x, w float64, block *style, justify bool, empty *style, logFlag int) {
	// CSS inline formatting: the line box spans the highest "above" and the
	// lowest "below" of all inline boxes, including the block's strut.
	strut := block
	if empty != nil {
		strut = empty
	}
	above, below := e.vmetrics(strut)
	for _, f := range frags {
		a, b := e.vmetrics(f.st)
		if f.atomic != nil {
			a, b = f.asc, f.desc
		}
		above = max(above, a-f.st.shift)
		below = max(below, b+f.st.shift)
	}
	lh := above + below
	e.applyMargin()
	e.ensureSpace(lh)
	baseline := e.y + above
	e.lastBaseline = baseline
	if logFlag == 1 {
		e.lineLog = append(e.lineLog, [2]float64{e.y, lh})
	}

	if !e.dry {
		for _, m := range e.markers {
			e.drawMarker(m, baseline)
		}
	}
	e.markers = nil

	total := 0.0
	spaces := 0
	for i, f := range frags {
		total += f.w
		if i > 0 {
			total += f.space
			if f.space > 0 {
				spaces++
			}
		}
	}
	cx := x
	extra := 0.0
	switch {
	case justify && spaces > 0:
		extra = max((w-total)/float64(spaces), 0)
	case block.align == "center":
		cx += (w - total) / 2
	case block.align == "right":
		cx += w - total
	}

	if !e.dry {
		p := e.page()
		// Inline box backgrounds and borders (code, mark, badges, ...): one
		// painted span per run of fragments belonging to the same element.
		xs := make([]float64, len(frags))
		bx := cx
		for i, f := range frags {
			if i > 0 && f.space > 0 {
				bx += f.space + extra
			}
			xs[i] = bx
			bx += f.w
		}
		for i := 0; i < len(frags); {
			box := frags[i].st.inlineBox
			j := i + 1
			for j < len(frags) && frags[j].st.inlineBox == box {
				j++
			}
			if box != nil && box.visible && box.paints() {
				asc, desc, _ := e.chain(box).fonts[0].f.Metrics()
				top := baseline + box.shift - asc*box.size - box.padding[0] - box.bw(0)
				bot := baseline + box.shift + desc*box.size + box.padding[2] + box.bw(2)
				hasLeft := frags[i].text == "" && frags[i].atomic == nil && frags[i].st == box
				hasRight := frags[j-1].text == "" && frags[j-1].atomic == nil && frags[j-1].st == box
				cp := *box
				if !hasLeft {
					cp.borderStyle[3], cp.radiusExpr[0], cp.radiusExpr[3] = "", "", ""
				}
				if !hasRight {
					cp.borderStyle[1], cp.radiusExpr[1], cp.radiusExpr[2] = "", "", ""
				}
				e.paintBox(p, e.depth+1, xs[i], top, xs[j-1]+frags[j-1].w-xs[i], bot-top, &cp, true, true)
			}
			i = j
		}
		for i, f := range frags {
			if i > 0 && f.space > 0 {
				cx += f.space + extra
			}
			if f.atomic != nil {
				e.drawAtomic(f, cx, baseline)
				cx += f.w
				continue
			}
			if f.text == "" { // gap
				cx += f.w
				continue
			}
			bl := baseline + f.st.shift
			e.drawText(p, cx, bl, f.st, f.text)
			thick := max(f.st.size*0.06, 0.5)
			if f.st.visible && f.st.underline {
				p.Line(cx, bl+f.st.size*0.12, cx+f.w, bl+f.st.size*0.12, thick, f.st.paint(f.st.color))
			}
			if f.st.visible && f.st.strike {
				p.Line(cx, bl-f.st.size*0.28, cx+f.w, bl-f.st.size*0.28, thick, f.st.paint(f.st.color))
			}
			e.addLink(p, f.st.link, cx, bl-f.st.size*0.8, f.w, f.st.size)
			cx += f.w
		}
	}
	e.y += lh
}

func (e *engine) drawMarker(m marker, baseline float64) {
	if !m.st.visible {
		return
	}
	p := e.page()
	sz := m.st.size
	if m.pic != nil {
		w, h := m.pic.w*0.75, m.pic.h*0.75
		if h > sz*1.5 { // keep huge images reasonable
			w, h = w*sz*1.5/h, sz*1.5
		}
		m.pic.draw(e, p, pdf.Layer{}, nil, m.x-w-sz*0.5, baseline-h, w, h, m.st.alpha)
		return
	}
	if m.text != "" {
		w := e.textWidth(m.st, m.text)
		gap := sz * 0.4
		if m.shape == "custom" {
			gap = 0
		}
		e.drawText(p, m.x-w-gap, baseline, m.st, m.text)
		return
	}
	cx, cy, r := m.x-sz*0.7, baseline-sz*0.3, sz*0.17
	switch m.shape {
	case "disc":
		p.Circle(cx, cy, r, m.st.paint(m.st.color), true)
	case "circle":
		p.Circle(cx, cy, r*0.85, m.st.paint(m.st.color), false)
	case "square":
		p.FillRect(cx-r*0.9, cy-r*0.9, r*1.8, r*1.8, m.st.paint(m.st.color))
	}
}

// ---------------------------------------------------------------------
// Block layout

func (e *engine) layoutChildren(n *htmlparse.Node, st *style, x, w float64) {
	var items []item
	e.collect(n, st, &items)
	e.flush(items, st, x, w)
}

func (e *engine) flush(items []item, st *style, x, w float64) {
	forceAt := -1
	if !e.noBreak && !e.dry && pureInline(items) {
		forceAt = e.orphansWidowsBreak(items, st, x, w)
	}
	e.flushLines(items, st, x, w, false, forceAt)
}

// pureInline reports whether items form a single run of lines (a
// paragraph) without block-level boxes.
func pureInline(items []item) bool {
	words := 0
	for _, it := range items {
		switch it.kind {
		case kBlock, kFloat, kAbs, kClear:
			return false
		case kWord, kAtomic:
			words++
		}
	}
	return words > 0
}

// flushLines lays out the items. log records line boxes in e.lineLog;
// forceAt >= 0 starts a new page before that line (orphans/widows).
func (e *engine) flushLines(items []item, st *style, x, w float64, log bool, forceAt int) {
	lb := &lineBuilder{e: e, x: x, w: w, st: st, log: log, forceAt: forceAt}
	for i, it := range items {
		switch it.kind {
		case kWord:
			lb.word(it.text, it.st)
		case kSpace:
			lb.space()
		case kGap:
			lb.gap(it.w, it.st)
		case kAtomic:
			lb.atomic(it.node, it.st)
		case kBreak:
			lb.finish(it.text != "soft", it.st)
		case kFloat:
			lb.float(it)
		case kClear:
			lb.finish(false, nil)
			e.clearFloats(it.st.clear)
		case kAbs:
			y := e.y + e.pendingMargin
			if len(lb.frags) > 0 {
				y = e.y
			}
			e.addAbsolute(it.node, it.st, x, y)
		case kBlock:
			lb.finish(false, nil)
			if !e.noBreak && !e.dry {
				e.keepTogether(items, i, x, w)
			}
			e.layoutBlock(it.node, it.st, x, w)
			lb.started = false
		}
	}
	lb.finish(false, nil)
}

func (e *engine) layoutBlock(n *htmlparse.Node, st *style, x, w float64) {
	e.anchor(n, e.y+e.pendingMargin)
	if st.breakBefore && !e.noBreak {
		e.breakPending = true
	}
	switch {
	case isReplaced(n):
		e.layoutImage(n, st, x, w)
	case n.Tag == "hr":
		e.layoutHR(st, x, w)
	case n.Tag == "table" || st.display == "table":
		e.layoutTable(n, st, x, w)
	default:
		e.layoutBox(n, st, x, w)
	}
	if st.breakAfter && !e.noBreak {
		e.breakPending = true
	}
}

func (e *engine) layoutBox(n *htmlparse.Node, st *style, x, w float64) {
	e.addMargin(st.margin[0])
	if st.clear != "" {
		e.clearFloats(st.clear)
	}
	bx := x + st.margin[3]
	bw := w - st.margin[3] - st.margin[1]
	bt, br, bb, bl := st.bw(0), st.bw(1), st.bw(2), st.bw(3)
	fixedW := false
	if ww, ok := e.borderBoxWidth(st, w); ok {
		// margin: auto centres (or right-aligns) a box with a definite width
		free := w - st.margin[3] - st.margin[1] - ww
		switch {
		case st.autoMargin[3] && st.autoMargin[1]:
			bx += max(free, 0) / 2
		case st.autoMargin[3]:
			bx += max(free, 0)
		}
		bw, fixedW = ww, true
	}
	// A box that establishes a formatting context sits beside floats.
	bfc := st.isFlex() || st.isGrid() || st.overflowHidden || st.display == "flow-root"
	if bfc && len(e.floats) > 0 {
		l, sw := e.span(e.y+e.pendingMargin, 1, x+st.margin[3], w-st.margin[3]-st.margin[1])
		if l > bx {
			if fixedW {
				bx = max(bx, l)
			} else {
				bw -= l - bx
				bx = l
			}
		}
		if !fixedW && bx+bw > l+sw {
			bw = l + sw - bx
		}
	}
	if bw < 1 {
		bw = 1
	}
	if st.isFlex() || st.isGrid() {
		// A flex or grid container is its own formatting context.
		e.applyMargin()
		e.renderBox(n, st, bx, bw, 0)
		e.addMargin(st.margin[2])
		return
	}

	height := e.borderBoxHeight(st)
	hasBox := st.paints() || st.padding[0] > 0 || st.padding[2] > 0 || height > 0 || bfc || st.positioned() || (st.xform != "" && st.xform != "none")
	startPage, startY := e.pageIdx, e.y
	mark := overflowMark{-1, -1}
	tmark := transformMark{-1, -1, -1}
	dx, dy := 0.0, 0.0
	var savedFloats []floatBox
	pushed := false
	if hasBox {
		e.applyMargin()
		if st.position == "relative" || st.position == "sticky" {
			dx, dy = st.relOffset(e)
			bx += dx
			e.y += dy
		}
		startPage, startY = e.pageIdx, e.y
		mark = e.overflowMark(st)
		tmark = e.transformStart(st)
		pushed = e.pushFrame(st)
		if bfc {
			savedFloats, e.floats = e.floats, nil
		}
		if bt+st.padding[0] > 0 || bfc {
			e.y += bt + st.padding[0]
			e.atTop = !bfc // child margins stay inside the box
		}
		e.depth++
	}

	cx := bx + bl + st.padding[3]
	cw := max(bw-bl-br-st.padding[3]-st.padding[1], 1)
	e.layoutContent(n, st, cx, cw)

	if hasBox {
		e.depth--
		if bb+st.padding[2] > 0 || bfc {
			e.y += e.pendingMargin
			e.resetMargin()
		}
		if bfc {
			e.y = e.floatsBottomSince(0, e.y) // contain floats
			e.floats = savedFloats
		}
		e.y += st.padding[2] + bb
		if height > 0 && e.pageIdx == startPage && e.y-startY < height {
			e.y = startY + height
		}
		e.clipOverflow(st, startPage, mark, bx, startY, bw, e.y)
		e.decorate(startPage, startY, e.y, bx, bw, st)
		e.transformEnd(st, tmark, bx, startY, bw, e.y-startY)
		if pushed {
			e.popFrame(cbRect{page: startPage, x: bx + bl, y: startY + bt, w: bw - bl - br, h: e.y - startY - bt - bb})
		}
		e.y -= dy
		e.atTop = false
	}
	e.addMargin(st.margin[2])
}

func (e *engine) makeMarker(n *htmlparse.Node, st *style, x float64) (marker, bool) {
	m := marker{st: st, x: x}
	var ctx *listCtx
	if len(e.lists) > 0 {
		ctx = e.lists[len(e.lists)-1]
		ctx.index++
		if v, err := strconv.Atoi(n.Attr("value")); err == nil {
			ctx.index = v
		}
	}
	idx := 1
	if ctx != nil {
		idx = ctx.index
	}
	t := st.listType
	if t == "" {
		t = "disc"
	}
	if marker := e.pseudoMarker(n, st); marker != nil {
		m.text, m.shape, m.st = marker.content, "custom", marker
		return m, m.text != ""
	}
	if st.listImage != "" {
		if pic := e.loadImage(urlOf(st.listImage)); pic != nil {
			m.pic = pic
			return m, true
		}
	}
	switch {
	case t == "none":
		return m, false
	case t == "disc" || t == "circle" || t == "square":
		m.shape = t
	case t[0] == '"' || t[0] == '\'':
		m.text, m.shape = unescapeCSSString(t[1:len(t)-1]), "custom"
	default:
		m.text = formatCounter(idx, t) + "."
	}
	return m, true
}

// pseudoMarker supports li::marker { content: ... }.
func (e *engine) pseudoMarker(n *htmlparse.Node, st *style) *style {
	if !e.hasPseudo["marker"] {
		return nil
	}
	return e.pseudoStyle(n, st, "marker")
}

func leftPad(s string) string {
	if len(s) < 2 {
		return "0" + s
	}
	return s
}

func alpha(n int, base rune) string {
	if n <= 0 {
		return strconv.Itoa(n)
	}
	s := ""
	for n > 0 {
		n--
		s = string(base+rune(n%26)) + s
		n /= 26
	}
	return s
}

func roman(n int) string {
	if n <= 0 || n >= 4000 {
		return strconv.Itoa(n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// decorate paints the background and borders of a box that ran from
// (startPage, startY) to the current page at endY, one segment per page.
func (e *engine) decorate(startPage int, startY, endY, x, w float64, st *style) {
	if e.dry || !st.visible || !st.paints() {
		return
	}
	for pi := startPage; pi <= e.pageIdx; pi++ {
		top, bot := e.top, e.bottom
		if pi == startPage {
			top = startY
		}
		if pi == e.pageIdx {
			bot = endY
		}
		if bot <= top {
			continue
		}
		e.paintBox(e.pages[pi], e.depth, x, top, w, bot-top, st, pi == startPage, pi == e.pageIdx)
	}
}

func (e *engine) layoutHR(st *style, x, w float64) {
	e.addMargin(st.margin[0])
	e.applyMargin()
	lw := st.bw(0)
	h := lw + st.bw(2)
	if hh := e.boxHeight(st); hh > h {
		h = hh
	}
	e.ensureSpace(h)
	x1 := x + st.margin[3]
	x2 := x + w - st.margin[1]
	if cw, ok := e.boxWidth(st, w); ok && x1+cw < x2 {
		if st.margin[3] == 0 && st.margin[1] == 0 { // hr is centred by default in HTML
			x1 += (x2 - x1 - cw) / 2
		}
		x2 = x1 + cw
	}
	if !e.dry && h > 0 {
		e.paintBox(e.page(), e.depth, x1, e.y, x2-x1, h, st, true, true)
	}
	e.y += h
	e.addMargin(st.margin[2])
}

// fitEllipsis shortens text so that it plus "…" fits in w.
func (e *engine) fitEllipsis(st *style, text string, w float64) string {
	const dots = "…"
	if w <= e.textWidth(st, dots) {
		return dots
	}
	cut := 0
	for i := range text {
		if i > 0 && e.textWidth(st, text[:i]+dots) > w {
			break
		}
		cut = i
	}
	if next := len(text); e.textWidth(st, text+dots) <= w {
		cut = next
	}
	return strings.TrimRight(text[:cut], " \u00a0") + dots
}
