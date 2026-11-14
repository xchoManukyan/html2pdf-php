package render

import (
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/css"
	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
)

// absItem is an absolutely or fixed positioned box waiting for its
// containing block to be complete.
type absItem struct {
	node             *htmlparse.Node
	st               *style
	staticX, staticY float64
	page             int
}

// cbFrame collects the absolutely positioned descendants of a positioned
// box (their containing block).
type cbFrame struct {
	pending []absItem
}

// cbRect is a containing block: a padding box on one page.
type cbRect struct {
	page       int
	x, y, w, h float64
}

func (st *style) positioned() bool {
	return st.position == "relative" || st.position == "absolute" || st.position == "fixed" || st.position == "sticky"
}

// offset resolves one of top/right/bottom/left; ok is false for auto.
func (e *engine) offset(st *style, side int, ref float64) (float64, bool) {
	expr := st.offsetExpr[side]
	if expr == "" || expr == "auto" {
		return 0, false
	}
	return e.resolveLength(expr, st, ref)
}

// relOffset returns the shift of a relatively positioned box. Percentages
// are not resolved here (no containing block size); lengths are.
func (st *style) relOffset(e *engine) (dx, dy float64) {
	res := func(side int) (float64, bool) {
		expr := st.offsetExpr[side]
		if expr == "" || expr == "auto" || strings.Contains(expr, "%") {
			return 0, false
		}
		return css.ParseLength(expr, css.LengthCtx{Em: st.size, Rem: 12})
	}
	if v, ok := res(3); ok {
		dx = v
	} else if v, ok := res(1); ok {
		dx = -v
	}
	if v, ok := res(0); ok {
		dy = v
	} else if v, ok := res(2); ok {
		dy = -v
	}
	return
}

// addAbsolute records an out-of-flow box met during layout.
func (e *engine) addAbsolute(n *htmlparse.Node, st *style, x, y float64) {
	if e.dry {
		return
	}
	it := absItem{node: n, st: blockify(st), staticX: x, staticY: y, page: e.pageIdx}
	if st.position == "fixed" {
		e.fixedItems = append(e.fixedItems, it)
		return
	}
	frame := e.rootFrame
	if len(e.cbStack) > 0 {
		frame = e.cbStack[len(e.cbStack)-1]
	}
	frame.pending = append(frame.pending, it)
}

// pushFrame starts collecting absolute descendants for a positioned box.
func (e *engine) pushFrame(st *style) bool {
	if !st.positioned() || e.dry {
		return false
	}
	e.cbStack = append(e.cbStack, &cbFrame{})
	return true
}

// popFrame lays out the absolute descendants of the box whose padding box
// is cb, then forgets the frame.
func (e *engine) popFrame(cb cbRect) {
	frame := e.cbStack[len(e.cbStack)-1]
	e.cbStack = e.cbStack[:len(e.cbStack)-1]
	for _, it := range frame.pending {
		e.layoutAbsolute(it, cb)
	}
}

// layoutAbsolute positions and paints one absolutely positioned box.
func (e *engine) layoutAbsolute(it absItem, cb cbRect) {
	st := it.st
	ml, mr, mt, mb := st.margin[3], st.margin[1], st.margin[0], st.margin[2]
	left, hasL := e.offset(st, 3, cb.w)
	right, hasR := e.offset(st, 1, cb.w)
	top, hasT := e.offset(st, 0, cb.h)
	bottom, hasB := e.offset(st, 2, cb.h)

	// width
	bw, ok := e.borderBoxWidth(st, cb.w)
	if !ok {
		if hasL && hasR {
			bw = cb.w - left - right - ml - mr
		} else {
			avail := cb.w
			if hasL {
				avail -= left
			} else if hasR {
				avail -= right
			}
			bw = e.shrinkToFit(it.node, st, avail)
		}
	}
	bw = max(bw, st.extrasH())

	// height: explicit (percentages of the containing block), stretched
	// between top and bottom, or from the content
	h := 0.0
	if strings.Contains(st.heightExpr, "%") {
		if v, ok := e.resolveLength(st.heightExpr, st, cb.h); ok {
			h = v
			if !st.borderBox {
				h += st.extrasV()
			}
		}
	} else {
		h = e.borderBoxHeight(st)
	}
	if h <= 0 && hasT && hasB {
		h = cb.h - top - bottom - mt - mb
	}
	if h <= 0 {
		h, _ = e.measureBox(it.node, st, bw)
	}

	// With both insets given, auto margins take the free space: "inset: 0;
	// margin: auto; max-width: ..." centers the box (CSS 2.1 10.3.7/10.6.4).
	if hasL && hasR {
		free := cb.w - left - right - ml - mr - bw
		switch {
		case st.autoMargin[3] && st.autoMargin[1]:
			if free > 0 {
				ml += free / 2
			}
		case st.autoMargin[3]:
			ml += max(free, 0)
		}
	}
	if hasT && hasB {
		free := cb.h - top - bottom - mt - mb - h
		switch {
		case st.autoMargin[0] && st.autoMargin[2]:
			if free > 0 {
				mt += free / 2
			}
		case st.autoMargin[0]:
			mt += max(free, 0)
		}
	}

	var x, y float64
	switch {
	case hasL:
		x = cb.x + left + ml
	case hasR:
		x = cb.x + cb.w - right - mr - bw
	default:
		x = it.staticX + ml
		if st.autoMargin[3] && st.autoMargin[1] {
			x = cb.x + (cb.w-bw)/2
		}
	}
	page := cb.page
	switch {
	case hasT:
		y = cb.y + top + mt
	case hasB:
		y = cb.y + cb.h - bottom - mb - h
	default:
		y, page = it.staticY+mt, it.page
	}
	e.renderPositioned(it.node, st, page, x, y, bw, h)
}

// renderPositioned paints a positioned box above the normal flow.
func (e *engine) renderPositioned(n *htmlparse.Node, st *style, page int, x, y, w, h float64) {
	if page >= len(e.pages) {
		return
	}
	s := e.saveState()
	savedDepth, savedFloats := e.depth, e.floats
	e.pageIdx, e.y = page, y
	e.noBreak = true
	e.markers = nil
	e.floats = nil
	e.depth = 1000 + st.zIndex*10
	if st.zIndex < 0 {
		e.depth = 1 + st.zIndex
	}
	e.renderBox(n, st, x, w, h)
	e.depth, e.floats = savedDepth, savedFloats
	e.restoreState(s)
}

// layoutFixed paints position: fixed boxes. Boxes anchored with top or
// bottom repeat on every page (running headers and footers), like
// browsers do when printing; others stay where they appeared.
func (e *engine) layoutFixed() {
	for _, it := range e.fixedItems {
		_, hasT := e.offset(it.st, 0, 0)
		_, hasB := e.offset(it.st, 2, 0)
		pages := []int{it.page}
		if hasT || hasB {
			pages = pages[:0]
			for i := range e.pages {
				pages = append(pages, i)
			}
		}
		for _, p := range pages {
			cb := cbRect{page: p, x: e.left, y: e.top, w: e.contentWidth, h: e.bottom - e.top}
			item := it
			if p != it.page {
				item.staticY = e.top
			}
			e.layoutAbsolute(item, cb)
		}
	}
}
