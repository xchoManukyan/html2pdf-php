package render

import (
	"math"

	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
)

// Atomic inline boxes (inline-block, inline-flex, inline-table, inline
// images) are laid out as a unit and placed on the line like a big glyph.

// atomicSize returns the margin-box width of an atomic inline and how far
// it extends above and below the baseline.
func (e *engine) atomicSize(n *htmlparse.Node, st *style, avail float64) (w, asc, desc float64) {
	mt, mb := st.margin[0], st.margin[2]
	ml, mr := st.margin[3], st.margin[1]
	if isReplaced(n) {
		iw, ih := e.imageSize(n, st, max(avail-ml-mr-st.extrasH(), 1))
		// An image sits on the baseline with its bottom margin edge.
		return iw + st.extrasH() + ml + mr, ih + st.extrasV() + mt + mb, 0
	}
	bw, ok := e.borderBoxWidth(st, avail)
	if !ok {
		// shrink-to-fit: min(max-content, max(min-content, available))
		mn, mx := e.measureBlock(n, st)
		bw = min(mx, max(mn, avail)) - ml - mr
	}
	bw = max(bw, st.extrasH())
	h, base := e.measureBox(n, e.atomicInner(st), bw)
	if math.IsNaN(base) || st.isFlex() || st.isGrid() || st.display == "inline-table" || isToggle(n) {
		// no line boxes inside (or flex/table): baseline is the bottom margin edge
		return bw + ml + mr, h + mt + mb, 0
	}
	return bw + ml + mr, mt + base, h - base + mb
}

// atomicInner returns the style used to lay out the inside of an atomic
// inline: it behaves like a block container (or flex/table container).
func (e *engine) atomicInner(st *style) *style {
	cp := *st
	switch st.display {
	case "inline-flex":
		cp.display = "flex"
	case "inline-grid":
		cp.display = "grid"
	case "inline-table":
		cp.display = "table"
	default:
		cp.display = "block"
	}
	cp.shift = 0
	cp.inlineBox = nil
	return &cp
}

func (lb *lineBuilder) atomic(n *htmlparse.Node, st *style) {
	e := lb.e
	w, asc, desc := e.atomicSize(n, st, lb.w)
	lb.ensureLine(w)
	sp := 0.0
	if lb.pending && len(lb.frags) > 0 {
		sp = e.spaceWidth(st)
	}
	lb.pending = false
	if len(lb.frags) > 0 && lb.width+sp+w > lb.avail()+0.01 {
		lb.emit(true)
		lb.ensureLine(w)
		sp = 0
	}
	lb.frags = append(lb.frags, frag{st: st, w: w, space: sp, atomic: n, asc: asc - st.shift, desc: desc + st.shift})
	lb.width += sp + w
}

// drawAtomic paints an atomic inline whose margin box starts at x.
func (e *engine) drawAtomic(f frag, x, baseline float64) {
	st := f.st
	top := baseline - f.asc + st.margin[0]
	bw := f.w - st.margin[3] - st.margin[1]
	s := e.saveState()
	e.noBreak = true
	e.markers = nil
	e.y = top
	if isReplaced(f.atomic) {
		e.renderBox(f.atomic, st, x+st.margin[3], bw, 0)
		if !e.dry {
			e.addLink(e.page(), st.link, x+st.margin[3], top, bw, f.asc+f.desc-st.margin[0]-st.margin[2])
		}
	} else {
		e.depth++
		e.renderBox(f.atomic, e.atomicInner(st), x+st.margin[3], bw, 0)
		e.depth--
	}
	e.restoreState(s)
}
