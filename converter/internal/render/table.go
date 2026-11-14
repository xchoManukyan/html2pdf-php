package render

import (
	"strconv"
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
	"github.com/xchoManukyan/html2pdf/converter/internal/pdf"
)

// Tables (CSS 2.1 chapter 17): a cell grid with colspan/rowspan, the
// separated and collapsing border models, automatic and fixed column
// widths, <col>/<colgroup>, captions, repeated header/footer groups and
// rows that split across pages.

type tableCell struct {
	node             *htmlparse.Node
	st               *style
	row, col         int
	colspan, rowspan int
}

type tableRow struct {
	node    *htmlparse.Node
	st      *style
	groupSt *style // thead/tbody/tfoot style, nil for bare rows
	cells   []*tableCell
	group   string // "head", "body" or "foot"
}

type tableModel struct {
	st        *style
	captions  []*htmlparse.Node
	captionSt []*style
	rows      []*tableRow
	cols      int
	colSt     []*style // per column: style of its <col> (or <colgroup>), may be nil
	collapse  bool
	hsp, vsp  float64 // border-spacing (separated borders only)
}

func (e *engine) buildTable(n *htmlparse.Node, st *style) *tableModel {
	t := &tableModel{st: st, collapse: st.borderCollapse}
	if !t.collapse && st.spacingExpr != "" {
		f := strings.Fields(st.spacingExpr)
		t.hsp, _ = e.resolveLength(f[0], st, 0)
		t.vsp = t.hsp
		if len(f) > 1 {
			t.vsp, _ = e.resolveLength(f[1], st, 0)
		}
		t.hsp, t.vsp = max(t.hsp, 0), max(t.vsp, 0)
	}

	type section struct {
		node  *htmlparse.Node
		st    *style
		group string
		trs   []*htmlparse.Node
	}
	var heads, bodies, foots []section
	var colStyles []*style
	addCols := func(node *htmlparse.Node, s *style) {
		span, err := strconv.Atoi(node.Attr("span"))
		if err != nil || span < 1 {
			span = 1
		}
		for i := 0; i < min(span, 1000); i++ {
			colStyles = append(colStyles, s)
		}
	}
	var loose []*htmlparse.Node // rows directly under <table>
	flushLoose := func() {
		if len(loose) > 0 {
			bodies = append(bodies, section{st: st, group: "body", trs: loose})
			loose = nil
		}
	}
	for _, c := range n.Children {
		if c.Type != htmlparse.ElementNode {
			continue
		}
		switch c.Tag {
		case "caption":
			t.captions = append(t.captions, c)
			t.captionSt = append(t.captionSt, e.computeStyle(c, st))
		case "colgroup":
			gst := e.computeStyle(c, st)
			hasCol := false
			for _, col := range c.Children {
				if col.Type == htmlparse.ElementNode && col.Tag == "col" {
					hasCol = true
					addCols(col, e.computeStyle(col, gst))
				}
			}
			if !hasCol {
				addCols(c, gst)
			}
		case "col":
			addCols(c, e.computeStyle(c, st))
		case "tr":
			loose = append(loose, c)
		case "thead", "tbody", "tfoot":
			flushLoose()
			sst := e.computeStyle(c, st)
			if sst.display == "none" {
				continue
			}
			sec := section{node: c, st: sst, group: map[string]string{"thead": "head", "tbody": "body", "tfoot": "foot"}[c.Tag]}
			for _, tr := range c.Children {
				if tr.Type == htmlparse.ElementNode && tr.Tag == "tr" {
					sec.trs = append(sec.trs, tr)
				}
			}
			switch c.Tag {
			case "thead":
				if len(heads) == 0 {
					heads = append(heads, sec)
				} else {
					bodies = append(bodies, sec)
				}
			case "tfoot":
				if len(foots) == 0 {
					foots = append(foots, sec)
				} else {
					bodies = append(bodies, sec)
				}
			default:
				bodies = append(bodies, sec)
			}
		}
	}
	flushLoose()

	// Place cells on the grid; rowspans block slots in later rows of the
	// same section.
	for _, sec := range append(append(heads, bodies...), foots...) {
		occupied := map[[2]int]bool{}
		first := len(t.rows)
		var secRows []*tableRow
		for _, tr := range sec.trs {
			rst := e.computeStyle(tr, sec.st)
			if rst.display == "none" {
				continue
			}
			var gst *style
			if sec.node != nil {
				gst = sec.st
			}
			secRows = append(secRows, &tableRow{node: tr, st: rst, groupSt: gst, group: sec.group})
		}
		for ri, row := range secRows {
			col := 0
			for _, c := range row.node.Children {
				if c.Type != htmlparse.ElementNode || (c.Tag != "td" && c.Tag != "th") {
					continue
				}
				cst := e.computeStyle(c, row.st)
				if cst.display == "none" {
					continue
				}
				for occupied[[2]int{ri, col}] {
					col++
				}
				cs := clampInt(c.Attr("colspan"), 1, 1000)
				rs := clampInt(c.Attr("rowspan"), 1, 65534)
				if c.Attr("rowspan") == "0" || rs > len(secRows)-ri {
					rs = len(secRows) - ri
				}
				cell := &tableCell{node: c, st: cst, row: first + ri, col: col, colspan: cs, rowspan: rs}
				row.cells = append(row.cells, cell)
				for r := ri; r < ri+rs; r++ {
					for k := col; k < col+cs; k++ {
						occupied[[2]int{r, k}] = true
					}
				}
				col += cs
				t.cols = max(t.cols, col)
			}
		}
		t.rows = append(t.rows, secRows...)
	}
	t.cols = max(t.cols, len(colStyles))
	t.colSt = make([]*style, t.cols)
	copy(t.colSt, colStyles)
	return t
}

