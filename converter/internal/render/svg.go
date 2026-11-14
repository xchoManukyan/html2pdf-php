package render

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/css"
	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
	"github.com/xchoManukyan/html2pdf/converter/internal/pdf"
)

// SVG 1.1 / 2 static subset rendered to PDF vectors: shapes, paths (incl.
// arcs), groups, transforms, nested svg / viewBox / preserveAspectRatio,
// use + symbol, linear and radial gradients, clip paths, text, embedded
// images, <style> sheets, presentation attributes and style="".

type svgDoc struct {
	root   *htmlparse.Node
	width  float64 // intrinsic size in px
	height float64
	ids    map[string]*htmlparse.Node
	sheet  *css.Stylesheet
}

// ---------------------------------------------------------------------
// Parsing

// parseSVGBytes parses an SVG file (XML) into the same node structure the
// HTML parser produces (lower-case tag and attribute names).
func parseSVGBytes(data []byte) *svgDoc {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity
	root := &htmlparse.Node{Type: htmlparse.DocumentNode}
	stack := []*htmlparse.Node{root}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		cur := stack[len(stack)-1]
		switch t := tok.(type) {
		case xml.StartElement:
			n := &htmlparse.Node{Type: htmlparse.ElementNode, Tag: strings.ToLower(t.Name.Local), Attrs: map[string]string{}, Parent: cur}
			for _, a := range t.Attr {
				n.Attrs[strings.ToLower(a.Name.Local)] = a.Value
			}
			cur.Children = append(cur.Children, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			cur.Children = append(cur.Children, &htmlparse.Node{Type: htmlparse.TextNode, Text: string(t), Parent: cur})
		}
	}
	svg := root.Find("svg")
	if svg == nil {
		return nil
	}
	return newSVGDoc(svg)
}

func (e *engine) svgFromNode(n *htmlparse.Node) *svgDoc { return newSVGDoc(n) }

func newSVGDoc(svg *htmlparse.Node) *svgDoc {
	doc := &svgDoc{root: svg, ids: map[string]*htmlparse.Node{}, sheet: css.NewStylesheet()}
	var walk func(*htmlparse.Node)
	walk = func(n *htmlparse.Node) {
		for _, c := range n.Children {
			if c.Type != htmlparse.ElementNode {
				continue
			}
			if id := c.Attr("id"); id != "" {
				doc.ids[id] = c
			}
			if c.Tag == "style" {
				doc.sheet.Add(c.TextContent(), css.Env{Width: 1000, Height: 1000})
			}
			walk(c)
		}
	}
	walk(svg)
	vb, hasVB := parseViewBox(svg.Attr("viewbox"))
	w, wok := svgAbsLen(svg.Attr("width"))
	h, hok := svgAbsLen(svg.Attr("height"))
	switch {
	case wok && hok:
	case wok && hasVB && vb[2] > 0:
		h = w * vb[3] / vb[2]
	case hok && hasVB && vb[3] > 0:
		w = h * vb[2] / vb[3]
	case hasVB:
		w, h = vb[2], vb[3]
	default:
		w, h = 300, 150
	}
	if w <= 0 || h <= 0 {
		w, h = 300, 150
	}
	doc.width, doc.height = w, h
	return doc
}

func parseViewBox(v string) ([4]float64, bool) {
	f := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
	var vb [4]float64
	if len(f) != 4 {
		return vb, false
	}
	for i := range vb {
		x, err := strconv.ParseFloat(f[i], 64)
		if err != nil {
			return vb, false
		}
		vb[i] = x
	}
	return vb, vb[2] > 0 && vb[3] > 0
}

// svgAbsLen parses an absolute SVG length to px (percentages are not
// absolute).
func svgAbsLen(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasSuffix(v, "%") {
		return 0, false
	}
	return svgLen(v, 0, 16), true
}

// svgLen converts an SVG length to user units (px). ref is the reference
// for percentages, em the font size.
func svgLen(v string, ref, em float64) float64 {
	v = strings.TrimSpace(strings.ToLower(v))
	units := []struct {
		s string
		f float64
	}{{"px", 1}, {"pt", 4.0 / 3}, {"pc", 16}, {"mm", 96 / 25.4}, {"cm", 96 / 2.54}, {"in", 96}, {"em", em}, {"ex", em / 2}, {"%", ref / 100}}
	for _, u := range units {
		if strings.HasSuffix(v, u.s) {
			x, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, u.s)), 64)
			return x * u.f
		}
	}
	x, _ := strconv.ParseFloat(v, 64)
	return x
}

// ---------------------------------------------------------------------
// Style state

type svgPaint struct {
	none  bool
	color pdf.Color
	ref   string // gradient id for url(#id)
}

type svgState struct {
	fill, stroke     svgPaint
	fillOp, strokeOp float64
	opacity          float64
	strokeW          float64
	evenOdd          bool
	cap, join        int
	miter            float64
	dash             []float64
	dashOff          float64
	color            pdf.Color
	fontSize         float64
	family           string
	weight           int
	italic           bool
	anchor           string
	visible          bool
	vw, vh           float64 // current viewport, for percentages

	markerStart, markerMid, markerEnd string // inherited marker references
	mask, filter, clipPath            string // not inherited (reset per element)
}

func defaultSVGState(w, h float64) svgState {
	return svgState{fill: svgPaint{color: pdf.Color{}}, stroke: svgPaint{none: true}, fillOp: 1, strokeOp: 1,
		opacity: 1, strokeW: 1, miter: 4, fontSize: 16, family: "sans-serif", weight: 400, anchor: "start",
		visible: true, vw: w, vh: h}
}

func (e *engine) svgPaintOf(v string, st *svgState) (svgPaint, bool) {
	v = strings.TrimSpace(v)
	lv := strings.ToLower(v)
	switch {
	case lv == "none" || lv == "transparent":
		return svgPaint{none: true}, true
	case lv == "currentcolor":
		return svgPaint{color: st.color}, true
	case strings.HasPrefix(lv, "url("):
		end := strings.IndexByte(v, ')')
		if end < 0 {
			return svgPaint{}, false
		}
		ref := strings.Trim(strings.TrimSpace(v[4:end]), `"'`)
		p := svgPaint{ref: strings.TrimPrefix(ref, "#")}
		// fallback color after the url
		if rest := strings.TrimSpace(v[end+1:]); rest != "" {
			if c, ok := css.ParseColor(rest); ok {
				p.color = pdf.Color{R: c.R, G: c.G, B: c.B}
			}
		}
		return p, true
	}
	c, ok := css.ParseColor(v)
	if !ok {
		return svgPaint{}, false
	}
	return svgPaint{color: pdf.Color{R: c.R, G: c.G, B: c.B, A: alphaField(c.A)}}, true
}

func alphaField(a float64) float64 {
	if a >= 1 {
		return 0
	}
	return a
}

// applySVGStyle updates the inherited state with the element's
// presentation attributes, stylesheet rules and style attribute.
func (e *engine) applySVGStyle(doc *svgDoc, n *htmlparse.Node, st *svgState) {
	var decls []css.Decl
	for _, p := range []string{"fill", "stroke", "stroke-width", "opacity", "fill-opacity", "stroke-opacity",
		"fill-rule", "stroke-linecap", "stroke-linejoin", "stroke-miterlimit", "stroke-dasharray", "stroke-dashoffset",
		"color", "font-size", "font-family", "font-weight", "font-style", "text-anchor", "visibility", "display", "stop-color", "stop-opacity", "font",
		"marker", "marker-start", "marker-mid", "marker-end", "mask", "filter", "clip-path", "flood-color", "flood-opacity"} {
		if v, ok := n.Attrs[p]; ok {
			decls = append(decls, css.Decl{Prop: p, Value: v})
		}
	}
	decls = append(decls, doc.sheet.Match(n, "")...)
	decls = append(decls, css.ParseDecls(n.Attr("style"))...)
	for _, d := range decls {
		v := strings.TrimSpace(d.Value)
		lv := strings.ToLower(v)
		switch d.Prop {
		case "fill":
			if p, ok := e.svgPaintOf(v, st); ok {
				st.fill = p
			}
		case "stroke":
			if p, ok := e.svgPaintOf(v, st); ok {
				st.stroke = p
			}
		case "color":
			if c, ok := css.ParseColor(v); ok {
				st.color = pdf.Color{R: c.R, G: c.G, B: c.B, A: alphaField(c.A)}
			}
		case "stroke-width":
			st.strokeW = svgLen(v, math.Hypot(st.vw, st.vh)/math.Sqrt2, st.fontSize)
		case "opacity":
			st.opacity *= parseOpacity(v)
		case "fill-opacity":
			st.fillOp = parseOpacity(v)
		case "stroke-opacity":
			st.strokeOp = parseOpacity(v)
		case "fill-rule":
			st.evenOdd = lv == "evenodd"
		case "stroke-linecap":
			st.cap = map[string]int{"butt": 0, "round": 1, "square": 2}[lv]
		case "stroke-linejoin":
			st.join = map[string]int{"miter": 0, "round": 1, "bevel": 2}[lv]
		case "stroke-miterlimit":
			if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 1 {
				st.miter = f
			}
		case "stroke-dasharray":
			st.dash = nil
			if lv != "none" {
				for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
					st.dash = append(st.dash, svgLen(f, st.vw, st.fontSize))
				}
				if len(st.dash)%2 == 1 {
					st.dash = append(st.dash, st.dash...)
				}
			}
		case "stroke-dashoffset":
			st.dashOff = svgLen(v, st.vw, st.fontSize)
		case "font-size":
			if sz := svgLen(v, st.fontSize, st.fontSize); sz > 0 {
				st.fontSize = sz
			}
		case "font-family":
			st.family = v
		case "font-weight":
			if w, ok := parseWeight(lv, st.weight); ok {
				st.weight = w
			}
		case "font-style":
			st.italic = lv == "italic" || lv == "oblique"
		case "text-anchor":
			st.anchor = lv
		case "marker":
			st.markerStart, st.markerMid, st.markerEnd = urlRef(v), urlRef(v), urlRef(v)
		case "marker-start":
			st.markerStart = urlRef(v)
		case "marker-mid":
			st.markerMid = urlRef(v)
		case "marker-end":
			st.markerEnd = urlRef(v)
		case "mask":
			st.mask = urlRef(v)
		case "filter":
			st.filter = urlRef(v)
		case "clip-path":
			st.clipPath = urlRef(v)
		case "visibility":
			st.visible = lv != "hidden" && lv != "collapse"
		case "display":
			if lv == "none" {
				st.visible = false
				st.opacity = 0
			}
		}
	}
}

