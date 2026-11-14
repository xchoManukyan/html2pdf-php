package pdf

import (
	"fmt"
	"strings"
)

// Radii are corner radii: top-left, top-right, bottom-right, bottom-left.
type Radii [4]float64

// IsZero reports whether all corners are square.
func (r Radii) IsZero() bool { return r[0] <= 0 && r[1] <= 0 && r[2] <= 0 && r[3] <= 0 }

// Rect is a box in top-left coordinates with optional rounded corners.
type Rect struct {
	X, Y, W, H float64
	R          Radii
}

// Grow returns the rectangle expanded by d on every side (radii follow).
func (r Rect) Grow(d float64) Rect {
	out := Rect{r.X - d, r.Y - d, r.W + 2*d, r.H + 2*d, r.R}
	for i := range out.R {
		if r.R[i] > 0 {
			out.R[i] = max(r.R[i]+d, 0)
		}
	}
	return out
}

// Layer selects where an operation is painted.
type Layer struct {
	Back bool // background layer (sorted by Z) instead of foreground
	Z    int
}

// Dash describes a stroke pattern.
type Dash struct {
	Array    []float64
	RoundCap bool
}

func (p *Page) emit(l Layer, op string) {
	if op == "" {
		return
	}
	if l.Back {
		p.background = append(p.background, bgOp{l.Z, op})
	} else {
		p.foreground.WriteString(op)
	}
}

// path returns PDF path construction operators for a (rounded) rectangle.
func (p *Page) path(r Rect) string {
	if r.W <= 0 || r.H <= 0 {
		return ""
	}
	H := p.doc.Height
	if r.R.IsZero() {
		return fmt.Sprintf("%s %s %s %s re ", num(r.X), num(H-r.Y-r.H), num(r.W), num(r.H))
	}
	rad := r.R
	// CSS: scale all radii down when adjacent ones overlap.
	f := 1.0
	for _, s := range [][3]float64{{rad[0], rad[1], r.W}, {rad[1], rad[2], r.H}, {rad[2], rad[3], r.W}, {rad[3], rad[0], r.H}} {
		if sum := s[0] + s[1]; sum > s[2] && sum > 0 {
			f = min(f, s[2]/sum)
		}
	}
	for i := range rad {
		rad[i] = max(rad[i]*f, 0)
	}
	const k = 0.4477 // 1 - 0.5523: control points of a quarter ellipse
	x0, x1 := r.X, r.X+r.W
	y0, y1 := r.Y, r.Y+r.H
	pt := func(x, y float64) string { return num(x) + " " + num(H-y) }
	var b strings.Builder
	tl, tr, br, bl := rad[0], rad[1], rad[2], rad[3]
	fmt.Fprintf(&b, "%s m %s l ", pt(x0+tl, y0), pt(x1-tr, y0))
	if tr > 0 {
		fmt.Fprintf(&b, "%s %s %s c ", pt(x1-tr*k, y0), pt(x1, y0+tr*k), pt(x1, y0+tr))
	}
	fmt.Fprintf(&b, "%s l ", pt(x1, y1-br))
	if br > 0 {
		fmt.Fprintf(&b, "%s %s %s c ", pt(x1, y1-br*k), pt(x1-br*k, y1), pt(x1-br, y1))
	}
	fmt.Fprintf(&b, "%s l ", pt(x0+bl, y1))
	if bl > 0 {
		fmt.Fprintf(&b, "%s %s %s c ", pt(x0+bl*k, y1), pt(x0, y1-bl*k), pt(x0, y1-bl))
	}
	fmt.Fprintf(&b, "%s l ", pt(x0, y0+tl))
	if tl > 0 {
		fmt.Fprintf(&b, "%s %s %s c ", pt(x0, y0+tl*k), pt(x0+tl*k, y0), pt(x0+tl, y0))
	}
	b.WriteString("h ")
	return b.String()
}

func dashOps(d Dash) string {
	var b strings.Builder
	if len(d.Array) > 0 {
		b.WriteString("[")
		for i, v := range d.Array {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(num(v))
		}
		b.WriteString("] 0 d ")
	}
	if d.RoundCap {
		b.WriteString("1 J ")
	}
	return b.String()
}

