package render

import (
	"math"
	"strconv"
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
)

// Fragmentation controls (CSS Fragmentation Level 3): break-inside: avoid,
// break-after: avoid, orphans and widows. And CSS transforms.

func (lb *lineBuilder) logFlag() int {
	if lb.log {
		return 1
	}
	return 0
}

// forcedBreak starts a new page before the line chosen by the
// orphans/widows pass.
func (lb *lineBuilder) forcedBreak() {
	if lb.forceAt < 0 || lb.lines != lb.forceAt {
		return
	}
	e := lb.e
	e.applyMargin()
	if e.y > e.top+0.5 {
		e.newPage()
		e.atTop = false
	}
}

// orphansWidowsBreak lays a paragraph out without drawing to find where it
// would cross the page end, then returns the line index that must start
// the next page so that at least `orphans` lines stay at the bottom and
// `widows` lines go to the top (-1: no adjustment needed).
func (e *engine) orphansWidowsBreak(items []item, st *style, x, w float64) int {
	o, wd := max(st.orphans, 1), max(st.widows, 1)
	if o <= 1 && wd <= 1 {
		return -1
	}
	s := e.saveState()
	savedLog := e.lineLog
	e.dry, e.noBreak = true, true
	e.lineLog = nil
	e.flushLines(items, st, x, w, true, -1)
	lines := e.lineLog
	e.lineLog = savedLog
	e.restoreState(s)

	n := len(lines)
	k := -1
	for i, l := range lines {
		if l[0]+l[1] > e.bottom+0.01 {
			k = i
			break
		}
	}
	if k <= 0 {
		return -1 // fits, or breaks before the first line anyway
	}
	atTop := lines[0][0] <= e.top+0.5
	kk := k
	if k < o {
		kk = 0
	}
	if n-kk < wd {
		kk = n - wd
		if kk < o {
			kk = 0
		}
	}
	if kk == 0 && atTop {
		return -1 // the paragraph cannot do better than start of page
	}
	if kk >= k {
		return -1
	}
	return kk
}

// keepTogether handles break-inside: avoid and break-after: avoid for the
// block items[i]: when it (plus the start of what follows, for
// break-after) does not fit on the rest of the page but would fit on an
// empty one, a page break is requested before it.
func (e *engine) keepTogether(items []item, i int, x, w float64) {
	it := items[i]
	st := it.st
	if !st.avoidInside && !st.avoidAfter {
		return
	}
	need := e.blockOuterHeight(it.node, st, w)
	if st.avoidAfter {
		need += e.firstChunkHeight(items[i+1:])
	}
	y := e.y + e.pendingMargin
	if need > 0 && y+need > e.bottom+0.01 && y > e.top+0.5 && need <= e.bottom-e.top {
		e.breakPending = true
	}
}

// blockOuterHeight measures a block's margin-box height.
func (e *engine) blockOuterHeight(n *htmlparse.Node, st *style, w float64) float64 {
	bw, ok := e.borderBoxWidth(st, w)
	if !ok {
		bw = w - st.margin[1] - st.margin[3]
	}
	h, _ := e.measureBox(n, st, max(bw, 1))
	return h + st.margin[0] + st.margin[2]
}

// firstChunkHeight estimates the height of the beginning of the content
// that follows (its first `orphans` lines), for break-after: avoid.
func (e *engine) firstChunkHeight(rest []item) float64 {
	for _, it := range rest {
		switch it.kind {
		case kWord, kAtomic:
			a, b := e.vmetrics(it.st)
			return (a + b) * float64(max(it.st.orphans, 1))
		case kBlock:
			s := it.st
			a, b := e.vmetrics(s)
			lines := (a + b) * float64(max(s.orphans, 1))
			if s.avoidInside || isReplaced(it.node) || it.node.Tag == "table" {
				return e.blockOuterHeight(it.node, s, e.contentWidth)
			}
			return s.margin[0] + s.bw(0) + s.padding[0] + lines
		}
	}
	return 0
}

// ---------------------------------------------------------------------
// Transforms

type transformMark struct {
	fg, bg, page int
}

func (e *engine) transformStart(st *style) transformMark {
	if st.xform == "" || st.xform == "none" || e.dry {
		return transformMark{-1, -1, -1}
	}
	p := e.page()
	return transformMark{p.ForegroundMark(), p.BackgroundMark(), e.pageIdx}
}

// transformEnd applies the element's transform to everything it drew
// since the mark; (x, y, w, h) is its border box.
func (e *engine) transformEnd(st *style, m transformMark, x, y, w, h float64) {
	if m.fg < 0 || e.dry || m.page != e.pageIdx {
		return
	}
	mat, ok := e.cssMatrix(st, w, h)
	if !ok {
		return
	}
	ox, oy := e.bgPosition(orDefault(st.xformOrigin, "50% 50%"), st, w, h)
	ox, oy = x+ox, y+oy
	a, b, c, d, tx, ty := mat[0], mat[1], mat[2], mat[3], mat[4], mat[5]
	// CSS: p' = O + L(p - O) + t, in y-down page coordinates.
	tX := ox - (a*ox + c*oy) + tx
	tY := oy - (b*ox + d*oy) + ty
	// Conjugate with the y flip to get the PDF matrix.
	H := e.doc.Height
	pdfM := [6]float64{a, -b, -c, d, c*H + tX, H - d*H - tY}
	e.pages[m.page].TransformSince(m.fg, m.bg, pdfM)
}