func clampInt(s string, lo, hi int) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v < lo {
		return lo
	}
	return min(v, hi)
}

// insets returns the space between a cell's border box and its content:
// in the collapsing model only half of each border lies inside the cell.
func (t *tableModel) insets(st *style) (l, r, top, bot float64) {
	f := 1.0
	if t.collapse {
		f = 0.5
	}
	return st.bw(3)*f + st.padding[3], st.bw(1)*f + st.padding[1], st.bw(0)*f + st.padding[0], st.bw(2)*f + st.padding[2]
}

func cellExtras(st *style) float64 {
	return st.padding[1] + st.padding[3] + st.bw(1) + st.bw(3)
}

// columnRange returns min/max widths for every column (cell border boxes),
// plus the widths fixed by CSS/attributes (0 = auto).
func (e *engine) columnRange(t *tableModel, ref float64) (mins, maxs, fixed []float64) {
	mins = make([]float64, t.cols)
	maxs = make([]float64, t.cols)
	fixed = make([]float64, t.cols)
	for i, cs := range t.colSt {
		if cs != nil {
			if w, ok := e.resolveLength(cs.widthExpr, cs, ref); ok && w > 0 {
				fixed[i] = w
			}
		}
	}
	type spanned struct {
		c      *tableCell
		mn, mx float64
	}
	var spans []spanned
	for _, r := range t.rows {
		for _, c := range r.cells {
			mn, mx := e.measure(c.node, c.st)
			l, rr, _, _ := t.insets(c.st)
			ex := l + rr
			mn, mx = mn+ex, mx+ex
			if c.colspan > 1 {
				spans = append(spans, spanned{c, mn, mx})
				continue
			}
			if cw, ok := e.boxWidth(c.st, ref); ok {
				if !c.st.borderBox {
					cw += ex
				}
				fixed[c.col] = max(fixed[c.col], cw)
			}
			mins[c.col] = max(mins[c.col], mn)
			maxs[c.col] = max(maxs[c.col], mx)
		}
	}
	for i := range fixed {
		if fixed[i] > 0 {
			maxs[i] = max(fixed[i], mins[i])
		}
	}
	for _, s := range spans {
		end := min(s.c.col+s.c.colspan, t.cols)
		sumMin, sumMax := 0.0, 0.0
		for i := s.c.col; i < end; i++ {
			sumMin += mins[i]
			sumMax += maxs[i]
		}
		inner := t.hsp * float64(end-s.c.col-1)
		n := float64(end - s.c.col)
		if s.mn > sumMin+inner {
			for i := s.c.col; i < end; i++ {
				mins[i] += (s.mn - sumMin - inner) / n
			}
		}
		if s.mx > sumMax+inner {
			for i := s.c.col; i < end; i++ {
				maxs[i] += (s.mx - sumMax - inner) / n
			}
		}
	}
	for i := range maxs {
		maxs[i] = max(maxs[i], mins[i])
	}
	return
}

