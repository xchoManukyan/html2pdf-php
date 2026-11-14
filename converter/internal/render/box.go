package render

import (
	"math"
	"strconv"

	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
	"github.com/xchoManukyan/html2pdf/converter/internal/pdf"
)

// flowState is the part of the engine state that a nested, positioned
// layout (flex item, inline-block, table cell) must not disturb.
type flowState struct {
	y, pendingMargin     float64
	marginPos, marginNeg float64
	atTop, noBreak       bool
	dry                  bool
	pageIdx              int
	markers              []marker
	listIdx              []int
	lastBaseline         float64
	breakPending         bool
	floats               []floatBox
}

func (e *engine) saveState() flowState {
	s := flowState{y: e.y, pendingMargin: e.pendingMargin, marginPos: e.marginPos, marginNeg: e.marginNeg, atTop: e.atTop, noBreak: e.noBreak,
		dry: e.dry, pageIdx: e.pageIdx, markers: e.markers, lastBaseline: e.lastBaseline,
		breakPending: e.breakPending, floats: e.floats}
	for _, l := range e.lists {
		s.listIdx = append(s.listIdx, l.index)
	}
	return s
}

func (e *engine) restoreState(s flowState) {
	e.y, e.pendingMargin, e.atTop, e.noBreak = s.y, s.pendingMargin, s.atTop, s.noBreak
	e.marginPos, e.marginNeg = s.marginPos, s.marginNeg
	e.dry, e.pageIdx, e.markers, e.lastBaseline = s.dry, s.pageIdx, s.markers, s.lastBaseline
	e.breakPending = s.breakPending
	e.floats = s.floats
	for i, idx := range s.listIdx {
		if i < len(e.lists) {
			e.lists[i].index = idx
		}
	}
}

// borderBoxWidth returns the specified width of a box as a border-box
// width (honouring box-sizing), or ok=false for auto.
func (e *engine) borderBoxWidth(st *style, containing float64) (float64, bool) {
	w, ok := e.boxWidth(st, containing)
	if !ok {
		return 0, false
	}
	if !st.borderBox {
		w += st.extrasH()
	}
	return max(w, st.extrasH()), true
}

// borderBoxHeight returns the specified (min-)height as a border-box
// height, or 0 for auto.
func (e *engine) borderBoxHeight(st *style) float64 {
	h := e.boxHeight(st)
	if h <= 0 {
		return 0
	}
	if !st.borderBox {
		h += st.extrasV()
	}
	return h
}

// contentHeight returns the definite content height of a box, or 0.
func (e *engine) contentHeight(st *style) float64 {
	h := e.borderBoxHeight(st)
	if h <= 0 {
		return 0
	}
	return max(h-st.extrasV(), 0)
}

// layoutContent lays out the inside of a box: list bookkeeping, then
// either a flex formatting context or normal flow.
func (e *engine) layoutContent(n *htmlparse.Node, st *style, cx, cw float64) {
	pushed := false
	if n.Tag == "ul" || n.Tag == "ol" || n.Tag == "menu" || n.Tag == "dir" {
		ctx := &listCtx{ordered: n.Tag == "ol"}
		if s, err := strconv.Atoi(n.Attr("start")); err == nil {
			ctx.index = s - 1
		}
		e.lists = append(e.lists, ctx)
		pushed = true
	}
	if st.display == "list-item" {
		if m, ok := e.makeMarker(n, st, cx); ok {
			e.markers = append(e.markers, m)
		}
	}
	if st.isFlex() {
		e.layoutFlex(n, st, cx, cw)
	} else if st.isGrid() {
		e.layoutGrid(n, st, cx, cw)
	} else {
		e.layoutChildren(n, st, cx, cw)
	}
	if len(e.markers) > 0 && st.display == "list-item" {
		e.emitLine(nil, cx, cw, st, false, nil, 0) // empty <li> still shows its bullet
	}
	if pushed {
		e.lists = e.lists[:len(e.lists)-1]
	}
}

// renderBox lays out an element whose border box starts at (x, e.y) with
// width w. Margins are the caller's business. The box is an independent
// formatting context: child margins stay inside. minH is a minimum
// border-box height (used by flex stretch). Page breaks follow e.noBreak.
func (e *engine) renderBox(n *htmlparse.Node, st *style, x, w, minH float64) {
	if st.xform == "" || st.xform == "none" {
		e.renderBoxInner(n, st, x, w, minH)
		return
	}
	tm := e.transformStart(st)
	y0, p0 := e.y, e.pageIdx
	e.renderBoxInner(n, st, x, w, minH)
	if e.pageIdx == p0 {
		e.transformEnd(st, tm, x, y0, w, e.y-y0)
	}
}