func parseOpacity(v string) float64 {
	v = strings.TrimSpace(v)
	f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
	if err != nil {
		return 1
	}
	if strings.HasSuffix(v, "%") {
		f /= 100
	}
	return clamp(f, 0, 1)
}

// ---------------------------------------------------------------------
// Transforms and paths

type mat [6]float64 // a b c d e f

func (m mat) mul(n mat) mat {
	return mat{
		m[0]*n[0] + m[2]*n[1], m[1]*n[0] + m[3]*n[1],
		m[0]*n[2] + m[2]*n[3], m[1]*n[2] + m[3]*n[3],
		m[0]*n[4] + m[2]*n[5] + m[4], m[1]*n[4] + m[3]*n[5] + m[5],
	}
}

func (m mat) ops() string {
	return fmt.Sprintf("%s %s %s %s %s %s cm ", pdf.Num(m[0]), pdf.Num(m[1]), pdf.Num(m[2]), pdf.Num(m[3]), pdf.Num(m[4]), pdf.Num(m[5]))
}

func svgNumbers(s string) []float64 {
	var out []float64
	i := 0
	for i < len(s) {
		c := s[i]
		if c == ',' || c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		j := i
		if s[j] == '-' || s[j] == '+' {
			j++
		}
		dot := false
		for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.' && !dot) {
			if s[j] == '.' {
				dot = true
			}
			j++
		}
		if j < len(s) && (s[j] == 'e' || s[j] == 'E') {
			k := j + 1
			if k < len(s) && (s[k] == '-' || s[k] == '+') {
				k++
			}
			if k < len(s) && s[k] >= '0' && s[k] <= '9' {
				j = k
				for j < len(s) && s[j] >= '0' && s[j] <= '9' {
					j++
				}
			}
		}
		if j == i {
			i++
			continue
		}
		f, err := strconv.ParseFloat(s[i:j], 64)
		if err == nil {
			out = append(out, f)
		}
		i = j
	}
	return out
}

// parseSVGTransform parses the transform attribute.
func parseSVGTransform(v string) mat {
	m := mat{1, 0, 0, 1, 0, 0}
	for _, part := range strings.Split(v, ")") {
		open := strings.IndexByte(part, '(')
		if open < 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(strings.Trim(part[:open], " ,")))
		a := svgNumbers(part[open+1:])
		get := func(i int, d float64) float64 {
			if i < len(a) {
				return a[i]
			}
			return d
		}
		var t mat
		switch name {
		case "matrix":
			if len(a) != 6 {
				continue
			}
			t = mat{a[0], a[1], a[2], a[3], a[4], a[5]}
		case "translate":
			t = mat{1, 0, 0, 1, get(0, 0), get(1, 0)}
		case "scale":
			sx := get(0, 1)
			t = mat{sx, 0, 0, get(1, sx), 0, 0}
		case "rotate":
			r := get(0, 0) * math.Pi / 180
			cx, cy := get(1, 0), get(2, 0)
			t = mat{1, 0, 0, 1, cx, cy}.mul(mat{math.Cos(r), math.Sin(r), -math.Sin(r), math.Cos(r), 0, 0}).mul(mat{1, 0, 0, 1, -cx, -cy})
		case "skewx":
			t = mat{1, 0, math.Tan(get(0, 0) * math.Pi / 180), 1, 0, 0}
		case "skewy":
			t = mat{1, math.Tan(get(0, 0) * math.Pi / 180), 0, 1, 0, 0}
		default:
			continue
		}
		m = m.mul(t)
	}
	return m
}

// pathBuilder emits PDF path operators and tracks the bounding box and the
// vertices with their tangent directions (for markers).
type pathBuilder struct {
	sb                     strings.Builder
	minX, minY, maxX, maxY float64
	any                    bool
	verts                  []vertex
	start                  int // index of the current subpath's first vertex
	curX, curY             float64
	tf                     *mat // optional transform applied to the emitted coordinates
}

// vertex is a path vertex with the directions (radians) of the segments
// arriving and leaving.
type vertex struct {
	x, y          float64
	in, out       float64
	hasIn, hasOut bool
}

func (b *pathBuilder) setOut(dir float64) {
	if n := len(b.verts); n > 0 && !b.verts[n-1].hasOut {
		b.verts[n-1].out, b.verts[n-1].hasOut = dir, true
	}
}

// direction of the first distinct point after (x0, y0).
func dirTo(x0, y0 float64, pts ...[2]float64) (float64, bool) {
	for _, p := range pts {
		if math.Abs(p[0]-x0) > 1e-9 || math.Abs(p[1]-y0) > 1e-9 {
			return math.Atan2(p[1]-y0, p[0]-x0), true
		}
	}
	return 0, false
}

func (b *pathBuilder) xy(x, y float64) (float64, float64) {
	if b.tf != nil {
		return b.tf.apply(x, y)
	}
	return x, y
}

func (b *pathBuilder) pt(x, y float64) {
	x, y = b.xy(x, y)
	if !b.any {
		b.minX, b.maxX, b.minY, b.maxY, b.any = x, x, y, y, true
		return
	}
	b.minX, b.maxX = math.Min(b.minX, x), math.Max(b.maxX, x)
	b.minY, b.maxY = math.Min(b.minY, y), math.Max(b.maxY, y)
}

func (b *pathBuilder) move(x, y float64) {
	b.pt(x, y)
	mx, my := b.xy(x, y)
	fmt.Fprintf(&b.sb, "%s %s m ", pdf.Num(mx), pdf.Num(my))
	b.verts = append(b.verts, vertex{x: x, y: y})
	b.start = len(b.verts) - 1
	b.curX, b.curY = x, y
}

func (b *pathBuilder) line(x, y float64) {
	b.pt(x, y)
	lx, ly := b.xy(x, y)
	fmt.Fprintf(&b.sb, "%s %s l ", pdf.Num(lx), pdf.Num(ly))
	d, ok := dirTo(b.curX, b.curY, [2]float64{x, y})
	if ok {
		b.setOut(d)
	}
	b.verts = append(b.verts, vertex{x: x, y: y, in: d, hasIn: ok})
	b.curX, b.curY = x, y
}

func (b *pathBuilder) curveRaw(x1, y1, x2, y2, x, y float64) {
	b.pt(x1, y1)
	b.pt(x2, y2)
	b.pt(x, y)
	x1, y1 = b.xy(x1, y1)
	x2, y2 = b.xy(x2, y2)
	ex, ey := b.xy(x, y)
	fmt.Fprintf(&b.sb, "%s %s %s %s %s %s c ", pdf.Num(x1), pdf.Num(y1), pdf.Num(x2), pdf.Num(y2), pdf.Num(ex), pdf.Num(ey))
}

func (b *pathBuilder) curve(x1, y1, x2, y2, x, y float64) {
	if d, ok := dirTo(b.curX, b.curY, [2]float64{x1, y1}, [2]float64{x2, y2}, [2]float64{x, y}); ok {
		b.setOut(d)
	}
	b.curveRaw(x1, y1, x2, y2, x, y)
	in, ok := dirTo(x, y, [2]float64{x2, y2}, [2]float64{x1, y1}, [2]float64{b.curX, b.curY})
	b.verts = append(b.verts, vertex{x: x, y: y, in: in + math.Pi, hasIn: ok})
	b.curX, b.curY = x, y
}

