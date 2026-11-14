package render

import (
	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
)

// floatBox is the margin box of a placed float, in page coordinates.
type floatBox struct {
	page   int
	left   bool
	x0, x1 float64
	y0, y1 float64
}

// span returns the horizontal space left between floats for a line box at
// y with height h, within the containing block [x, x+w].
func (e *engine) span(y, h, x, w float64) (float64, float64) {
	left, right := x, x+w
	for _, f := range e.floats {
		if f.page != e.pageIdx || f.y1 <= y+0.01 || f.y0 >= y+h-0.01 {
			continue
		}
		if f.left {
			left = max(left, f.x1)
		} else {
			right = min(right, f.x0)
		}
	}
	return left, max(right-left, 0)
}

// nextFloatBottom returns the smallest bottom edge of the floats that
// intersect [y, y+h] on this page, or y when none do.
func (e *engine) nextFloatBottom(y, h float64) float64 {
	best := -1.0
	for _, f := range e.floats {
		if f.page != e.pageIdx || f.y1 <= y+0.01 || f.y0 >= y+h-0.01 {
			continue
		}
		if best < 0 || f.y1 < best {
			best = f.y1
		}
	}
	if best < 0 {
		return y
	}
	return best
}

// floatBottom returns the lowest bottom of the floats on this page on the
// given side ("left", "right" or "both").
func (e *engine) floatBottom(side string) float64 {
	bottom := 0.0
	for _, f := range e.floats {
		if f.page != e.pageIdx {
			continue
		}
		if side == "both" || (side == "left") == f.left {
			bottom = max(bottom, f.y1)
		}
	}
	return bottom
}

// clearFloats moves the flow below floats (CSS clear).
func (e *engine) clearFloats(side string) {
	if side == "" || len(e.floats) == 0 {
		return
	}
	b := e.floatBottom(side)
	if b > e.y+e.pendingMargin {
		e.applyMargin()
		e.y = b
	}
}

// blockify returns the style a float/absolutely positioned box is laid out
// with: inline-level display values become block-level.
func blockify(st *style) *style {
	if st.isBlock() && !st.isAtomicInline() {
		return st
	}
	cp := *st
	switch st.display {
	case "inline-flex":
		cp.display = "flex"
	case "inline-grid":
		cp.display = "grid"
	case "inline-table":
		cp.display = "table"
	case "list-item":
	default:
		cp.display = "block"
	}
	cp.inlineBox = nil
	cp.shift = 0
	cp.relX = 0
	return &cp
}

// shrinkToFit returns the border-box width of a float or absolutely
// positioned box in an available width.
func (e *engine) shrinkToFit(n *htmlparse.Node, st *style, avail float64) float64 {
	if bw, ok := e.borderBoxWidth(st, avail); ok {
		return bw
	}
	if isReplaced(n) {
		iw, _ := e.imageSize(n, st, max(avail-st.margin[1]-st.margin[3]-st.extrasH(), 1))
		return iw + st.extrasH()
	}
	mn, mx := e.measureBlock(n, st)
	return max(min(mx, max(mn, avail))-st.margin[1]-st.margin[3], st.extrasH())
}

// placeFloat lays out a float at the current position inside the
// containing block [x, x+w] and registers its exclusion area.
func (e *engine) placeFloat(n *htmlparse.Node, st *style, x, w float64) {
	fs := blockify(st)
	ml, mr, mt, mb := fs.margin[3], fs.margin[1], fs.margin[0], fs.margin[2]
	bw := e.shrinkToFit(n, fs, w)
	h, _ := e.measureBox(n, fs, bw)
	outerW, outerH := bw+ml+mr, h+mt+mb
	left := fs.float != "right"

	if fs.clear != "" {
		e.clearFloats(fs.clear)
	}
	y := e.y + e.pendingMargin
	if !e.noBreak && !e.dry && y+outerH > e.bottom+0.01 && y > e.top+0.5 && outerH <= e.bottom-e.top {
		e.applyMargin()
		e.newPage()
		y = e.y
	}
	// Move down until the float fits next to the other floats.
	var fx float64
	for i := 0; i < 100; i++ {
		l, sw := e.span(y, outerH, x, w)
		if sw >= outerW-0.01 || (l == x && sw == w) {
			if left {
				fx = l
			} else {
				fx = l + sw - outerW
			}
			break
		}
		nb := e.nextFloatBottom(y, outerH)
		if nb <= y {
			fx = x
			if !left {
				fx = x + w - outerW
			}
			break
		}
		y = nb
	}

	s := e.saveState()
	e.noBreak = true
	e.markers = nil
	e.y = y + mt
	e.depth++
	e.renderBox(n, fs, fx+ml, bw, 0)
	e.depth--
	e.restoreState(s)
	e.floats = append(e.floats, floatBox{page: e.pageIdx, left: left, x0: fx, x1: fx + outerW, y0: y, y1: y + outerH})
}

// floatsBottom returns how far floats placed after index from extend below y
// on the current page (used by boxes that contain their floats).
func (e *engine) floatsBottomSince(from int, y float64) float64 {
	for _, f := range e.floats[from:] {
		if f.page == e.pageIdx {
			y = max(y, f.y1)
		}
	}
	return y
}
