package render

import (
	"math"
	"sort"
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
)

// Flexbox layout (CSS Flexible Box Layout Level 1), adapted to paged
// output: a flex line that does not fit the rest of the page moves to the
// next page; a line taller than a whole page is laid out with each item
// flowing across pages on its own.

type flexItem struct {
	node *htmlparse.Node
	st   *style

	// main axis, border-box sizes
	basis, hypo, minMain, maxMain float64
	target                        float64
	frozen                        bool
	mStart, mEnd                  float64 // main-axis margins
	autoStart, autoEnd            bool

	// cross axis
	cross                 float64 // border-box cross size
	mBefore, mAfter       float64
	autoBefore, autoAfter bool
	align                 string
}

func (it *flexItem) outerHypo() float64   { return it.hypo + it.mStart + it.mEnd }
func (it *flexItem) outerTarget() float64 { return it.target + it.mStart + it.mEnd }

// flexChildren returns the flex items of a container: element children
// (blockified), runs of text (anonymous items) and ::before/::after.
func (e *engine) flexChildren(n *htmlparse.Node, st *style) []*flexItem {
	var items []*flexItem
	anon := func(children []*htmlparse.Node, s *style) {
		node := &htmlparse.Node{Type: htmlparse.ElementNode, Tag: "#anon", Children: children, Parent: n}
		cp := *s
		cp.display = "block"
		cp.inlineBox = nil
		e.styles[node] = &cp
		items = append(items, &flexItem{node: node, st: &cp})
	}
	if ps := e.pseudoStyle(n, st, "before"); ps != nil && ps.content != "" {
		anon([]*htmlparse.Node{{Type: htmlparse.TextNode, Text: ps.content}}, ps)
	}
	var run []*htmlparse.Node
	flushRun := func() {
		for _, t := range run {
			if strings.TrimSpace(t.Text) != "" || st.pre() {
				anon(run, inheritFrom(st))
				break
			}
		}
		run = nil
	}
	for _, c := range n.Children {
		switch c.Type {
		case htmlparse.TextNode:
			run = append(run, c)
		case htmlparse.ElementNode:
			cst := e.computeStyle(c, st)
			if cst.display == "none" {
				continue
			}
			if cst.position == "absolute" || cst.position == "fixed" {
				continue // out of flow: laid out by layoutFlex
			}
			flushRun()
			if c.Tag == "br" {
				continue
			}
			// Flex items are blockified.
			item := cst
			if !cst.isBlock() || cst.isAtomicInline() {
				cp := *cst
				switch cst.display {
				case "inline-flex":
					cp.display = "flex"
				case "inline-grid":
					cp.display = "grid"
				case "inline-table":
					cp.display = "table"
				default:
					cp.display = "block"
				}
				cp.inlineBox = nil
				cp.shift = 0
				item = &cp
			}
			// form controls are shown by a stand-in carrying their value
			if fn, fst := e.formNode(c, item); fn != nil {
				items = append(items, &flexItem{node: fn, st: fst})
				continue
			}
			items = append(items, &flexItem{node: c, st: item})
		}
	}
	flushRun()
	if ps := e.pseudoStyle(n, st, "after"); ps != nil && ps.content != "" {
		anon([]*htmlparse.Node{{Type: htmlparse.TextNode, Text: ps.content}}, ps)
	}
	sort.SliceStable(items, func(a, b int) bool { return items[a].st.order < items[b].st.order })
	return items
}

func (e *engine) gap(expr string, st *style, ref float64) float64 {
	if expr == "" || expr == "normal" {
		return 0
	}
	g, _ := e.resolveLength(expr, st, ref)
	return max(g, 0)
}

// itemWidths returns the min-content and max-content border-box widths.
func (e *engine) itemWidths(it *flexItem) (mn, mx float64) {
	mn, mx = e.measureBlock(it.node, it.st)
	m := it.st.margin[1] + it.st.margin[3]
	return max(mn-m, 0), max(mx-m, 0)
}

func (e *engine) layoutFlex(n *htmlparse.Node, st *style, x, w float64) {
	for _, c := range n.Children {
		if c.Type == htmlparse.ElementNode {
			if cst := e.computeStyle(c, st); cst.display != "none" && (cst.position == "absolute" || cst.position == "fixed") {
				e.addAbsolute(c, cst, x, e.y+e.pendingMargin)
			}
		}
	}
	items := e.flexChildren(n, st)
	if len(items) == 0 {
		return
	}
	e.applyMargin()
	if strings.HasPrefix(st.flexDir, "column") {
		e.layoutFlexColumn(items, st, x, w)
		return
	}
	e.layoutFlexRow(items, st, x, w)
}

// ---------------------------------------------------------------------
// Row direction