func (b *pathBuilder) close() {
	if b.start < len(b.verts) {
		s := b.verts[b.start]
		if d, ok := dirTo(b.curX, b.curY, [2]float64{s.x, s.y}); ok {
			b.setOut(d)
			b.verts = append(b.verts, vertex{x: s.x, y: s.y, in: d, hasIn: true})
			if !b.verts[b.start].hasIn {
				b.verts[b.start].in, b.verts[b.start].hasIn = d, true
			}
		} else if n := len(b.verts); n > 0 && !b.verts[b.start].hasIn {
			b.verts[b.start].in, b.verts[b.start].hasIn = b.verts[n-1].in, b.verts[n-1].hasIn
		}
		b.curX, b.curY = s.x, s.y
	}
	b.sb.WriteString("h ")
}

func (b *pathBuilder) ellipse(cx, cy, rx, ry float64) {
	const k = 0.5523
	b.move(cx+rx, cy)
	b.curve(cx+rx, cy+k*ry, cx+k*rx, cy+ry, cx, cy+ry)
	b.curve(cx-k*rx, cy+ry, cx-rx, cy+k*ry, cx-rx, cy)
	b.curve(cx-rx, cy-k*ry, cx-k*rx, cy-ry, cx, cy-ry)
	b.curve(cx+k*rx, cy-ry, cx+rx, cy-k*ry, cx+rx, cy)
	b.close()
}