func (e *engine) renderBoxInner(n *htmlparse.Node, st *style, x, w, minH float64) {
	e.anchor(n, e.y)
	if isReplaced(n) {
		_, ih := e.imageSize(n, st, w-st.extrasH())
		if img := e.pictureFor(n); img != nil {
			e.drawImageBox(img, st, x, e.y, w-st.extrasH(), ih)
		}
		e.y += ih + st.extrasV()
		return
	}
	if st.display == "table" || st.display == "inline-table" || n.Tag == "table" {
		cp := *st
		cp.margin = [4]float64{}
		cp.widthExpr = strconv.FormatFloat(w/0.75, 'f', 3, 64) + "px"
		cp.borderBox = true
		e.atTop = false
		e.resetMargin()
		e.layoutTable(n, &cp, x, w)
		e.y += e.pendingMargin
		e.resetMargin()
		return
	}
	dx, dy := 0.0, 0.0
	if st.position == "relative" || st.position == "sticky" {
		dx, dy = st.relOffset(e)
		x += dx
		e.y += dy
	}
	startPage, startY := e.pageIdx, e.y
	mark := e.overflowMark(st)
	pushed := e.pushFrame(st)
	savedFloats := e.floats
	e.floats = nil
	e.depth++
	e.y += st.bw(0) + st.padding[0]
	e.atTop = false
	e.resetMargin()
	cx := x + st.bw(3) + st.padding[3]
	cw := max(w-st.extrasH(), 1)
	e.layoutContent(n, st, cx, cw)
	e.y += e.pendingMargin // margins do not escape a formatting context
	e.resetMargin()
	e.y = e.floatsBottomSince(0, e.y) // a formatting context contains its floats
	e.floats = savedFloats
	e.y += st.padding[2] + st.bw(2)
	e.depth--
	if h := max(minH, e.borderBoxHeight(st)); e.pageIdx == startPage && e.y-startY < h {
		e.y = startY + h
	}
	e.clipOverflow(st, startPage, mark, x, startY, w, e.y)
	e.decorate(startPage, startY, e.y, x, w, st)
	if pushed {
		e.popFrame(cbRect{page: startPage, x: x + st.bw(3), y: startY + st.bw(0), w: w - st.bw(1) - st.bw(3),
			h: e.y - startY - st.bw(0) - st.bw(2)})
	}
	e.y -= dy
	e.atTop = false
}

// drawImageBox paints an image element: its box (shadow, background,
// border) and the picture clipped to the rounded content box. (x, y) is
// the border-box origin; iw x ih the content size.
func (e *engine) drawImageBox(img *picture, st *style, x, y, iw, ih float64) {
	if e.dry || !st.visible {
		return
	}
	p := e.page()
	bw, bh := iw+st.extrasH(), ih+st.extrasV()
	e.paintBox(p, e.depth, x, y, bw, bh, st, true, true)
	cx, cy := x+st.bw(3)+st.padding[3], y+st.bw(0)+st.padding[0]
	r := e.radii(st, bw, bh)
	if r.IsZero() {
		img.draw(e, p, pdf.Layer{}, nil, cx, cy, iw, ih, st.alpha)
		return
	}
	inset := []float64{max(st.bw(0), st.bw(3)), max(st.bw(0), st.bw(1)), max(st.bw(2), st.bw(1)), max(st.bw(2), st.bw(3))}
	for i := range r {
		r[i] = max(r[i]-inset[i], 0)
	}
	clip := pdf.Rect{X: cx, Y: cy, W: iw, H: ih, R: r}
	img.draw(e, p, pdf.Layer{}, &clip, cx, cy, iw, ih, st.alpha)
}

// measureBox lays a box out without drawing and returns its border-box
// height and the baseline of its last line (relative to the border-box
// top), or NaN when it has no line boxes.
func (e *engine) measureBox(n *htmlparse.Node, st *style, w float64) (h, baseline float64) {
	s := e.saveState()
	e.dry, e.noBreak = true, true
	e.markers = nil
	e.y = 0
	e.lastBaseline = math.NaN()
	e.renderBox(n, st, 0, w, 0)
	h, baseline = e.y, e.lastBaseline
	e.restoreState(s)
	return
}

// overflowMark remembers where a clipped box's content starts.
// overflowMark packs the foreground and background marks of the page.
type overflowMark struct{ fg, bg int }

func (e *engine) overflowMark(st *style) overflowMark {
	if !st.overflowHidden || e.dry {
		return overflowMark{-1, -1}
	}
	return overflowMark{e.page().ForegroundMark(), e.page().BackgroundMark()}
}

// clipOverflow clips the content drawn since mark to the padding box
// (rounded like the border), when the box did not cross a page break.
func (e *engine) clipOverflow(st *style, startPage int, mark overflowMark, x, top, w, bottom float64) {
	if mark.fg < 0 || e.dry || e.pageIdx != startPage {
		return
	}
	h := bottom - top
	r := e.radii(st, w, h)
	inset := []float64{max(st.bw(0), st.bw(3)), max(st.bw(0), st.bw(1)), max(st.bw(2), st.bw(1)), max(st.bw(2), st.bw(3))}
	for i := range r {
		r[i] = max(r[i]-inset[i], 0)
	}
	clip := pdf.Rect{X: x + st.bw(3), Y: top + st.bw(0), W: w - st.bw(1) - st.bw(3), H: h - st.bw(0) - st.bw(2), R: r}
	pg := e.pages[startPage]
	pg.ClipForeground(mark.fg, clip)
	pg.ClipBackground(mark.bg, clip)
}