func (e *engine) resolveRowSizes(items []*flexItem, st *style, w float64) {
	for _, it := range items {
		s := it.st
		it.mStart, it.mEnd = s.margin[3], s.margin[1]
		it.autoStart, it.autoEnd = s.autoMargin[3], s.autoMargin[1]
		it.mBefore, it.mAfter = s.margin[0], s.margin[2]
		it.autoBefore, it.autoAfter = s.autoMargin[0], s.autoMargin[2]
		it.align = s.alignSelf
		if it.align == "auto" || it.align == "" {
			it.align = st.alignItems
		}

		minC, maxC := -1.0, -1.0
		content := func() (float64, float64) {
			if minC < 0 {
				minC, maxC = e.itemWidths(it)
			}
			return minC, maxC
		}
		// flex-basis
		basisSet := false
		switch b := s.basisExpr; b {
		case "", "auto":
			if bw, ok := e.borderBoxWidth(s, w); ok {
				it.basis, basisSet = bw, true
			}
		case "content", "max-content", "fit-content":
		case "min-content":
			mn, _ := content()
			it.basis, basisSet = mn, true
		default:
			if l, ok := e.resolveLength(b, s, w); ok {
				if !s.borderBox {
					l += s.extrasH()
				}
				it.basis, basisSet = max(l, s.extrasH()), true
			}
		}
		if !basisSet {
			_, mx := content()
			it.basis = mx
		}
		// min/max main size
		it.minMain, it.maxMain = 0, math.Inf(1)
		if v, ok := e.resolveLength(s.minWidthExpr, s, w); ok {
			it.minMain = v
			if !s.borderBox {
				it.minMain += s.extrasH()
			}
		} else if s.minWidthExpr == "" || s.minWidthExpr == "auto" {
			// automatic minimum size: the min-content width
			mn, _ := content()
			it.minMain = mn
			if bw, ok := e.borderBoxWidth(s, w); ok && bw < mn {
				it.minMain = bw
			}
		}
		if v, ok := e.resolveLength(s.maxWidthExpr, s, w); ok {
			if !s.borderBox {
				v += s.extrasH()
			}
			it.maxMain = max(v, it.minMain)
		}
		it.hypo = clamp(it.basis, it.minMain, it.maxMain)
	}
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(v, hi)) }

// flexLine resolves flexible lengths for one line (CSS Flexbox 9.7).
func flexLine(line []*flexItem, avail, gap float64) {
	sumHypo := gap * float64(len(line)-1)
	for _, it := range line {
		sumHypo += it.outerHypo()
		it.frozen = false
		it.target = it.hypo
	}
	growing := sumHypo < avail
	for _, it := range line {
		if growing && (it.st.grow == 0 || it.basis > it.hypo) || !growing && (it.st.shrink == 0 || it.basis < it.hypo) {
			it.frozen = true
		}
	}
	for iter := 0; iter <= len(line); iter++ {
		free := avail - gap*float64(len(line)-1)
		var unfrozen []*flexItem
		sumGrow, sumShrink := 0.0, 0.0
		for _, it := range line {
			if it.frozen {
				free -= it.target + it.mStart + it.mEnd
			} else {
				free -= it.basis + it.mStart + it.mEnd
				unfrozen = append(unfrozen, it)
				sumGrow += it.st.grow
				sumShrink += it.st.shrink * it.basis
			}
		}
		if len(unfrozen) == 0 {
			return
		}
		if growing && sumGrow < 1 {
			free *= sumGrow
		}
		violation := 0.0
		for _, it := range unfrozen {
			t := it.basis
			switch {
			case growing && sumGrow > 0:
				t += free * it.st.grow / sumGrow
			case !growing && sumShrink > 0:
				t += free * it.st.shrink * it.basis / sumShrink
			}
			c := clamp(t, it.minMain, it.maxMain)
			violation += c - t
			it.target = c
		}
		for _, it := range unfrozen {
			t := it.target
			switch {
			case violation == 0:
				it.frozen = true
			case violation > 0 && t <= it.minMain+1e-9:
				it.frozen = true
			case violation < 0 && t >= it.maxMain-1e-9:
				it.frozen = true
			}
		}
		if violation == 0 {
			return
		}
	}
}

// distributeMain returns the start offset and the space between items for
// the remaining free space on a line, after auto margins took their part.
func distributeMain(line []*flexItem, free float64, justify string, reverse bool) (offset, between float64) {
	autos := 0
	for _, it := range line {
		if it.autoStart {
			autos++
		}
		if it.autoEnd {
			autos++
		}
	}
	if free > 0 && autos > 0 {
		share := free / float64(autos)
		for _, it := range line {
			if it.autoStart {
				it.mStart += share
			}
			if it.autoEnd {
				it.mEnd += share
			}
		}
		return 0, 0
	}
	n := float64(len(line))
	switch justify {
	case "end":
		offset = free
	case "center":
		offset = free / 2
	case "space-between":
		if n > 1 && free > 0 {
			between = free / (n - 1)
		} else if free < 0 {
			offset = 0
		}
	case "space-around":
		if free > 0 {
			between = free / n
			offset = between / 2
		} else {
			offset = free / 2
		}
	case "space-evenly":
		if free > 0 {
			between = free / (n + 1)
			offset = between
		} else {
			offset = free / 2
		}
	}
	if reverse {
		// for row-reverse, "start" is the right edge
		switch justify {
		case "start", "stretch", "normal", "":
			offset = free
		case "end":
			offset = 0
		}
	}
	return
}