func distribute(mins, maxs, fixed []float64, total float64) []float64 {
	n := len(mins)
	w := make([]float64, n)
	sumMin, sumMax := 0.0, 0.0
	for i := range mins {
		sumMin += mins[i]
		sumMax += maxs[i]
	}
	switch {
	case total >= sumMax:
		copy(w, maxs)
		extra := total - sumMax
		// Give the remaining space to auto columns, by content width.
		weight, autoCount := 0.0, 0
		for i := range w {
			if fixed[i] == 0 {
				weight += maxs[i]
				autoCount++
			}
		}
		for i := range w {
			switch {
			case autoCount == 0:
				w[i] += extra / float64(n)
			case fixed[i] > 0:
			case weight > 0:
				w[i] += extra * maxs[i] / weight
			default:
				w[i] += extra / float64(autoCount)
			}
		}
	case total >= sumMin && sumMax > sumMin:
		for i := range w {
			w[i] = mins[i] + (maxs[i]-mins[i])*(total-sumMin)/(sumMax-sumMin)
		}
	default:
		for i := range w {
			if sumMin > 0 {
				w[i] = mins[i] * total / sumMin
			} else {
				w[i] = total / float64(n)
			}
		}
	}
	return w
}

// fixedColumns implements table-layout: fixed: widths come from <col> and
// the first row only; the rest is shared equally.
func (e *engine) fixedColumns(t *tableModel, total float64) []float64 {
	w := make([]float64, t.cols)
	set := make([]bool, t.cols)
	for i, cs := range t.colSt {
		if cs != nil {
			if v, ok := e.resolveLength(cs.widthExpr, cs, total); ok && v > 0 {
				w[i], set[i] = v, true
			}
		}
	}
	if len(t.rows) > 0 {
		for _, c := range t.rows[0].cells {
			if set[c.col] {
				continue
			}
			if v, ok := e.boxWidth(c.st, total); ok {
				l, r, _, _ := t.insets(c.st)
				if !c.st.borderBox {
					v += l + r
				}
				per := v / float64(c.colspan)
				for k := c.col; k < c.col+c.colspan && k < t.cols; k++ {
					w[k], set[k] = per, true
				}
			}
		}
	}
	used, free := 0.0, 0
	for i := range w {
		if set[i] {
			used += w[i]
		} else {
			free++
		}
	}
	if free > 0 {
		each := max(total-used, 0) / float64(free)
		for i := range w {
			if !set[i] {
				w[i] = each
			}
		}
	} else if used > 0 && used < total {
		for i := range w {
			w[i] *= total / used
		}
	}
	return w
}

// tableGeom holds the resolved geometry of a table.
type tableGeom struct {
	tx, tw float64   // border box of the table
	colX   []float64 // left edge of each column's cells; colX[cols] is the right edge
	colW   []float64
}

func (g *tableGeom) cellBox(t *tableModel, c *tableCell) (float64, float64) {
	end := min(c.col+c.colspan, t.cols)
	return g.colX[c.col], g.colX[end-1] + g.colW[end-1] - g.colX[c.col]
}

func (e *engine) tableGeometry(t *tableModel, st *style, x, w float64) tableGeom {
	avail := w - st.margin[1] - st.margin[3]
	// frame: what lies between the table border box and the cell grid
	var frameL, frameR float64
	if t.collapse {
		frameL, frameR = st.bw(3)/2, st.bw(1)/2
	} else {
		frameL, frameR = st.bw(3)+st.padding[3]+t.hsp, st.bw(1)+st.padding[1]+t.hsp
	}
	gaps := t.hsp * float64(max(t.cols-1, 0))
	frame := frameL + frameR + gaps
	tw, fixedWidth := e.borderBoxWidth(st, avail)
	var colW []float64
	switch {
	case st.tableFixed && fixedWidth:
		colW = e.fixedColumns(t, max(tw-frame, 0))
	default:
		ref := avail - frame
		if fixedWidth {
			ref = tw - frame
		}
		mins, maxs, fixed := e.columnRange(t, ref)
		if !fixedWidth {
			sum := frame
			for _, m := range maxs {
				sum += m
			}
			minSum := frame
			for _, m := range mins {
				minSum += m
			}
			tw = max(min(sum, avail), minSum)
		} else {
			minSum := frame
			for _, m := range mins {
				minSum += m
			}
			tw = max(tw, minSum)
		}
		colW = distribute(mins, maxs, fixed, tw-frame)
	}
	total := frame
	for _, c := range colW {
		total += c
	}
	tw = max(tw, total)

	tx := x + st.margin[3]
	switch {
	case st.autoMargin[3] && st.autoMargin[1]:
		tx += max(avail-tw, 0) / 2
	case st.autoMargin[3]:
		tx += max(avail-tw, 0)
	}
	g := tableGeom{tx: tx, tw: tw, colW: colW, colX: make([]float64, t.cols+1)}
	cx := tx + frameL
	for i, cw := range colW {
		g.colX[i] = cx
		cx += cw + t.hsp
	}
	g.colX[t.cols] = cx - t.hsp
	return g
}