// arc converts an SVG elliptical arc to cubic Beziers (SVG spec F.6).
func (b *pathBuilder) arc(x1, y1, rx, ry, phi float64, large, sweep bool, x2, y2 float64) {
	if rx == 0 || ry == 0 {
		b.line(x2, y2)
		return
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	sinP, cosP := math.Sincos(phi * math.Pi / 180)
	dx, dy := (x1-x2)/2, (y1-y2)/2
	x1p := cosP*dx + sinP*dy
	y1p := -sinP*dx + cosP*dy
	if l := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry); l > 1 {
		rx, ry = rx*math.Sqrt(l), ry*math.Sqrt(l)
	}
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	co := 0.0
	if den != 0 && num > 0 {
		co = math.Sqrt(num / den)
	}
	if large == sweep {
		co = -co
	}
	cxp, cyp := co*rx*y1p/ry, -co*ry*x1p/rx
	cx := cosP*cxp - sinP*cyp + (x1+x2)/2
	cy := sinP*cxp + cosP*cyp + (y1+y2)/2
	angle := func(ux, uy, vx, vy float64) float64 {
		a := math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
		return a
	}
	t1 := angle(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	dt := angle((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	if !sweep && dt > 0 {
		dt -= 2 * math.Pi
	} else if sweep && dt < 0 {
		dt += 2 * math.Pi
	}
	segs := int(math.Ceil(math.Abs(dt) / (math.Pi / 2)))
	if segs == 0 {
		return
	}
	step := dt / float64(segs)
	k := 4.0 / 3 * math.Tan(step/4)
	point := func(t float64) (float64, float64) {
		s, c := math.Sincos(t)
		return cx + rx*c*cosP - ry*s*sinP, cy + rx*c*sinP + ry*s*cosP
	}
	deriv := func(t float64) (float64, float64) {
		s, c := math.Sincos(t)
		return -rx*s*cosP - ry*c*sinP, -rx*s*sinP + ry*c*cosP
	}
	sign := 1.0
	if dt < 0 {
		sign = -1
	}
	if dx0, dy0 := deriv(t1); dx0 != 0 || dy0 != 0 {
		b.setOut(math.Atan2(sign*dy0, sign*dx0))
	}
	t := t1
	for i := 0; i < segs; i++ {
		ax, ay := point(t)
		bx, by := point(t + step)
		dax, day := deriv(t)
		dbx, dby := deriv(t + step)
		b.curveRaw(ax+k*dax, ay+k*day, bx-k*dbx, by-k*dby, bx, by)
		t += step
	}
	dx1, dy1 := deriv(t1 + dt)
	b.verts = append(b.verts, vertex{x: x2, y: y2, in: math.Atan2(sign*dy1, sign*dx1), hasIn: dx1 != 0 || dy1 != 0})
	b.curX, b.curY = x2, y2
}

// pathData converts SVG path data ("M10 10 L20 20 ...").
func pathData(d string, b *pathBuilder) {
	var cmd byte
	var args []float64
	var cx, cy, sx, sy float64 // current and subpath start
	var lcx, lcy float64       // last control point
	var lastCmd byte
	flush := func(c byte, a []float64) {
		rel := c >= 'a'
		up := c &^ 0x20
		n := map[byte]int{'M': 2, 'L': 2, 'H': 1, 'V': 1, 'C': 6, 'S': 4, 'Q': 4, 'T': 2, 'A': 7, 'Z': 0}[up]
		if up == 'Z' {
			b.close()
			cx, cy = sx, sy
			lastCmd = 'Z'
			return
		}
		if n == 0 {
			return
		}
		first := true
		for len(a) >= n {
			p := a[:n]
			a = a[n:]
			ox, oy := 0.0, 0.0
			if rel {
				ox, oy = cx, cy
			}
			switch up {
			case 'M':
				if first {
					cx, cy = p[0]+ox, p[1]+oy
					sx, sy = cx, cy
					b.move(cx, cy)
				} else { // implicit lineto
					cx, cy = p[0]+ox, p[1]+oy
					b.line(cx, cy)
				}
			case 'L':
				cx, cy = p[0]+ox, p[1]+oy
				b.line(cx, cy)
			case 'H':
				if rel {
					cx += p[0]
				} else {
					cx = p[0]
				}
				b.line(cx, cy)
			case 'V':
				if rel {
					cy += p[0]
				} else {
					cy = p[0]
				}
				b.line(cx, cy)
			case 'C':
				b.curve(p[0]+ox, p[1]+oy, p[2]+ox, p[3]+oy, p[4]+ox, p[5]+oy)
				lcx, lcy = p[2]+ox, p[3]+oy
				cx, cy = p[4]+ox, p[5]+oy
			case 'S':
				x1, y1 := cx, cy
				if lastCmd == 'C' || lastCmd == 'S' {
					x1, y1 = 2*cx-lcx, 2*cy-lcy
				}
				b.curve(x1, y1, p[0]+ox, p[1]+oy, p[2]+ox, p[3]+oy)
				lcx, lcy = p[0]+ox, p[1]+oy
				cx, cy = p[2]+ox, p[3]+oy
			case 'Q', 'T':
				var qx, qy, x, y float64
				if up == 'Q' {
					qx, qy, x, y = p[0]+ox, p[1]+oy, p[2]+ox, p[3]+oy
				} else {
					qx, qy = cx, cy
					if lastCmd == 'Q' || lastCmd == 'T' {
						qx, qy = 2*cx-lcx, 2*cy-lcy
					}
					x, y = p[0]+ox, p[1]+oy
				}
				b.curve(cx+2.0/3*(qx-cx), cy+2.0/3*(qy-cy), x+2.0/3*(qx-x), y+2.0/3*(qy-y), x, y)
				lcx, lcy = qx, qy
				cx, cy = x, y
			case 'A':
				x, y := p[5]+ox, p[6]+oy
				b.arc(cx, cy, p[0], p[1], p[2], p[3] != 0, p[4] != 0, x, y)
				cx, cy = x, y
			}
			lastCmd = up
			first = false
		}
	}
	i := 0
	for i < len(d) {
		c := d[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' {
			if c == 'e' || c == 'E' { // exponent handled by number parsing
				i++
				continue
			}
			if cmd != 0 {
				flush(cmd, args)
			}
			cmd, args = c, nil
			i++
			continue
		}
		j := i
		for j < len(d) && !(d[j] >= 'A' && d[j] <= 'Z' && d[j] != 'E' || d[j] >= 'a' && d[j] <= 'z' && d[j] != 'e') {
			j++
		}
		// arc flags may be written without separators: "a1 1 0 00 10 10"
		if cmd|0x20 == 'a' {
			args = append(args, arcNumbers(d[i:j])...)
		} else {
			args = append(args, svgNumbers(d[i:j])...)
		}
		i = j
	}
	if cmd != 0 {
		flush(cmd, args)
	}
}

// arcNumbers parses arc arguments where the two flags may be glued
// together or to the next number.
func arcNumbers(s string) []float64 {
	var out []float64
	pos := 0 // index within the 7-number group
	i := 0
	for i < len(s) {
		c := s[i]
		if c == ',' || c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if (pos == 3 || pos == 4) && (c == '0' || c == '1') {
			out = append(out, float64(c-'0'))
			pos = (pos + 1) % 7
			i++
			continue
		}
		nums := svgNumbers(firstNumber(s[i:]))
		if len(nums) == 0 {
			i++
			continue
		}
		out = append(out, nums[0])
		i += len(firstNumber(s[i:]))
		pos = (pos + 1) % 7
	}
	return out
}

func firstNumber(s string) string {
	j := 0
	if j < len(s) && (s[j] == '-' || s[j] == '+') {
		j++
	}
	dot := false
	for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.' && !dot) {
		if s[j] == '.' {
			dot = true
		}
		j++
	}
	if j < len(s) && (s[j] == 'e' || s[j] == 'E') {
		k := j + 1
		if k < len(s) && (s[k] == '-' || s[k] == '+') {
			k++
		}
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		j = k
	}
	if j == 0 {
		return s[:1]
	}
	return s[:j]
}

// shapePath builds the path of a basic shape element.
func shapePath(n *htmlparse.Node, st *svgState) (*pathBuilder, bool) { return shapePathT(n, st, nil) }

// shapePathT builds a shape's path with an optional coordinate transform.
func shapePathT(n *htmlparse.Node, st *svgState, tf *mat) (*pathBuilder, bool) {
	b := &pathBuilder{tf: tf}
	diag := math.Hypot(st.vw, st.vh) / math.Sqrt2
	lx := func(a string) float64 { return svgLen(n.Attr(a), st.vw, st.fontSize) }
	ly := func(a string) float64 { return svgLen(n.Attr(a), st.vh, st.fontSize) }
	switch n.Tag {
	case "rect":
		x, y, w, h := lx("x"), ly("y"), lx("width"), ly("height")
		if w <= 0 || h <= 0 {
			return nil, false
		}
		rx, ry := lx("rx"), ly("ry")
		if !n.HasAttr("rx") {
			rx = ry
		}
		if !n.HasAttr("ry") {
			ry = rx
		}
		rx, ry = math.Min(rx, w/2), math.Min(ry, h/2)
		if rx <= 0 || ry <= 0 {
			b.move(x, y)
			b.line(x+w, y)
			b.line(x+w, y+h)
			b.line(x, y+h)
			b.close()
			return b, true
		}
		const k = 0.4477
		b.move(x+rx, y)
		b.line(x+w-rx, y)
		b.curve(x+w-rx*k, y, x+w, y+ry*k, x+w, y+ry)
		b.line(x+w, y+h-ry)
		b.curve(x+w, y+h-ry*k, x+w-rx*k, y+h, x+w-rx, y+h)
		b.line(x+rx, y+h)
		b.curve(x+rx*k, y+h, x, y+h-ry*k, x, y+h-ry)
		b.line(x, y+ry)
		b.curve(x, y+ry*k, x+rx*k, y, x+rx, y)
		b.close()
	case "circle":
		r := svgLen(n.Attr("r"), diag, st.fontSize)
		if r <= 0 {
			return nil, false
		}
		b.ellipse(lx("cx"), ly("cy"), r, r)
	case "ellipse":
		rx, ry := lx("rx"), ly("ry")
		if rx <= 0 || ry <= 0 {
			return nil, false
		}
		b.ellipse(lx("cx"), ly("cy"), rx, ry)
	case "line":
		b.move(lx("x1"), ly("y1"))
		b.line(lx("x2"), ly("y2"))
	case "polyline", "polygon":
		pts := svgNumbers(n.Attr("points"))
		if len(pts) < 4 {
			return nil, false
		}
		b.move(pts[0], pts[1])
		for i := 2; i+1 < len(pts); i += 2 {
			b.line(pts[i], pts[i+1])
		}
		if n.Tag == "polygon" {
			b.close()
		}
	case "path":
		pathData(n.Attr("d"), b)
	default:
		return nil, false
	}
	return b, b.any
}

// ---------------------------------------------------------------------
// Rendering

type svgRenderer struct {
	e     *engine
	doc   *svgDoc
	page  *pdf.Page
	out   strings.Builder
	alpha float64
	depth int

	fx       []func(pdf.Color) pdf.Color // color transforms from filters
	override *pdf.Color                  // shadow pass: paint everything in this color
}

// col applies the active filter color transforms (and the shadow override).
func (r *svgRenderer) col(c pdf.Color) pdf.Color {
	if r.override != nil {
		o := *r.override
		o.A = alphaField(o.Alpha() * c.Alpha())
		return o
	}
	for _, f := range r.fx {
		c = f(c)
	}
	return c
}

// drawSVG paints doc into the box (x, y, w, h) of the page.
func (e *engine) drawSVG(p *pdf.Page, l pdf.Layer, doc *svgDoc, clip *pdf.Rect, x, y, w, h, alpha float64) {
	if e.dry || w <= 0 || h <= 0 {
		return
	}
	r := &svgRenderer{e: e, doc: doc, page: p, alpha: alpha}
	H := e.doc.Height
	// clip to the viewport, flip to y-down user space, then map the viewBox
	cr := pdf.Rect{X: x, Y: y, W: w, H: h}
	if clip != nil {
		cr = *clip
	}
	r.out.WriteString(fmt.Sprintf("q %s %s %s %s re W n ", pdf.Num(cr.X), pdf.Num(H-cr.Y-cr.H), pdf.Num(cr.W), pdf.Num(cr.H)))
	r.out.WriteString(mat{1, 0, 0, -1, 0, H}.ops())
	st := defaultSVGState(doc.width, doc.height)
	r.viewport(doc.root, st, x, y, w, h)
	r.out.WriteString("Q\n")
	p.Raw(l, r.out.String())
}

// viewBoxMatrix maps a viewBox into the box (x, y, w, h) following
// preserveAspectRatio.
func viewBoxMatrix(vb [4]float64, par string, x, y, w, h float64) mat {
	sx, sy := w/vb[2], h/vb[3]
	f := strings.Fields(strings.ToLower(par))
	align, slice := "xmidymid", false
	if len(f) > 0 {
		align = f[0]
	}
	if len(f) > 1 && f[1] == "slice" {
		slice = true
	}
	tx, ty := x, y
	if align != "none" {
		s := math.Min(sx, sy)
		if slice {
			s = math.Max(sx, sy)
		}
		sx, sy = s, s
		freeX, freeY := w-vb[2]*s, h-vb[3]*s
		switch {
		case strings.Contains(align, "xmid"):
			tx += freeX / 2
		case strings.Contains(align, "xmax"):
			tx += freeX
		}
		switch {
		case strings.Contains(align, "ymid"):
			ty += freeY / 2
		case strings.Contains(align, "ymax"):
			ty += freeY
		}
	}
	return mat{sx, 0, 0, sy, tx - vb[0]*sx, ty - vb[1]*sy}
}

func (m mat) apply(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

func (m mat) inverse() (mat, bool) {
	det := m[0]*m[3] - m[1]*m[2]
	if math.Abs(det) < 1e-12 {
		return mat{}, false
	}
	a, b, c, d := m[3]/det, -m[1]/det, -m[2]/det, m[0]/det
	return mat{a, b, c, d, -(a*m[4] + c*m[5]), -(b*m[4] + d*m[5])}, true
}

// viewport renders an <svg> (or <symbol> via <use>) into the box
// (x, y, w, h) of the current user space.
func (r *svgRenderer) viewport(n *htmlparse.Node, st svgState, x, y, w, h float64) {
	vb, hasVB := parseViewBox(n.Attr("viewbox"))
	r.out.WriteString("q ")
	if hasVB {
		r.out.WriteString(viewBoxMatrix(vb, n.Attr("preserveaspectratio"), x, y, w, h).ops())
		st.vw, st.vh = vb[2], vb[3]
	} else {
		r.out.WriteString(mat{1, 0, 0, 1, x, y}.ops())
		st.vw, st.vh = w, h
		if n == r.doc.root {
			// the outer svg without viewBox: scale intrinsic size to the box
			sx, sy := w/r.doc.width, h/r.doc.height
			r.out.WriteString(mat{sx, 0, 0, sy, 0, 0}.ops())
			st.vw, st.vh = r.doc.width, r.doc.height
		}
	}
	r.e.applySVGStyle(r.doc, n, &st)
	r.children(n, st)
	r.out.WriteString("Q\n")
}

func (r *svgRenderer) children(n *htmlparse.Node, st svgState) {
	for _, c := range n.Children {
		if c.Type == htmlparse.ElementNode {
			r.element(c, st)
		}
	}
}

func (r *svgRenderer) element(n *htmlparse.Node, parent svgState) {
	if r.depth > 64 {
		return
	}
	r.depth++
	defer func() { r.depth-- }()
	switch n.Tag {
	case "defs", "style", "title", "desc", "metadata", "lineargradient", "radialgradient", "clippath", "mask",
		"pattern", "symbol", "marker", "filter", "script", "foreignobject":
		return
	}
	st := parent
	st.mask, st.filter, st.clipPath = "", "", ""
	r.e.applySVGStyle(r.doc, n, &st)
	if st.opacity <= 0 {
		return
	}
	r.out.WriteString("q ")
	defer r.out.WriteString("Q\n")
	if t := n.Attr("transform"); t != "" {
		r.out.WriteString(parseSVGTransform(t).ops())
	}
	if st.clipPath != "" {
		r.clip(st.clipPath, st)
	}
	if st.mask != "" && r.override == nil {
		r.applyMask(n, st)
	}
	if st.filter != "" && r.filtered(n, st, parent) {
		return
	}
	r.content(n, st, parent)
}

// content draws an element itself (after transform, clip, mask, filter).
func (r *svgRenderer) content(n *htmlparse.Node, st, parent svgState) {
	switch n.Tag {
	case "g", "a":
		r.children(n, st)
	case "switch":
		for _, c := range n.Children {
			if c.Type == htmlparse.ElementNode {
				r.element(c, st)
				return
			}
		}
	case "svg":
		x, y := svgLen(n.Attr("x"), st.vw, st.fontSize), svgLen(n.Attr("y"), st.vh, st.fontSize)
		w, h := st.vw, st.vh
		if v := n.Attr("width"); v != "" {
			w = svgLen(v, st.vw, st.fontSize)
		}
		if v := n.Attr("height"); v != "" {
			h = svgLen(v, st.vh, st.fontSize)
		}
		r.viewport(n, parent, x, y, w, h)
	case "use":
		ref := strings.TrimPrefix(firstNonEmpty(n.Attr("href"), n.Attr("xlink:href")), "#")
		target := r.doc.ids[ref]
		if target == nil || target == n {
			return
		}
		x, y := svgLen(n.Attr("x"), st.vw, st.fontSize), svgLen(n.Attr("y"), st.vh, st.fontSize)
		r.out.WriteString(mat{1, 0, 0, 1, x, y}.ops())
		if target.Tag == "symbol" || target.Tag == "svg" {
			w, h := st.vw, st.vh
			if v := firstNonEmpty(n.Attr("width"), target.Attr("width")); v != "" {
				w = svgLen(v, st.vw, st.fontSize)
			}
			if v := firstNonEmpty(n.Attr("height"), target.Attr("height")); v != "" {
				h = svgLen(v, st.vh, st.fontSize)
			}
			r.viewport(target, st, 0, 0, w, h)
			return
		}
		r.element(target, st)
	case "text":
		r.text(n, st)
	case "image":
		r.image(n, st)
	default:
		if b, ok := shapePath(n, &st); ok {
			r.paint(b, st, n.Tag == "line")
			switch n.Tag {
			case "path", "line", "polyline", "polygon":
				r.markers(b, st)
			}
		}
	}
}

func (r *svgRenderer) clip(id string, st svgState) {
	cp := r.doc.ids[id]
	if cp == nil {
		return
	}
	var paths strings.Builder
	for _, c := range cp.Children {
		if c.Type != htmlparse.ElementNode {
			continue
		}
		cs := st
		var tf *mat
		if t := c.Attr("transform"); t != "" {
			m := parseSVGTransform(t)
			tf = &m
		}
		if b, ok := shapePathT(c, &cs, tf); ok {
			paths.WriteString(b.sb.String())
		}
	}
	if paths.Len() > 0 {
		r.out.WriteString(paths.String() + "W n ")
	}
}

func (r *svgRenderer) gs(a float64) string { return r.page.GS(a * r.alpha) }

// paint fills and strokes a path.
func (r *svgRenderer) paint(b *pathBuilder, st svgState, openShape bool) {
	path := b.sb.String()
	if !st.visible {
		return
	}
	if !st.fill.none && !openShape {
		op, clipOp := "f", "W n"
		if st.evenOdd {
			op, clipOp = "f*", "W* n"
		}
		c := r.col(st.fill.color)
		alpha := st.opacity * st.fillOp * c.Alpha()
		done := false
		if st.fill.ref != "" && r.override == nil {
			switch t := r.doc.ids[st.fill.ref]; {
			case t == nil:
			case t.Tag == "pattern":
				done = r.patternFill(t, b, st, path, clipOp, st.opacity*st.fillOp)
			default:
				if sh, m, ok := r.gradient(st.fill.ref, b); ok {
					r.out.WriteString("q " + r.gs(st.opacity*st.fillOp) + path + clipOp + " " + m.ops() + sh.Name() + " sh Q\n")
					done = true
				}
			}
		}
		if !done && (st.fill.ref == "" || r.override != nil || st.fill.color != (pdf.Color{})) {
			r.out.WriteString("q " + r.gs(alpha) + rgbOp(c, "rg") + " " + path + op + " Q\n")
		}
	}
	if !st.stroke.none && st.strokeW > 0 {
		c := st.stroke.color
		if st.stroke.ref != "" {
			if g := r.doc.ids[st.stroke.ref]; g != nil {
				if stops := r.stops(g); len(stops) > 0 {
					c = stops[len(stops)/2].Color
				}
			}
		}
		c = r.col(c)
		var sb strings.Builder
		sb.WriteString("q " + r.gs(st.opacity*st.strokeOp*c.Alpha()) + rgbOp(c, "RG") + " ")
		fmt.Fprintf(&sb, "%s w %d J %d j %s M ", pdf.Num(st.strokeW), st.cap, st.join, pdf.Num(st.miter))
		if len(st.dash) > 0 {
			sb.WriteString("[")
			for i, d := range st.dash {
				if i > 0 {
					sb.WriteByte(' ')
				}
				sb.WriteString(pdf.Num(d))
			}
			sb.WriteString("] " + pdf.Num(st.dashOff) + " d ")
		}
		sb.WriteString(path + "S Q\n")
		r.out.WriteString(sb.String())
	}
}

// inherited reads an attribute of a paint server, following href chains.
func (r *svgRenderer) inherited(n *htmlparse.Node, name, def string) string {
	for i := 0; i < 8 && n != nil; i++ {
		if v := n.Attr(name); v != "" {
			return v
		}
		n = r.doc.ids[strings.TrimPrefix(firstNonEmpty(n.Attr("href"), n.Attr("xlink:href")), "#")]
	}
	return def
}

// unitValue resolves x/y/width/height of patterns and masks: fractions of
// the bounding box (objectBoundingBox) or user-space lengths.
func unitValue(v string, bboxUnits bool, origin, size, viewport, em float64) float64 {
	v = strings.TrimSpace(v)
	if bboxUnits {
		f := 0.0
		if strings.HasSuffix(v, "%") {
			f, _ = strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
			f /= 100
		} else {
			f, _ = strconv.ParseFloat(v, 64)
		}
		return origin + f*size
	}
	return svgLen(v, viewport, em)
}

// patternFill fills a path with a tiled <pattern>.
func (r *svgRenderer) patternFill(p *htmlparse.Node, b *pathBuilder, st svgState, path, clipOp string, alpha float64) bool {
	bx, by, bw, bh := b.minX, b.minY, b.maxX-b.minX, b.maxY-b.minY
	bboxUnits := !strings.EqualFold(r.inherited(p, "patternunits", ""), "userSpaceOnUse")
	x := unitValue(r.inherited(p, "x", "0"), bboxUnits, bx, bw, st.vw, st.fontSize)
	y := unitValue(r.inherited(p, "y", "0"), bboxUnits, by, bh, st.vh, st.fontSize)
	w := unitValue(r.inherited(p, "width", "0"), bboxUnits, 0, bw, st.vw, st.fontSize)
	h := unitValue(r.inherited(p, "height", "0"), bboxUnits, 0, bh, st.vh, st.fontSize)
	if w <= 0.001 || h <= 0.001 {
		return false
	}
	content := p
	for i := 0; i < 8 && content != nil; i++ {
		has := false
		for _, c := range content.Children {
			if c.Type == htmlparse.ElementNode {
				has = true
				break
			}
		}
		if has {
			break
		}
		content = r.doc.ids[strings.TrimPrefix(firstNonEmpty(content.Attr("href"), content.Attr("xlink:href")), "#")]
	}
	if content == nil {
		return false
	}
	ptm := parseSVGTransform(r.inherited(p, "patterntransform", ""))
	inv, ok := ptm.inverse()
	if !ok {
		return false
	}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, c := range [][2]float64{{b.minX, b.minY}, {b.maxX, b.minY}, {b.minX, b.maxY}, {b.maxX, b.maxY}} {
		px, py := inv.apply(c[0], c[1])
		minX, maxX = math.Min(minX, px), math.Max(maxX, px)
		minY, maxY = math.Min(minY, py), math.Max(maxY, py)
	}
	i0, i1 := int(math.Floor((minX-x)/w)), int(math.Ceil((maxX-x)/w))
	j0, j1 := int(math.Floor((minY-y)/h)), int(math.Ceil((maxY-y)/h))
	if (i1-i0)*(j1-j0) > 2500 || i1 <= i0 || j1 <= j0 {
		return false
	}
	vb, hasVB := parseViewBox(r.inherited(p, "viewbox", ""))
	contentBBox := strings.EqualFold(r.inherited(p, "patterncontentunits", ""), "objectBoundingBox")
	ps := defaultSVGState(w, h)
	r.e.applySVGStyle(r.doc, content, &ps)

	saved := r.alpha
	r.alpha *= alpha
	r.out.WriteString("q " + path + clipOp + " " + ptm.ops())
	for j := j0; j < j1; j++ {
		for i := i0; i < i1; i++ {
			r.out.WriteString("q " + mat{1, 0, 0, 1, x + float64(i)*w, y + float64(j)*h}.ops())
			if strings.ToLower(p.Attr("overflow")) != "visible" {
				r.out.WriteString(fmt.Sprintf("0 0 %s %s re W n ", pdf.Num(w), pdf.Num(h)))
			}
			switch {
			case hasVB:
				r.out.WriteString(viewBoxMatrix(vb, r.inherited(p, "preserveaspectratio", ""), 0, 0, w, h).ops())
			case contentBBox:
				r.out.WriteString(mat{bw, 0, 0, bh, 0, 0}.ops())
			}
			r.children(content, ps)
			r.out.WriteString("Q\n")
		}
	}
	r.out.WriteString("Q\n")
	r.alpha = saved
	return true
}

// markers draws marker-start / -mid / -end on the vertices of a path.
func (r *svgRenderer) markers(b *pathBuilder, st svgState) {
	if (st.markerStart == "" && st.markerMid == "" && st.markerEnd == "") || len(b.verts) == 0 || !st.visible {
		return
	}
	n := len(b.verts)
	for i, v := range b.verts {
		id := st.markerMid
		switch {
		case i == 0:
			id = st.markerStart
		case i == n-1:
			id = st.markerEnd
		}
		m := r.doc.ids[id]
		if id == "" || m == nil || m.Tag != "marker" {
			continue
		}
		angle := 0.0
		orient := strings.ToLower(strings.TrimSpace(m.Attr("orient")))
		switch orient {
		case "auto", "auto-start-reverse":
			switch {
			case v.hasIn && v.hasOut:
				angle = math.Atan2(math.Sin(v.in)+math.Sin(v.out), math.Cos(v.in)+math.Cos(v.out))
			case v.hasOut:
				angle = v.out
			case v.hasIn:
				angle = v.in
			}
			if orient == "auto-start-reverse" && i == 0 {
				angle += math.Pi
			}
		default:
			if a, ok := parseAngle(orient); ok {
				angle = a * math.Pi / 180
			} else if f, err := strconv.ParseFloat(orient, 64); err == nil {
				angle = f * math.Pi / 180
			}
		}
		r.marker(m, v.x, v.y, angle, st)
	}
}

func (r *svgRenderer) marker(m *htmlparse.Node, x, y, angle float64, st svgState) {
	mw := svgLen(firstNonEmpty(m.Attr("markerwidth"), "3"), st.vw, st.fontSize)
	mh := svgLen(firstNonEmpty(m.Attr("markerheight"), "3"), st.vh, st.fontSize)
	if mw <= 0 || mh <= 0 {
		return
	}
	scale := st.strokeW
	if strings.EqualFold(m.Attr("markerunits"), "userSpaceOnUse") {
		scale = 1
	}
	refX := svgLen(m.Attr("refx"), st.vw, st.fontSize)
	refY := svgLen(m.Attr("refy"), st.vh, st.fontSize)
	sin, cos := math.Sincos(angle)
	r.out.WriteString("q " + mat{cos, sin, -sin, cos, x, y}.ops() + mat{scale, 0, 0, scale, 0, 0}.ops())
	v := mat{1, 0, 0, 1, 0, 0}
	ms := defaultSVGState(mw, mh)
	if vb, ok := parseViewBox(m.Attr("viewbox")); ok {
		v = viewBoxMatrix(vb, m.Attr("preserveaspectratio"), 0, 0, mw, mh)
		ms.vw, ms.vh = vb[2], vb[3]
	}
	rx, ry := v.apply(refX, refY)
	r.out.WriteString(mat{1, 0, 0, 1, -rx, -ry}.ops())
	if strings.ToLower(m.Attr("overflow")) != "visible" {
		r.out.WriteString(fmt.Sprintf("0 0 %s %s re W n ", pdf.Num(mw), pdf.Num(mh)))
	}
	r.out.WriteString(v.ops())
	r.e.applySVGStyle(r.doc, m, &ms)
	r.children(m, ms)
	r.out.WriteString("Q\n")
}

// bbox returns the bounding box of an element in its own user space.
func (r *svgRenderer) bbox(n *htmlparse.Node, st svgState, depth int) (float64, float64, float64, float64, bool) {
	if depth > 16 {
		return 0, 0, 0, 0, false
	}
	if b, ok := shapePath(n, &st); ok {
		return b.minX, b.minY, b.maxX, b.maxY, true
	}
	switch n.Tag {
	case "text":
		x, y := svgLen(n.Attr("x"), st.vw, st.fontSize), svgLen(n.Attr("y"), st.vh, st.fontSize)
		w := float64(len([]rune(n.TextContent()))) * st.fontSize * 0.55
		return x, y - st.fontSize, x + w, y + st.fontSize*0.25, true
	case "image", "svg":
		x, y := svgLen(n.Attr("x"), st.vw, st.fontSize), svgLen(n.Attr("y"), st.vh, st.fontSize)
		w, h := svgLen(n.Attr("width"), st.vw, st.fontSize), svgLen(n.Attr("height"), st.vh, st.fontSize)
		if n.Tag == "svg" && n.Attr("width") == "" {
			w, h = st.vw, st.vh
		}
		return x, y, x + w, y + h, w > 0 && h > 0
	case "use":
		if t := r.doc.ids[strings.TrimPrefix(firstNonEmpty(n.Attr("href"), n.Attr("xlink:href")), "#")]; t != nil && t != n {
			x0, y0, x1, y1, ok := r.bbox(t, st, depth+1)
			dx, dy := svgLen(n.Attr("x"), st.vw, st.fontSize), svgLen(n.Attr("y"), st.vh, st.fontSize)
			return x0 + dx, y0 + dy, x1 + dx, y1 + dy, ok
		}
	}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	any := false
	for _, c := range n.Children {
		if c.Type != htmlparse.ElementNode {
			continue
		}
		x0, y0, x1, y1, ok := r.bbox(c, st, depth+1)
		if !ok {
			continue
		}
		t := parseSVGTransform(c.Attr("transform"))
		for _, p := range [][2]float64{{x0, y0}, {x1, y0}, {x0, y1}, {x1, y1}} {
			px, py := t.apply(p[0], p[1])
			minX, maxX = math.Min(minX, px), math.Max(maxX, px)
			minY, maxY = math.Min(minY, py), math.Max(maxY, py)
		}
		any = true
	}
	return minX, minY, maxX, maxY, any
}

// applyMask installs a <mask> as a PDF soft mask for the element.
func (r *svgRenderer) applyMask(n *htmlparse.Node, st svgState) {
	m := r.doc.ids[st.mask]
	if m == nil || m.Tag != "mask" {
		return
	}
	bx0, by0, bx1, by1, ok := r.bbox(n, st, 0)
	if !ok {
		bx0, by0, bx1, by1 = 0, 0, st.vw, st.vh
	}
	bw, bh := bx1-bx0, by1-by0
	bboxUnits := !strings.EqualFold(m.Attr("maskunits"), "userSpaceOnUse")
	def := func(name, d string) string { return firstNonEmpty(m.Attr(name), d) }
	var x, y, w, h float64
	if bboxUnits {
		x = unitValue(def("x", "-10%"), true, bx0, bw, 0, 0)
		y = unitValue(def("y", "-10%"), true, by0, bh, 0, 0)
		w = unitValue(def("width", "120%"), true, 0, bw, 0, 0)
		h = unitValue(def("height", "120%"), true, 0, bh, 0, 0)
	} else {
		x = svgLen(def("x", "-10%"), st.vw, st.fontSize)
		y = svgLen(def("y", "-10%"), st.vh, st.fontSize)
		w = svgLen(def("width", "120%"), st.vw, st.fontSize)
		h = svgLen(def("height", "120%"), st.vh, st.fontSize)
	}
	sub := &svgRenderer{e: r.e, doc: r.doc, page: r.page, alpha: 1, depth: r.depth}
	sub.out.WriteString(fmt.Sprintf("%s %s %s %s re W n ", pdf.Num(x), pdf.Num(y), pdf.Num(w), pdf.Num(h)))
	if strings.EqualFold(m.Attr("maskcontentunits"), "objectBoundingBox") {
		sub.out.WriteString(mat{bw, 0, 0, bh, bx0, by0}.ops())
	}
	ms := defaultSVGState(st.vw, st.vh)
	r.e.applySVGStyle(r.doc, m, &ms)
	sub.children(m, ms)
	form := r.e.doc.AddForm(sub.out.String(), [4]float64{-1e5, -1e5, 1e5, 1e5}, true)
	alphaMask := strings.EqualFold(m.Attr("mask-type"), "alpha") ||
		strings.Contains(strings.ToLower(strings.ReplaceAll(m.Attr("style"), " ", "")), "mask-type:alpha")
	r.out.WriteString(r.e.doc.SoftMask(form, alphaMask))
}

// ---------------------------------------------------------------------
// Filters: PDF has no filter primitives, so the common effects are
// approximated with vectors: drop shadows (blurred by stacked copies),
// Gaussian blur, offsets, and color transforms (feColorMatrix,
// feComponentTransfer). Other primitives are ignored.

type filterPlan struct {
	blur, dx, dy float64
	shadow       *shadowPlan
	fx           []func(pdf.Color) pdf.Color
}

type shadowPlan struct {
	dx, dy, blur float64
	color        pdf.Color
}

func numAttr(n *htmlparse.Node, name string, def float64) float64 {
	if v := svgNumbers(n.Attr(name)); len(v) > 0 {
		return v[0]
	}
	return def
}

func (r *svgRenderer) floodOf(n *htmlparse.Node) pdf.Color {
	c := pdf.Color{}
	op := 1.0
	decls := []css.Decl{}
	for _, p := range []string{"flood-color", "flood-opacity"} {
		if v, ok := n.Attrs[p]; ok {
			decls = append(decls, css.Decl{Prop: p, Value: v})
		}
	}
	decls = append(decls, css.ParseDecls(n.Attr("style"))...)
	for _, d := range decls {
		switch d.Prop {
		case "flood-color":
			if cc, ok := css.ParseColor(d.Value); ok {
				c = pdf.Color{R: cc.R, G: cc.G, B: cc.B, A: alphaField(cc.A)}
			}
		case "flood-opacity":
			op = parseOpacity(d.Value)
		}
	}
	c.A = alphaField(c.Alpha() * op)
	return c
}

func (r *svgRenderer) parseFilter(f *htmlparse.Node) filterPlan {
	var plan filterPlan
	alphaChain := false // the current result derives from SourceAlpha
	shadow := shadowPlan{color: pdf.Color{}}
	shadowAlpha := 1.0
	for _, p := range f.Children {
		if p.Type != htmlparse.ElementNode {
			continue
		}
		in := p.Attr("in")
		if in == "SourceAlpha" {
			alphaChain = true
		}
		onShadow := alphaChain && in != "SourceGraphic"
		switch p.Tag {
		case "fedropshadow":
			plan.shadow = &shadowPlan{dx: numAttr(p, "dx", 2), dy: numAttr(p, "dy", 2), blur: numAttr(p, "stddeviation", 2), color: r.floodOf(p)}
		case "fegaussianblur":
			if onShadow {
				shadow.blur = numAttr(p, "stddeviation", 0)
			} else {
				plan.blur = numAttr(p, "stddeviation", 0)
			}
		case "feoffset":
			if onShadow {
				shadow.dx, shadow.dy = numAttr(p, "dx", 0), numAttr(p, "dy", 0)
			} else {
				plan.dx, plan.dy = numAttr(p, "dx", 0), numAttr(p, "dy", 0)
			}
		case "feflood":
			shadow.color = r.floodOf(p)
		case "fecomponenttransfer":
			if onShadow {
				for _, c := range p.Children {
					if c.Type == htmlparse.ElementNode && c.Tag == "fefunca" {
						shadowAlpha *= numAttr(c, "slope", 1)
					}
				}
			} else if fx := componentTransfer(p); fx != nil {
				plan.fx = append(plan.fx, fx)
			}
		case "fecolormatrix":
			if fx := colorMatrix(p); fx != nil && !onShadow {
				plan.fx = append(plan.fx, fx)
			}
		case "femerge":
			if alphaChain {
				shadow.color.A = alphaField(shadow.color.Alpha() * shadowAlpha)
				s := shadow
				plan.shadow = &s
			}
		}
	}
	return plan
}

func colorMatrix(p *htmlparse.Node) func(pdf.Color) pdf.Color {
	var m [20]float64
	switch strings.ToLower(p.Attr("type")) {
	case "saturate":
		s := numAttr(p, "values", 1)
		m = [20]float64{0.213 + 0.787*s, 0.715 - 0.715*s, 0.072 - 0.072*s, 0, 0,
			0.213 - 0.213*s, 0.715 + 0.285*s, 0.072 - 0.072*s, 0, 0,
			0.213 - 0.213*s, 0.715 - 0.715*s, 0.072 + 0.928*s, 0, 0,
			0, 0, 0, 1, 0}
	case "huerotate":
		a := numAttr(p, "values", 0) * math.Pi / 180
		s, c := math.Sincos(a)
		m = [20]float64{0.213 + c*0.787 - s*0.213, 0.715 - c*0.715 - s*0.715, 0.072 - c*0.072 + s*0.928, 0, 0,
			0.213 - c*0.213 + s*0.143, 0.715 + c*0.285 + s*0.140, 0.072 - c*0.072 - s*0.283, 0, 0,
			0.213 - c*0.213 - s*0.787, 0.715 - c*0.715 + s*0.715, 0.072 + c*0.928 + s*0.072, 0, 0,
			0, 0, 0, 1, 0}
	case "matrix", "":
		v := svgNumbers(p.Attr("values"))
		if len(v) != 20 {
			return nil
		}
		copy(m[:], v)
	default:
		return nil
	}
	return func(c pdf.Color) pdf.Color {
		a := c.Alpha()
		in := [4]float64{c.R, c.G, c.B, a}
		var o [4]float64
		for i := 0; i < 4; i++ {
			o[i] = m[i*5]*in[0] + m[i*5+1]*in[1] + m[i*5+2]*in[2] + m[i*5+3]*in[3] + m[i*5+4]
			o[i] = clamp(o[i], 0, 1)
		}
		return pdf.Color{R: o[0], G: o[1], B: o[2], A: alphaField(math.Max(o[3], 0.001))}
	}
}

func componentTransfer(p *htmlparse.Node) func(pdf.Color) pdf.Color {
	funcs := map[string]func(float64) float64{}
	for _, c := range p.Children {
		if c.Type != htmlparse.ElementNode || !strings.HasPrefix(c.Tag, "fefunc") {
			continue
		}
		ch := c.Tag[len("fefunc"):]
		node := c
		switch strings.ToLower(node.Attr("type")) {
		case "linear":
			slope, icpt := numAttr(node, "slope", 1), numAttr(node, "intercept", 0)
			funcs[ch] = func(v float64) float64 { return slope*v + icpt }
		case "gamma":
			amp, exp, off := numAttr(node, "amplitude", 1), numAttr(node, "exponent", 1), numAttr(node, "offset", 0)
			funcs[ch] = func(v float64) float64 { return amp*math.Pow(v, exp) + off }
		case "table", "discrete":
			vals := svgNumbers(node.Attr("tablevalues"))
			discrete := strings.ToLower(node.Attr("type")) == "discrete"
			if len(vals) == 0 {
				continue
			}
			funcs[ch] = func(v float64) float64 {
				n := len(vals)
				if discrete {
					k := min(int(v*float64(n)), n-1)
					return vals[k]
				}
				if n == 1 {
					return vals[0]
				}
				pos := v * float64(n-1)
				k := min(int(pos), n-2)
				return vals[k] + (pos-float64(k))*(vals[k+1]-vals[k])
			}
		}
	}
	if len(funcs) == 0 {
		return nil
	}
	ap := func(ch string, v float64) float64 {
		if f, ok := funcs[ch]; ok {
			return clamp(f(v), 0, 1)
		}
		return v
	}
	return func(c pdf.Color) pdf.Color {
		return pdf.Color{R: ap("r", c.R), G: ap("g", c.G), B: ap("b", c.B), A: alphaField(math.Max(ap("a", c.Alpha()), 0.001))}
	}
}

// filtered draws an element through its filter. It returns false when the
// filter has nothing we can render (the element is then drawn as is).
func (r *svgRenderer) filtered(n *htmlparse.Node, st, parent svgState) bool {
	f := r.doc.ids[st.filter]
	if f == nil || f.Tag != "filter" {
		return false
	}
	plan := r.parseFilter(f)
	if plan.shadow != nil {
		saved := r.override
		c := plan.shadow.color
		r.override = &c
		r.out.WriteString("q " + mat{1, 0, 0, 1, plan.shadow.dx, plan.shadow.dy}.ops())
		r.blurred(n, st, parent, plan.shadow.blur)
		r.out.WriteString("Q\n")
		r.override = saved
	}
	r.fx = append(r.fx, plan.fx...)
	r.out.WriteString("q " + mat{1, 0, 0, 1, plan.dx, plan.dy}.ops())
	r.blurred(n, st, parent, plan.blur)
	r.out.WriteString("Q\n")
	r.fx = r.fx[:len(r.fx)-len(plan.fx)]
	return true
}

// blurred draws the element; with a blur radius, as several translucent
// copies around the original position (an approximation of a Gaussian).
func (r *svgRenderer) blurred(n *htmlparse.Node, st, parent svgState, sigma float64) {
	if sigma <= 0.3 {
		r.content(n, st, parent)
		return
	}
	const k = 10
	saved := r.alpha
	r.alpha = saved * (1 - math.Pow(0.12, 1.0/k))
	rad := sigma * 1.2
	for i := 0; i < k; i++ {
		a := float64(i) * 2 * math.Pi / k
		r.out.WriteString("q " + mat{1, 0, 0, 1, math.Cos(a) * rad, math.Sin(a) * rad}.ops())
		r.content(n, st, parent)
		r.out.WriteString("Q\n")
	}
	r.alpha = saved * 0.35
	r.content(n, st, parent)
	r.alpha = saved
}

func rgbOp(c pdf.Color, op string) string {
	return pdf.Num(c.R) + " " + pdf.Num(c.G) + " " + pdf.Num(c.B) + " " + op
}

// stops returns the color stops of a gradient (following href chains).
func (r *svgRenderer) stops(g *htmlparse.Node) []pdf.Stop {
	for i := 0; i < 8 && g != nil; i++ {
		var out []pdf.Stop
		for _, c := range g.Children {
			if c.Type != htmlparse.ElementNode || c.Tag != "stop" {
				continue
			}
			st := defaultSVGState(1, 1)
			color, op := pdf.Color{}, 1.0
			decls := []css.Decl{}
			for _, p := range []string{"stop-color", "stop-opacity"} {
				if v, ok := c.Attrs[p]; ok {
					decls = append(decls, css.Decl{Prop: p, Value: v})
				}
			}
			decls = append(decls, r.doc.sheet.Match(c, "")...)
			decls = append(decls, css.ParseDecls(c.Attr("style"))...)
			for _, d := range decls {
				switch d.Prop {
				case "stop-color":
					if p, ok := r.e.svgPaintOf(d.Value, &st); ok && !p.none {
						color = p.color
					}
				case "stop-opacity":
					op = parseOpacity(d.Value)
				}
			}
			off := strings.TrimSpace(c.Attr("offset"))
			o := 0.0
			if strings.HasSuffix(off, "%") {
				o, _ = strconv.ParseFloat(strings.TrimSuffix(off, "%"), 64)
				o /= 100
			} else {
				o, _ = strconv.ParseFloat(off, 64)
			}
			if len(out) > 0 {
				o = math.Max(o, out[len(out)-1].Offset)
			}
			a := color.Alpha() * op
			color = pdf.Color{R: color.R*a + 1 - a, G: color.G*a + 1 - a, B: color.B*a + 1 - a}
			color = r.col(color)
			out = append(out, pdf.Stop{Offset: clamp(o, 0, 1), Color: color})
		}
		if len(out) > 0 {
			if out[0].Offset > 0 {
				out = append([]pdf.Stop{{Offset: 0, Color: out[0].Color}}, out...)
			}
			if out[len(out)-1].Offset < 1 {
				out = append(out, pdf.Stop{Offset: 1, Color: out[len(out)-1].Color})
			}
			return out
		}
		g = r.doc.ids[strings.TrimPrefix(firstNonEmpty(g.Attr("href"), g.Attr("xlink:href")), "#")]
	}
	return nil
}

// gradient creates the shading for a gradient fill and the matrix that
// maps its coordinate system into the current user space.
func (r *svgRenderer) gradient(id string, b *pathBuilder) (*pdf.Shading, mat, bool) {
	g := r.doc.ids[id]
	if g == nil || (g.Tag != "lineargradient" && g.Tag != "radialgradient") {
		return nil, mat{}, false
	}
	stops := r.stops(g)
	if len(stops) == 0 {
		return nil, mat{}, false
	}
	attr := func(name, def string) string {
		for n, i := g, 0; n != nil && i < 8; i++ {
			if v := n.Attr(name); v != "" {
				return v
			}
			n = r.doc.ids[strings.TrimPrefix(firstNonEmpty(n.Attr("href"), n.Attr("xlink:href")), "#")]
		}
		return def
	}
	bbox := attr("gradientunits", "objectBoundingBox") != "userSpaceOnUse"
	m := mat{1, 0, 0, 1, 0, 0}
	ref := 1.0
	if bbox {
		w, h := b.maxX-b.minX, b.maxY-b.minY
		if w <= 0 || h <= 0 {
			return nil, mat{}, false
		}
		m = mat{w, 0, 0, h, b.minX, b.minY}
	} else {
		ref = 100
	}
	if t := attr("gradienttransform", ""); t != "" {
		m = m.mul(parseSVGTransform(t))
	}
	val := func(name, def string) float64 {
		v := attr(name, def)
		if strings.HasSuffix(v, "%") {
			f, _ := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
			if bbox {
				return f / 100
			}
			return f / 100 * ref
		}
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	if g.Tag == "lineargradient" {
		sh := r.e.doc.AddRawShading(false, []float64{val("x1", "0%"), val("y1", "0%"), val("x2", "100%"), val("y2", "0%")}, stops)
		return sh, m, true
	}
	cx, cy, rr := val("cx", "50%"), val("cy", "50%"), val("r", "50%")
	fx, fy := val("fx", attr("cx", "50%")), val("fy", attr("cy", "50%"))
	sh := r.e.doc.AddRawShading(true, []float64{fx, fy, 0, cx, cy, rr}, stops)
	return sh, m, true
}

func (r *svgRenderer) text(n *htmlparse.Node, st svgState) {
	x := svgLen(n.Attr("x"), st.vw, st.fontSize)
	y := svgLen(n.Attr("y"), st.vh, st.fontSize)
	type run struct {
		text string
		st   svgState
		dx   float64
	}
	var runs []run
	var walk func(*htmlparse.Node, svgState)
	walk = func(node *htmlparse.Node, s svgState) {
		for _, c := range node.Children {
			switch c.Type {
			case htmlparse.TextNode:
				t := strings.Join(strings.Fields(c.Text), " ")
				if strings.TrimSpace(c.Text) != "" && len(c.Text) > 0 && (c.Text[0] == ' ' || c.Text[0] == '\n') && len(runs) > 0 {
					t = " " + t
				}
				if t != "" {
					runs = append(runs, run{text: t, st: s})
				}
			case htmlparse.ElementNode:
				if c.Tag == "tspan" || c.Tag == "a" {
					cs := s
					r.e.applySVGStyle(r.doc, c, &cs)
					walk(c, cs)
				}
			}
		}
	}
	walk(n, st)
	if len(runs) == 0 || !st.visible {
		return
	}
	styleOf := func(s svgState) *style {
		hs := rootStyle()
		hs.family, hs.weight, hs.italic = s.family, s.weight, s.italic
		hs.size = s.fontSize
		return hs
	}
	total := 0.0
	for _, rn := range runs {
		total += r.e.textWidth(styleOf(rn.st), rn.text)
	}
	switch st.anchor {
	case "middle":
		x -= total / 2
	case "end":
		x -= total
	}
	for _, rn := range runs {
		hs := styleOf(rn.st)
		c := rn.st.fill.color
		if rn.st.fill.ref != "" {
			if g := r.doc.ids[rn.st.fill.ref]; g != nil {
				if stops := r.stops(g); len(stops) > 0 {
					c = stops[0].Color
				}
			}
		}
		c = r.col(c)
		c.A = alphaField(c.Alpha() * rn.st.fillOp * rn.st.opacity * r.alpha)
		for _, seg := range r.e.chain(hs).segments(rn.text) {
			if !rn.st.fill.none {
				r.out.WriteString(r.page.TextOps(pdf.TextStyle{Font: seg.cf.f, Size: hs.size, Color: c,
					FakeBold: seg.cf.fakeBold, FakeItalic: seg.cf.fakeItalic}, x, y, seg.text))
			}
			x += seg.cf.f.Width(seg.text, hs.size)
		}
	}
}

func (r *svgRenderer) image(n *htmlparse.Node, st svgState) {
	href := firstNonEmpty(n.Attr("href"), n.Attr("xlink:href"))
	pic := r.e.loadImage(href)
	if pic == nil || pic.raster == nil {
		return
	}
	x, y := svgLen(n.Attr("x"), st.vw, st.fontSize), svgLen(n.Attr("y"), st.vh, st.fontSize)
	w, h := svgLen(n.Attr("width"), st.vw, st.fontSize), svgLen(n.Attr("height"), st.vh, st.fontSize)
	if w <= 0 || h <= 0 {
		w, h = pic.w, pic.h
	}
	// the image is drawn upright in the y-down user space
	r.out.WriteString("q " + r.gs(st.opacity) + mat{w, 0, 0, -h, x, y + h}.ops() + pic.raster.Name() + " Do Q\n")
}

// urlRef returns the id in url(#id), or "" (for none or other values).
func urlRef(v string) string {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(strings.ToLower(v), "url(") {
		return ""
	}
	end := strings.IndexByte(v, ')')
	if end < 0 {
		return ""
	}
	ref := strings.Trim(strings.TrimSpace(v[4:end]), `"'`)
	if i := strings.IndexByte(ref, '#'); i >= 0 {
		ref = ref[i+1:]
	}
	return ref
}