func (e *engine) layoutFlexRow(items []*flexItem, st *style, x, w float64) {
	colGap := e.gap(st.colGapExpr, st, w)
	rowGap := e.gap(st.rowGapExpr, st, w)
	e.resolveRowSizes(items, st, w)
	reverse := st.flexDir == "row-reverse"

	// Break into lines.
	var lines [][]*flexItem
	if st.flexWrap == "nowrap" || st.flexWrap == "" {
		lines = [][]*flexItem{items}
	} else {
		var cur []*flexItem
		used := 0.0
		for _, it := range items {
			add := it.outerHypo()
			if len(cur) > 0 {
				add += colGap
			}
			if len(cur) > 0 && used+add > w+0.01 {
				lines = append(lines, cur)
				cur, used = nil, 0
				add = it.outerHypo()
			}
			cur = append(cur, it)
			used += add
		}
		if len(cur) > 0 {
			lines = append(lines, cur)
		}
		if st.flexWrap == "wrap-reverse" {
			for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
				lines[i], lines[j] = lines[j], lines[i]
			}
		}
	}

	definite := e.contentHeight(st)
	type lineInfo struct {
		cross float64
	}
	infos := make([]lineInfo, len(lines))
	for li, line := range lines {
		flexLine(line, w, colGap)
		for _, it := range line {
			it.cross, _ = e.measureBox(it.node, it.st, it.target)
			infos[li].cross = max(infos[li].cross, it.cross+it.mBefore+it.mAfter)
		}
	}
	// A single line fills a definite container height; extra cross space
	// is shared between lines (align-content: stretch).
	if definite > 0 {
		total := rowGap * float64(len(lines)-1)
		for _, in := range infos {
			total += in.cross
		}
		if free := definite - total; free > 0 {
			if len(lines) == 1 {
				infos[0].cross = definite
			} else if st.alignContent == "stretch" {
				for i := range infos {
					infos[i].cross += free / float64(len(infos))
				}
			}
		}
	}

	pageH := e.bottom - e.top
	for li, line := range lines {
		lineCross := infos[li].cross
		if li > 0 {
			e.y += rowGap
		}
		if !e.noBreak && !e.dry && e.y+lineCross > e.bottom+0.01 && e.y > e.top+0.5 && lineCross <= pageH {
			e.newPage()
			e.atTop = false
		}
		used := colGap * float64(len(line)-1)
		for _, it := range line {
			used += it.outerTarget()
		}
		offset, between := distributeMain(line, w-used, st.justify, reverse)

		order := line
		if reverse {
			order = make([]*flexItem, len(line))
			for i, it := range line {
				order[len(line)-1-i] = it
			}
		}
		lineTop := e.y
		flow := !e.noBreak && !e.dry && lineTop+lineCross > e.bottom+0.01
		cx := x + offset
		startPage := e.pageIdx
		endPage, endY := e.pageIdx, lineTop+lineCross
		for _, it := range order {
			ix := cx + it.mStart
			cx += it.outerTarget() + colGap + between
			minH := 0.0
			top := lineTop + it.mBefore
			free := lineCross - it.cross - it.mBefore - it.mAfter
			switch {
			case it.autoBefore && it.autoAfter:
				top += max(free, 0) / 2
			case it.autoBefore:
				top += max(free, 0)
			case it.autoAfter:
			case it.align == "stretch" && e.boxHeight(it.st) == 0:
				minH = lineCross - it.mBefore - it.mAfter
			case it.align == "center":
				top += free / 2
			case it.align == "end":
				top += free
			}
			if flow {
				// The line is taller than a page: let each item break pages.
				e.pageIdx, e.y = startPage, top
				e.atTop = false
				e.resetMargin()
				e.renderBox(it.node, it.st, ix, it.target, 0)
				y := e.y + it.mAfter
				if e.pageIdx > endPage || e.pageIdx == endPage && y > endY {
					endPage, endY = e.pageIdx, y
				}
				continue
			}
			s := e.saveState()
			e.noBreak = true
			e.y = top
			e.renderBox(it.node, it.st, ix, it.target, minH)
			e.restoreState(s)
		}
		if flow {
			e.pageIdx, e.y = endPage, endY
		} else {
			e.y = lineTop + lineCross
		}
	}
	e.atTop = false
}