// rowHeights measures every cell and resolves row heights, letting rowspan
// cells stretch the last row they span.
func (e *engine) rowHeights(t *tableModel, g *tableGeom) (rowH []float64, cellH map[*tableCell]float64) {
	rowH = make([]float64, len(t.rows))
	cellH = map[*tableCell]float64{}
	for ri, r := range t.rows {
		rowH[ri] = e.boxHeight(r.st)
		for _, c := range r.cells {
			x, w := g.cellBox(t, c)
			h := e.layoutCell(t, c, x, w, 0, 0, true)
			cellH[c] = h
			if c.rowspan == 1 {
				rowH[ri] = max(rowH[ri], h)
			}
		}
	}
	for ri, r := range t.rows {
		for _, c := range r.cells {
			if c.rowspan < 2 {
				continue
			}
			end := min(ri+c.rowspan, len(t.rows))
			have := t.vsp * float64(end-ri-1)
			for k := ri; k < end; k++ {
				have += rowH[k]
			}
			if need := cellH[c]; need > have {
				rowH[end-1] += need - have
			}
		}
	}
	return
}

// layoutCell lays out a cell's content starting at top+offset and returns
// the height used, including padding and borders. measure=true lays out
// without drawing.
func (e *engine) layoutCell(t *tableModel, c *tableCell, x, w, top, offset float64, measure bool) float64 {
	s := e.saveState()
	if measure {
		e.dry = true
	}
	e.noBreak = true
	e.markers = nil
	e.resetMargin()
	e.atTop = true
	st := c.st
	l, r, it, ib := t.insets(st)
	e.y = top + offset + it
	cw := max(w-l-r, 1)
	savedFloats := e.floats
	e.floats = nil
	e.depth += 2
	e.layoutChildren(c.node, st, x+l, cw)
	e.y = e.floatsBottomSince(0, e.y) // cells contain their floats
	e.floats = savedFloats
	e.depth -= 2
	h := e.y - top - offset + ib
	if sh := e.boxHeight(st); sh > h {
		h = sh
	}
	e.restoreState(s)
	return h
}

// spanHeight returns the height of a cell spanning rows from ri.
func (t *tableModel) spanHeight(rowH []float64, c *tableCell) float64 {
	end := min(c.row+c.rowspan, len(rowH))
	h := t.vsp * float64(end-c.row-1)
	for k := c.row; k < end; k++ {
		h += rowH[k]
	}
	return h
}

// paintCellBackgrounds paints, in CSS order, the column, row group, row
// and cell backgrounds of one cell area.
func (e *engine) paintCellBackgrounds(p *pdf.Page, t *tableModel, row *tableRow, c *tableCell, x, y, w, h float64, top, bottom bool) {
	d := e.depth
	if c.col < len(t.colSt) && t.colSt[c.col] != nil {
		e.paintBox(p, d, x, y, w, h, noBorders(t.colSt[c.col]), top, bottom)
	}
	if row.groupSt != nil {
		e.paintBox(p, d, x, y, w, h, noBorders(row.groupSt), top, bottom)
	}
	e.paintBox(p, d+1, x, y, w, h, noBorders(row.st), top, bottom)
	if t.collapse {
		e.paintBox(p, d+2, x, y, w, h, noBorders(c.st), top, bottom)
		bs := *c.st
		if !top {
			bs.borderStyle[0] = ""
		}
		if !bottom {
			bs.borderStyle[2] = ""
		}
		drawCellBorders(p, &bs, x, y, w, h)
	} else {
		e.paintBox(p, d+2, x, y, w, h, c.st, top, bottom)
	}
}