// FillShape fills a (rounded) rectangle.
func (p *Page) FillShape(l Layer, r Rect, c Color) {
	if path := p.path(r); path != "" {
		p.emit(l, "q "+p.gs(c.Alpha())+colorOp(c, "rg")+" "+path+"f Q\n")
	}
}

// StrokeShape strokes the outline of a (rounded) rectangle; the stroke is
// centred on the outline.
func (p *Page) StrokeShape(l Layer, r Rect, width float64, c Color, d Dash) {
	if width <= 0 {
		return
	}
	if path := p.path(r); path != "" {
		p.emit(l, fmt.Sprintf("q %s%s %s w %s%sS Q\n", p.gs(c.Alpha()), colorOp(c, "RG"), num(width), dashOps(d), path))
	}
}

// StyledLine strokes a straight line with a dash pattern.
func (p *Page) StyledLine(l Layer, x1, y1, x2, y2, width float64, c Color, d Dash) {
	if width <= 0 {
		return
	}
	H := p.doc.Height
	p.emit(l, fmt.Sprintf("q %s%s %s w %s%s %s m %s %s l S Q\n", p.gs(c.Alpha()), colorOp(c, "RG"), num(width),
		dashOps(d), num(x1), num(H-y1), num(x2), num(H-y2)))
}

// FillFrame fills the area between outer and inner (even-odd rule),
// clipped to clip. Used for inset shadows.
func (p *Page) FillFrame(l Layer, clip, outer, inner Rect, c Color) {
	cp, op, ip := p.path(clip), p.path(outer), p.path(inner)
	if cp == "" || op == "" {
		return
	}
	p.emit(l, "q "+cp+"W n "+p.gs(c.Alpha())+colorOp(c, "rg")+" "+op+ip+"f* Q\n")
}

// Stop is one color stop of a gradient (offset in 0..1).
type Stop struct {
	Offset float64
	Color  Color
}

// Shading is an axial (linear) or radial gradient.
type Shading struct {
	id, objID int
	radial    bool
	coords    []float64 // PDF space
	stops     []Stop
}

// AddLinearShading creates a gradient from (x0,y0) to (x1,y1), in top-left
// coordinates.
func (d *Document) AddLinearShading(x0, y0, x1, y1 float64, stops []Stop) *Shading {
	sh := &Shading{id: len(d.shadings) + 1, coords: []float64{x0, d.Height - y0, x1, d.Height - y1}, stops: stops}
	d.shadings = append(d.shadings, sh)
	return sh
}

// AddRadialShading creates a circular gradient centred at (cx, cy) with
// radius r, in top-left coordinates.
func (d *Document) AddRadialShading(cx, cy, r float64, stops []Stop) *Shading {
	y := d.Height - cy
	sh := &Shading{id: len(d.shadings) + 1, radial: true, coords: []float64{cx, y, 0, cx, y, r}, stops: stops}
	d.shadings = append(d.shadings, sh)
	return sh
}

func rgb(c Color) string { return "[" + num(c.R) + " " + num(c.G) + " " + num(c.B) + "]" }

func (sh *Shading) dict() string {
	stops := sh.stops
	if len(stops) == 1 {
		stops = append(stops, stops[0])
	}
	var fn string
	if len(stops) == 2 {
		fn = fmt.Sprintf("<< /FunctionType 2 /Domain [0 1] /C0 %s /C1 %s /N 1 >>", rgb(stops[0].Color), rgb(stops[1].Color))
	} else {
		var fns, bounds, encode strings.Builder
		for i := 0; i+1 < len(stops); i++ {
			fmt.Fprintf(&fns, "<< /FunctionType 2 /Domain [0 1] /C0 %s /C1 %s /N 1 >> ", rgb(stops[i].Color), rgb(stops[i+1].Color))
			encode.WriteString("0 1 ")
			if i > 0 {
				bounds.WriteString(num(stops[i].Offset) + " ")
			}
		}
		fn = fmt.Sprintf("<< /FunctionType 3 /Domain [0 1] /Functions [ %s] /Bounds [ %s] /Encode [ %s] >>",
			fns.String(), bounds.String(), encode.String())
	}
	var coords strings.Builder
	for _, c := range sh.coords {
		coords.WriteString(num(c) + " ")
	}
	typ := 2
	if sh.radial {
		typ = 3
	}
	return fmt.Sprintf("<< /ShadingType %d /ColorSpace /DeviceRGB /Coords [ %s] /Function %s /Extend [true true] >>",
		typ, coords.String(), fn)
}