// ---------------------------------------------------------------------
// Column direction

func (e *engine) layoutFlexColumn(items []*flexItem, st *style, x, w float64) {
	rowGap := e.gap(st.rowGapExpr, st, w)
	if st.flexDir == "column-reverse" {
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}
	type placed struct {
		it      *flexItem
		x, w, h float64
		minH    float64
	}
	var ps []placed
	total := rowGap * float64(len(items)-1)
	for _, it := range items {
		s := it.st
		align := s.alignSelf
		if align == "auto" || align == "" {
			align = st.alignItems
		}
		ml, mr := s.margin[3], s.margin[1]
		avail := w - ml - mr
		bw, ok := e.borderBoxWidth(s, w)
		if !ok {
			if align == "stretch" && !(s.autoMargin[1] || s.autoMargin[3]) {
				bw = avail
			} else {
				mn, mx := e.itemWidths(it)
				bw = min(mx, max(mn, avail))
			}
		}
		ix := x + ml
		switch {
		case s.autoMargin[3] && s.autoMargin[1]:
			ix += (avail - bw) / 2
		case s.autoMargin[3]:
			ix += avail - bw
		case align == "center":
			ix += (avail - bw) / 2
		case align == "end":
			ix += avail - bw
		}
		h, _ := e.measureBox(it.node, s, bw)
		ps = append(ps, placed{it: it, x: ix, w: bw, h: h})
		total += h + s.margin[0] + s.margin[2]
	}

	definite := e.contentHeight(st)
	offset, between := 0.0, 0.0
	if definite > 0 && total < definite {
		free := definite - total
		autos, sumGrow := 0, 0.0
		for _, p := range ps {
			sumGrow += p.it.st.grow
			if p.it.st.autoMargin[0] {
				autos++
			}
			if p.it.st.autoMargin[2] {
				autos++
			}
		}
		switch {
		case sumGrow > 0:
			for i := range ps {
				if g := ps[i].it.st.grow; g > 0 {
					ps[i].minH = ps[i].h + free*g/sumGrow
				}
			}
		case autos > 0:
			share := free / float64(autos)
			for i := range ps {
				if ps[i].it.st.autoMargin[0] {
					ps[i].it.st = withMargin(ps[i].it.st, 0, share)
				}
				if ps[i].it.st.autoMargin[2] {
					ps[i].it.st = withMargin(ps[i].it.st, 2, share)
				}
			}
		default:
			fi := make([]*flexItem, len(ps))
			for i := range ps {
				fi[i] = ps[i].it
			}
			offset, between = distributeMain(fi, free, st.justify, false)
		}
	}
	// Fits on the rest of this page: place exactly. Otherwise flow the
	// items one after another, breaking pages between and inside them.
	exact := definite > 0 && (e.noBreak || e.dry || e.y+max(total, definite) <= e.bottom+0.01)
	start := e.y
	e.y += offset
	for i, p := range ps {
		s := p.it.st
		if i > 0 {
			e.y += rowGap + between
		}
		e.y += s.margin[0]
		if !exact && !e.noBreak && !e.dry && e.y+p.h > e.bottom+0.01 && e.y > e.top+0.5 && p.h <= e.bottom-e.top {
			e.newPage()
		}
		e.renderBox(p.it.node, s, p.x, p.w, p.minH)
		e.y += s.margin[2]
	}
	if exact && e.y-start < definite {
		e.y = start + definite
	}
	e.atTop = false
}

func withMargin(s *style, side int, v float64) *style {
	cp := *s
	cp.margin[side] = v
	cp.autoMargin[side] = false
	return &cp
}

// measureFlex returns the min-content and max-content widths of a flex
// container's content box.
func (e *engine) measureFlex(n *htmlparse.Node, st *style) (mn, mx float64) {
	items := e.flexChildren(n, st)
	column := strings.HasPrefix(st.flexDir, "column")
	gap := e.gap(st.colGapExpr, st, e.contentWidth)
	for i, it := range items {
		imn, imx := e.measureBlock(it.node, it.st)
		if bw, ok := e.borderBoxWidth(it.st, e.contentWidth); ok && !strings.Contains(it.st.widthExpr, "%") {
			imx = bw + it.st.margin[1] + it.st.margin[3]
			imn = min(imn, imx)
		}
		switch {
		case column:
			mn, mx = max(mn, imn), max(mx, imx)
		case st.flexWrap == "nowrap" || st.flexWrap == "":
			if i > 0 {
				mn += gap
				mx += gap
			}
			mn += imn
			mx += imx
		default:
			mn = max(mn, imn)
			if i > 0 {
				mx += gap
			}
			mx += imx
		}
	}
	return
}