// drawRow draws the cells starting in row ri with their tops at e.y.
func (e *engine) drawRow(t *tableModel, g *tableGeom, ri int, rowH []float64, cellH map[*tableCell]float64) {
	top := e.y
	row := t.rows[ri]
	for _, c := range row.cells {
		x, w := g.cellBox(t, c)
		h := t.spanHeight(rowH, c)
		offset := 0.0
		switch c.st.valign {
		case "top":
		case "bottom":
			offset = h - cellH[c]
		default: // middle is the table-cell default
			offset = (h - cellH[c]) / 2
		}
		if !e.dry {
			e.paintCellBackgrounds(e.page(), t, row, c, x, top, w, h, true, true)
		}
		e.layoutCell(t, c, x, w, top, offset, false)
	}
	if !e.dry && t.collapse {
		drawCellBorders(e.page(), row.st, g.colX[0], top, g.colX[t.cols]-g.colX[0], rowH[ri])
	}
	e.y = top + rowH[ri]
}

// drawRowSplit draws a row taller than a page: every cell flows over as
// many pages as it needs, then the row's boxes are painted page by page.
func (e *engine) drawRowSplit(t *tableModel, g *tableGeom, ri int) {
	row := t.rows[ri]
	startPage, startY := e.pageIdx, e.y
	endPage, endY := startPage, startY
	for _, c := range row.cells {
		x, w := g.cellBox(t, c)
		l, r, it, ib := t.insets(c.st)
		s := e.saveState()
		e.pageIdx, e.y = startPage, startY+it
		e.noBreak = false
		e.markers = nil
		e.atTop = false
		e.resetMargin()
		savedFloats := e.floats
		e.floats = nil
		e.depth += 2
		e.layoutChildren(c.node, c.st, x+l, max(w-l-r, 1))
		e.depth -= 2
		e.floats = savedFloats
		y := e.y + ib
		if e.pageIdx > endPage || e.pageIdx == endPage && y > endY {
			endPage, endY = e.pageIdx, y
		}
		e.restoreState(s)
	}
	for _, c := range row.cells {
		x, w := g.cellBox(t, c)
		for pi := startPage; pi <= endPage; pi++ {
			top, bot := e.top, e.bottom
			if pi == startPage {
				top = startY
			}
			if pi == endPage {
				bot = endY
			}
			if bot > top {
				e.paintCellBackgrounds(e.pages[pi], t, row, c, x, top, w, bot-top, pi == startPage, pi == endPage)
			}
		}
	}
	e.pageIdx, e.y = endPage, endY
}

// rowGroupEnd returns the index after the last row tied to row ri by
// rowspans (such rows are kept on one page).
func (t *tableModel) rowGroupEnd(ri int) int {
	end := ri + 1
	for k := ri; k < end && k < len(t.rows); k++ {
		for _, c := range t.rows[k].cells {
			end = max(end, min(k+c.rowspan, len(t.rows)))
		}
	}
	return end
}