// cssMatrix composes a transform list into [a b c d e f]
// (x' = a x + c y + e, y' = b x + d y + f).
func (e *engine) cssMatrix(st *style, w, h float64) ([6]float64, bool) {
	total := [6]float64{1, 0, 0, 1, 0, 0}
	mul := func(m, n [6]float64) [6]float64 {
		return [6]float64{
			m[0]*n[0] + m[2]*n[1], m[1]*n[0] + m[3]*n[1],
			m[0]*n[2] + m[2]*n[3], m[1]*n[2] + m[3]*n[3],
			m[0]*n[4] + m[2]*n[5] + m[4], m[1]*n[4] + m[3]*n[5] + m[5],
		}
	}
	length := func(v string, ref float64) float64 {
		l, _ := e.resolveLength(strings.TrimSpace(v), st, ref)
		if strings.TrimSpace(v) == "0" {
			return 0
		}
		return l
	}
	angle := func(v string) float64 {
		v = strings.TrimSpace(v)
		if v == "0" {
			return 0
		}
		a, _ := parseAngle(v)
		return a * math.Pi / 180
	}
	num := func(v string) float64 {
		v = strings.TrimSpace(v)
		if strings.HasSuffix(v, "%") {
			f, _ := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
			return f / 100
		}
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	any := false
	for _, fn := range splitValue(st.xform) {
		open := strings.IndexByte(fn, '(')
		if open < 0 || !strings.HasSuffix(fn, ")") {
			continue
		}
		name := fn[:open]
		args := strings.FieldsFunc(fn[open+1:len(fn)-1], func(r rune) bool { return r == ',' || r == ' ' })
		if len(args) == 0 {
			continue
		}
		arg := func(i int) string {
			if i < len(args) {
				return args[i]
			}
			return ""
		}
		var m [6]float64
		switch name {
		case "translate":
			ty := 0.0
			if arg(1) != "" {
				ty = length(arg(1), h)
			}
			m = [6]float64{1, 0, 0, 1, length(arg(0), w), ty}
		case "translatex":
			m = [6]float64{1, 0, 0, 1, length(arg(0), w), 0}
		case "translatey":
			m = [6]float64{1, 0, 0, 1, 0, length(arg(0), h)}
		case "scale":
			sx := num(arg(0))
			sy := sx
			if arg(1) != "" {
				sy = num(arg(1))
			}
			m = [6]float64{sx, 0, 0, sy, 0, 0}
		case "scalex":
			m = [6]float64{num(arg(0)), 0, 0, 1, 0, 0}
		case "scaley":
			m = [6]float64{1, 0, 0, num(arg(0)), 0, 0}
		case "rotate", "rotatez":
			r := angle(arg(len(args) - 1))
			m = [6]float64{math.Cos(r), math.Sin(r), -math.Sin(r), math.Cos(r), 0, 0}
		case "skew":
			ay := 0.0
			if arg(1) != "" {
				ay = angle(arg(1))
			}
			m = [6]float64{1, math.Tan(ay), math.Tan(angle(arg(0))), 1, 0, 0}
		case "skewx":
			m = [6]float64{1, 0, math.Tan(angle(arg(0))), 1, 0, 0}
		case "skewy":
			m = [6]float64{1, math.Tan(angle(arg(0))), 0, 1, 0, 0}
		case "matrix":
			if len(args) != 6 {
				continue
			}
			for i := range m {
				m[i] = num(args[i])
			}
			// the translation of matrix() is in px
			m[4], m[5] = m[4]*0.75, m[5]*0.75
		default:
			continue // 3D functions are not supported
		}
		total = mul(total, m)
		any = true
	}
	return total, any
}

// pageCounters replaces counter(page) / counter(pages) placeholders with
// the current page number and the page count.
func (e *engine) pageCounters(s string) string {
	var sb strings.Builder
	for {
		i := strings.IndexByte(s, 0)
		if i < 0 {
			sb.WriteString(s)
			return sb.String()
		}
		j := strings.IndexByte(s[i+1:], 0)
		if j < 0 {
			sb.WriteString(s)
			return sb.String()
		}
		sb.WriteString(s[:i])
		name, styleName, _ := strings.Cut(s[i+1:i+1+j], ":")
		v := e.pageIdx + 1
		if name == "pages" {
			v = len(e.pages)
		}
		sb.WriteString(formatCounter(v, styleName))
		s = s[i+j+2:]
	}
}