// FillShading paints a gradient inside a (rounded) rectangle. matrix, if
// not nil, transforms the gradient (used for elliptical radial gradients)
// and is given in PDF space.
func (p *Page) FillShading(l Layer, clip Rect, sh *Shading, matrix []float64, alpha float64) {
	path := p.path(clip)
	if path == "" || sh == nil {
		return
	}
	cm := ""
	if len(matrix) == 6 {
		cm = fmt.Sprintf("%s %s %s %s %s %s cm ", num(matrix[0]), num(matrix[1]), num(matrix[2]), num(matrix[3]), num(matrix[4]), num(matrix[5]))
	}
	p.emit(l, fmt.Sprintf("q %s%sW n %s/Sh%d sh Q\n", p.gs(alpha), path, cm, sh.id))
}

// DrawImageTiles paints an image at each tile position (x, y, w, h),
// clipped to clip.
func (p *Page) DrawImageTiles(l Layer, img *Image, clip Rect, tiles [][4]float64, alpha float64) {
	path := p.path(clip)
	if path == "" || len(tiles) == 0 {
		return
	}
	var b strings.Builder
	b.WriteString("q " + p.gs(alpha) + path + "W n ")
	for _, t := range tiles {
		fmt.Fprintf(&b, "q %s 0 0 %s %s %s cm /Im%d Do Q ", num(t[2]), num(t[3]), num(t[0]), num(p.doc.Height-t[1]-t[3]), img.id)
	}
	b.WriteString("Q\n")
	p.emit(l, b.String())
}

// ForegroundMark returns the current end of the foreground content, for a
// later ClipForeground.
func (p *Page) ForegroundMark() int { return p.foreground.Len() }

// TransformSince applies the PDF matrix m to everything drawn on this page
// since the given foreground and background marks.
func (p *Page) TransformSince(fgMark, bgMark int, m [6]float64) {
	cm := fmt.Sprintf("q %s %s %s %s %s %s cm\n", num(m[0]), num(m[1]), num(m[2]), num(m[3]), num(m[4]), num(m[5]))
	if fgMark <= p.foreground.Len() {
		tail := append([]byte(nil), p.foreground.Bytes()[fgMark:]...)
		p.foreground.Truncate(fgMark)
		p.foreground.WriteString(cm)
		p.foreground.Write(tail)
		p.foreground.WriteString("Q\n")
	}
	for i := bgMark; i < len(p.background); i++ {
		p.background[i].op = cm + p.background[i].op + "Q\n"
	}
}

// BackgroundMark returns the number of background operations so far.
func (p *Page) BackgroundMark() int { return len(p.background) }

// ClipBackground clips every background operation added since mark (they
// are wrapped one by one, so z-ordering still works).
func (p *Page) ClipBackground(mark int, r Rect) {
	path := p.path(r)
	if path == "" {
		return
	}
	for i := mark; i < len(p.background); i++ {
		p.background[i].op = "q " + path + "W n\n" + p.background[i].op + "Q\n"
	}
}

// ClipForeground clips everything drawn on the foreground since mark to r
// (used for overflow: hidden and rounded images).
func (p *Page) ClipForeground(mark int, r Rect) {
	path := p.path(r)
	if path == "" || mark > p.foreground.Len() {
		return
	}
	tail := append([]byte(nil), p.foreground.Bytes()[mark:]...)
	p.foreground.Truncate(mark)
	p.foreground.WriteString("q " + path + "W n\n")
	p.foreground.Write(tail)
	p.foreground.WriteString("Q\n")
}