func (e *engine) layoutTable(n *htmlparse.Node, st *style, x, w float64) {
	t := e.buildTable(n, st)
	e.addMargin(st.margin[0])
	if t.cols == 0 {
		for i, c := range t.captions {
			e.layoutBox(c, t.captionSt[i], x, w)
		}
		e.addMargin(st.margin[2])
		return
	}
	g := e.tableGeometry(t, st, x, w)
	for i, c := range t.captions {
		if t.captionSt[i].captionSide != "bottom" {
			e.layoutBox(c, t.captionSt[i], g.tx, g.tw)
		}
	}
	rowH, cellH := e.rowHeights(t, &g)

	var head, foot []int
	for ri, r := range t.rows {
		switch r.group {
		case "head":
			head = append(head, ri)
		case "foot":
			foot = append(foot, ri)
		}
	}
	groupH := func(from, to int) float64 {
		h := 0.0
		for k := from; k < to; k++ {
			h += rowH[k] + t.vsp
		}
		return h
	}
	headH, footH := 0.0, 0.0
	for _, ri := range head {
		headH += rowH[ri] + t.vsp
	}
	for _, ri := range foot {
		footH += rowH[ri] + t.vsp
	}

	e.applyMargin()
	startPage, startY := e.pageIdx, e.y
	var topInset, botInset float64
	if t.collapse {
		topInset, botInset = st.bw(0)/2, st.bw(2)/2
	} else {
		topInset, botInset = st.bw(0)+st.padding[0]+t.vsp, st.bw(2)+st.padding[2]
	}
	e.y += topInset
	e.depth++
	drawGroup := func(rows []int) {
		for _, ri := range rows {
			e.drawRow(t, &g, ri, rowH, cellH)
			e.y += t.vsp
		}
	}
	paging := !e.noBreak && !e.dry
	pageBodyStart := e.y
	drawGroup(head)
	pageBodyStart = e.y
	for ri := 0; ri < len(t.rows); {
		if t.rows[ri].group != "body" {
			ri++
			continue
		}
		end := t.rowGroupEnd(ri)
		h := groupH(ri, end)
		if paging && e.y+h+footH > e.bottom+0.01 && e.y > pageBodyStart+0.5 {
			drawGroup(foot) // repeat the footer at the bottom of the page
			e.decorateTable(t, &g, st, startPage, startY, e.y, true)
			e.newPage()
			e.atTop = false
			startPage, startY = e.pageIdx, e.y
			drawGroup(head)
			pageBodyStart = e.y
		}
		if paging && end == ri+1 && e.y+h > e.bottom+0.01 {
			e.drawRowSplit(t, &g, ri)
			e.y += t.vsp
			ri = end
			continue
		}
		for k := ri; k < end; k++ {
			e.drawRow(t, &g, k, rowH, cellH)
			e.y += t.vsp
		}
		ri = end
	}
	drawGroup(foot)
	e.y += botInset
	e.depth--
	e.decorateTable(t, &g, st, startPage, startY, e.y, false)
	for i, c := range t.captions {
		if t.captionSt[i].captionSide == "bottom" {
			e.layoutBox(c, t.captionSt[i], g.tx, g.tw)
		}
	}
	e.addMargin(st.margin[2])
}

// decorateTable paints the table box (background, borders) for the part
// from (startPage, startY) to endY on the current page. continued means
// the table goes on on the next page.
func (e *engine) decorateTable(t *tableModel, g *tableGeom, st *style, startPage int, startY, endY float64, continued bool) {
	if e.dry {
		return
	}
	if !t.collapse {
		bs := *st
		if continued {
			bs.borderStyle[2] = ""
		}
		e.decorate(startPage, startY, endY, g.tx, g.tw, &bs)
		return
	}
	e.decorate(startPage, startY, endY, g.tx, g.tw, noBorders(st))
	// collapsed: the table border is centred on the outer grid lines
	gx0, gx1 := g.colX[0], g.colX[t.cols]
	gy0, gy1 := startY+st.bw(0)/2, endY-st.bw(2)/2
	for pi := startPage; pi <= e.pageIdx; pi++ {
		top, bot := e.top, e.bottom
		if pi == startPage {
			top = gy0
		}
		if pi == e.pageIdx {
			bot = gy1
		}
		bs := *st
		if pi != startPage {
			bs.borderStyle[0] = ""
		}
		if pi != e.pageIdx || continued {
			bs.borderStyle[2] = ""
		}
		drawCellBorders(e.pages[pi], &bs, gx0, top, gx1-gx0, bot-top)
	}
}

// noBorders returns a copy of a style without borders.
func noBorders(st *style) *style {
	if !st.hasBorder() {
		return st
	}
	cp := *st
	cp.borderStyle = [4]string{}
	return &cp
}

func drawCellBorders(p *pdf.Page, st *style, x, y, w, h float64) {
	// Borders are centred on the cell edges so neighbouring cells overlap
	// exactly (border-collapse: collapse).
	if !st.visible {
		return
	}
	fg := pdf.Layer{}
	line := func(side int, x1, y1, x2, y2 float64) {
		b := st.bw(side)
		if b <= 0 {
			return
		}
		c := st.paint(st.borderColor[side])
		if s := st.borderStyle[side]; s == "double" && b >= 3 {
			dx, dy := 0.0, 0.0
			if y1 == y2 {
				dy = b / 3
			} else {
				dx = b / 3
			}
			p.StyledLine(fg, x1-dx, y1-dy, x2-dx, y2-dy, b/3, c, pdf.Dash{})
			p.StyledLine(fg, x1+dx, y1+dy, x2+dx, y2+dy, b/3, c, pdf.Dash{})
			return
		}
		p.StyledLine(fg, x1, y1, x2, y2, b, c, dashFor(st.borderStyle[side], b))
	}
	line(0, x, y, x+w, y)
	line(2, x, y+h, x+w, y+h)
	line(3, x, y, x, y+h)
	line(1, x+w, y, x+w, y+h)
}

// measure returns the minimum (longest word) and maximum (no wrapping)
// content widths of an element's children.
func (e *engine) measure(n *htmlparse.Node, st *style) (mn, mx float64) {
	if st.isFlex() {
		return e.measureFlex(n, st)
	}
	if st.isGrid() {
		return e.measureGrid(n, st)
	}
	var items []item
	e.collect(n, st, &items)
	line := 0.0
	pending := false
	for _, it := range items {
		switch it.kind {
		case kWord:
			ww := e.textWidth(it.st, it.text)
			mn = max(mn, ww)
			if pending && line > 0 {
				line += e.spaceWidth(it.st)
			}
			line += ww
			pending = false
			if it.st.ws == "pre" || it.st.ws == "nowrap" {
				mn = max(mn, line)
			}
		case kSpace:
			pending = true
		case kGap:
			line += it.w
		case kAtomic:
			amn, amx := e.measureBlock(it.node, it.st)
			// a percentage width is unknown while measuring (it behaves as
			// auto for intrinsic sizes), so only fixed widths apply here
			if !isReplaced(it.node) && !strings.Contains(it.st.widthExpr, "%") {
				if bw, ok := e.borderBoxWidth(it.st, e.contentWidth); ok {
					amn = bw + it.st.margin[1] + it.st.margin[3]
					amx = amn
				}
			}
			if pending && line > 0 {
				line += e.spaceWidth(it.st)
			}
			pending = false
			mn = max(mn, amn)
			line += amx
		case kBreak:
			mx = max(mx, line)
			line, pending = 0, false
		case kBlock:
			mx = max(mx, line)
			line, pending = 0, false
			bmn, bmx := e.measureBlock(it.node, it.st)
			mn = max(mn, bmn)
			mx = max(mx, bmx)
		case kFloat:
			// a float sits beside the line's content, so it adds to the
			// max-content width
			fmn, fmx := e.measureBlock(it.node, blockify(it.st))
			mn = max(mn, fmn)
			line += fmx
		}
	}
	mx = max(mx, line)
	return
}

// measureBlock returns the min-content and max-content margin-box widths.
func (e *engine) measureBlock(n *htmlparse.Node, st *style) (float64, float64) {
	margins := st.margin[1] + st.margin[3]
	extra := margins + st.extrasH()
	fixed := func() (float64, bool) {
		if strings.Contains(st.widthExpr, "%") {
			return 0, false
		}
		return e.borderBoxWidth(st, e.contentWidth)
	}
	switch {
	case isReplaced(n):
		w, _ := e.imageSize(n, st, 1e6)
		if strings.Contains(st.widthExpr, "%") {
			return 0, w + extra
		}
		return w + extra, w + extra
	case n.Tag == "table" || st.display == "table" || st.display == "inline-table":
		t := e.buildTable(n, st)
		mins, maxs, _ := e.columnRange(t, e.contentWidth)
		smn, smx := 0.0, 0.0
		for i := range mins {
			smn += mins[i]
			smx += maxs[i]
		}
		if !t.collapse {
			frame := t.hsp * float64(t.cols+1)
			smn, smx = smn+frame, smx+frame
		}
		if bw, ok := fixed(); ok {
			smx = max(smx+st.extrasH(), bw) - st.extrasH()
		}
		return smn + extra, smx + extra
	}
	inner := st
	if st.isAtomicInline() {
		inner = e.atomicInner(st)
	}
	// list markers sit outside the box, so they add no width (an <li> used
	// as a flex item in a nav bar must be as wide as its link)
	mn, mx := e.measure(n, inner)
	if bw, ok := fixed(); ok {
		return max(mn+st.extrasH(), bw) + margins, bw + margins
	}
	if mxw, ok := e.resolveLength(st.maxWidthExpr, st, e.contentWidth); ok && !strings.Contains(st.maxWidthExpr, "%") {
		if !st.borderBox {
			mxw += st.extrasH()
		}
		mx = min(mx+st.extrasH(), max(mxw, mn+st.extrasH())) - st.extrasH()
	}
	return mn + extra, mx + extra
}
