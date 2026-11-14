package render

import (
	"math"
	"strconv"
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/css"
	"github.com/xchoManukyan/html2pdf/converter/internal/pdf"
)

// boxShadow is one box-shadow (or text-shadow) layer.
type boxShadow struct {
	x, y, blur, spread float64
	color              pdf.Color
	inset              bool
}

// applyPaint handles the visual-effect properties. It reports whether the
// property was one of them.
func (e *engine) applyPaint(s *style, prop, raw, v string, parent *style) bool {
	ctx := e.lengthCtx(s)
	length := func(val string) (float64, bool) { return css.ParseLength(val, ctx) }
	switch prop {
	case "background":
		e.applyBackground(s, raw)
	case "background-image":
		s.bgImages = nil
		if v != "none" {
			for _, layer := range splitTopLevelComma(raw) {
				if l := strings.TrimSpace(layer); l != "" && !strings.EqualFold(l, "none") {
					s.bgImages = append(s.bgImages, l)
				}
			}
		}
	case "list-style-image":
		s.listImage = ""
		if isImageValue(raw) {
			s.listImage = raw
		}
	case "list-style":
		for _, tok := range splitValue(raw) {
			if isImageValue(tok) {
				s.listImage = tok
			}
		}
		return false // the main switch handles the type keywords
	case "background-repeat":
		s.bgRepeat = trimList(splitTopLevelComma(v))
	case "background-position":
		s.bgPos = trimList(splitTopLevelComma(v))
	case "background-size":
		s.bgSize = trimList(splitTopLevelComma(v))
	case "opacity":
		f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
		if err == nil {
			if strings.HasSuffix(v, "%") {
				f /= 100
			}
			s.alpha = parent.alpha * clamp(f, 0, 1)
		}
	case "border-radius":
		// "a b c d / e f g h": horizontal radii; the vertical part is ignored
		h, _, _ := strings.Cut(v, "/")
		if vals := expandBox(splitValue(h)); vals != nil {
			copy(s.radiusExpr[:], vals)
		}
	case "border-top-left-radius", "border-start-start-radius":
		s.radiusExpr[0] = firstField(v)
	case "border-top-right-radius", "border-start-end-radius":
		s.radiusExpr[1] = firstField(v)
	case "border-bottom-right-radius", "border-end-end-radius":
		s.radiusExpr[2] = firstField(v)
	case "border-bottom-left-radius", "border-end-start-radius":
		s.radiusExpr[3] = firstField(v)
	case "box-shadow":
		s.shadows = e.parseShadows(v, s)
	case "text-shadow":
		s.textShadow = nil
		if sh := e.parseShadows(v, s); len(sh) > 0 {
			s.textShadow = &sh[0]
		}
	case "outline":
		s.outlineStyle, s.outlineW, s.outlineColSet = "none", 2.25, false
		for _, tok := range splitValue(v) {
			switch {
			case borderStyles[tok] || tok == "auto":
				s.outlineStyle = tok
			default:
				if c, ok := e.color(tok, s); ok {
					s.outlineColor, s.outlineColSet = c, true
				} else if l, ok := borderLength(length)(tok); ok {
					s.outlineW = l
				}
			}
		}
	case "outline-width":
		if l, ok := borderLength(length)(v); ok {
			s.outlineW = l
		}
	case "outline-style":
		s.outlineStyle = v
	case "outline-color":
		if c, ok := e.color(v, s); ok {
			s.outlineColor, s.outlineColSet = c, true
		}
	case "outline-offset":
		if l, ok := length(v); ok {
			s.outlineOffset = l
		}
	case "text-overflow":
		s.ellipsis = strings.Contains(v, "ellipsis")
	case "overflow", "overflow-x", "overflow-y":
		s.overflowHidden = strings.Contains(v, "hidden") || strings.Contains(v, "clip") ||
			strings.Contains(v, "auto") || strings.Contains(v, "scroll")
	default:
		return false
	}
	return true
}

func firstField(v string) string {
	if f := strings.Fields(v); len(f) > 0 {
		return f[0]
	}
	return ""
}

func trimList(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.TrimSpace(s)
	}
	return out
}

func isImageValue(tok string) bool {
	t := strings.ToLower(tok)
	return strings.HasPrefix(t, "url(") || strings.Contains(t, "gradient(")
}

var repeatWords = map[string]bool{"repeat": true, "no-repeat": true, "repeat-x": true, "repeat-y": true, "space": true, "round": true}

// applyBackground parses the background shorthand (all layers).
func (e *engine) applyBackground(s *style, raw string) {
	s.bg = nil
	s.bgImages, s.bgRepeat, s.bgPos, s.bgSize = nil, nil, nil, nil
	if strings.EqualFold(strings.TrimSpace(raw), "none") {
		return
	}
	for _, layer := range splitTopLevelComma(raw) {
		layer = strings.TrimSpace(layer)
		// separate "position / size"
		layer = strings.ReplaceAll(layer, "/", " / ")
		image, repeat := "", ""
		var pos, size []string
		afterSlash := false
		for _, tok := range splitValue(layer) {
			lt := strings.ToLower(tok)
			switch {
			case isImageValue(tok):
				image = strings.ReplaceAll(tok, " / ", "/")
			case lt == "/":
				afterSlash = true
			case repeatWords[lt]:
				repeat = strings.TrimSpace(repeat + " " + lt)
			case lt == "none" || lt == "scroll" || lt == "fixed" || lt == "local" ||
				strings.HasSuffix(lt, "-box") || lt == "text":
			default:
				if c, ok := e.color(lt, s); ok {
					s.bg = &c
				} else if _, isColor := css.ParseColor(lt); isColor || lt == "currentcolor" {
					// transparent
				} else if afterSlash {
					size = append(size, lt)
				} else {
					pos = append(pos, lt)
				}
			}
		}
		if image != "" {
			s.bgImages = append(s.bgImages, image)
			s.bgRepeat = append(s.bgRepeat, orDefault(repeat, "repeat"))
			s.bgPos = append(s.bgPos, orDefault(strings.Join(pos, " "), "0% 0%"))
			s.bgSize = append(s.bgSize, orDefault(strings.Join(size, " "), "auto"))
		}
	}
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func (e *engine) parseShadows(v string, s *style) []boxShadow {
	if v == "none" {
		return nil
	}
	ctx := e.lengthCtx(s)
	var out []boxShadow
	for _, part := range splitTopLevelComma(v) {
		sh := boxShadow{color: pdf.Color{A: 0.5}} // default: currentColor-ish translucent black
		sh.color = s.color
		var lens []float64
		for _, tok := range splitValue(strings.TrimSpace(part)) {
			if tok == "inset" {
				sh.inset = true
			} else if c, ok := e.color(tok, s); ok {
				sh.color = c
			} else if l, ok := css.ParseLength(tok, ctx); ok {
				lens = append(lens, l)
			}
		}
		if len(lens) < 2 {
			continue
		}
		sh.x, sh.y = lens[0], lens[1]
		if len(lens) > 2 {
			sh.blur = max(lens[2], 0)
		}
		if len(lens) > 3 {
			sh.spread = lens[3]
		}
		out = append(out, sh)
	}
	return out
}

// radii resolves border-radius against the box size.
func (e *engine) radii(st *style, w, h float64) pdf.Radii {
	var r pdf.Radii
	for i, expr := range st.radiusExpr {
		if expr == "" {
			continue
		}
		ref := w
		if strings.HasSuffix(expr, "%") {
			// percentages use width horizontally; average both axes for a circle
			ref = (w + h) / 2
		}
		if v, ok := e.resolveLength(expr, st, ref); ok && v > 0 {
			r[i] = v
		}
	}
	return r
}

// ---------------------------------------------------------------------
// Box painting

// paintBox paints shadows, background (color, gradients, images), borders
// and outline of a border box. top/bottom say whether the top and bottom
// edges belong to this segment (boxes split by a page break get no
// borders or rounding at the cut).
func (e *engine) paintBox(p *pdf.Page, z int, x, y, w, h float64, st *style, top, bottom bool) {
	if !st.visible || w <= 0 || h <= 0 {
		return
	}
	r := e.radii(st, w, h)
	if !top {
		r[0], r[1] = 0, 0
	}
	if !bottom {
		r[2], r[3] = 0, 0
	}
	box := pdf.Rect{X: x, Y: y, W: w, H: h, R: r}
	back := pdf.Layer{Back: true, Z: z}

	// outer shadows
	for i := len(st.shadows) - 1; i >= 0; i-- {
		if sh := st.shadows[i]; !sh.inset {
			e.paintShadow(p, back, box, sh, st)
		}
	}
	if st.bg != nil {
		p.FillShape(back, box, st.paint(*st.bg))
	}
	for i := len(st.bgImages) - 1; i >= 0; i-- {
		e.paintBackgroundLayer(p, back, box, st, i)
	}
	for i := len(st.shadows) - 1; i >= 0; i-- {
		if sh := st.shadows[i]; sh.inset {
			e.paintInsetShadow(p, back, box, sh, st)
		}
	}
	e.paintBorders(p, box, st, top, bottom)
	if st.outlineW > 0 && st.outlineStyle != "" && st.outlineStyle != "none" {
		c := st.color
		if st.outlineColSet {
			c = st.outlineColor
		}
		o := box.Grow(st.outlineOffset + st.outlineW/2)
		p.StrokeShape(pdf.Layer{}, o, st.outlineW, st.paint(c), dashFor(st.outlineStyle, st.outlineW))
	}
}

func (e *engine) paintShadow(p *pdf.Page, l pdf.Layer, box pdf.Rect, sh boxShadow, st *style) {
	base := box.Grow(sh.spread)
	base.X += sh.x
	base.Y += sh.y
	alpha := sh.color.Alpha() * st.alpha
	if sh.blur <= 0.5 {
		c := sh.color
		c.A = alpha
		p.FillShape(l, base, c)
		return
	}
	// Approximate a Gaussian blur with stacked translucent layers growing
	// from -blur/2 to +blur/2 around the shadow edge.
	steps := int(min(max(sh.blur/1.5, 4), 12))
	layerA := 1 - math.Pow(1-alpha, 1/float64(steps))
	for i := 0; i < steps; i++ {
		t := (float64(i)+0.5)/float64(steps) - 0.5
		c := sh.color
		c.A = layerA
		p.FillShape(l, base.Grow(-t*sh.blur), c)
	}
}

func (e *engine) paintInsetShadow(p *pdf.Page, l pdf.Layer, box pdf.Rect, sh boxShadow, st *style) {
	inner := box.Grow(-sh.spread)
	inner.X += sh.x
	inner.Y += sh.y
	outer := box.Grow(math.Abs(sh.x) + math.Abs(sh.y) + sh.blur + 10)
	alpha := sh.color.Alpha() * st.alpha
	steps := 1
	if sh.blur > 0.5 {
		steps = int(min(max(sh.blur/1.5, 4), 12))
	}
	layerA := 1 - math.Pow(1-alpha, 1/float64(steps))
	for i := 0; i < steps; i++ {
		t := 0.0
		if steps > 1 {
			t = (float64(i)+0.5)/float64(steps) - 0.5
		}
		c := sh.color
		c.A = layerA
		p.FillFrame(l, box, outer, inner.Grow(-t*sh.blur), c)
	}
}

// dashFor returns the stroke pattern of a CSS border style.
func dashFor(style string, w float64) pdf.Dash {
	switch style {
	case "dashed":
		d := max(w*3, 2)
		return pdf.Dash{Array: []float64{d, d}}
	case "dotted":
		return pdf.Dash{Array: []float64{0, max(w*2, 1)}, RoundCap: true}
	}
	return pdf.Dash{}
}

func shade(c pdf.Color, f float64) pdf.Color {
	return pdf.Color{R: c.R * f, G: c.G * f, B: c.B * f, A: c.A}
}

// sideColors returns the colors for 3D styles (inset, outset, groove,
// ridge): the first for the outer half, the second for the inner half.
func sideColors(style string, side int, c pdf.Color) (pdf.Color, pdf.Color) {
	dark, light := shade(c, 0.6), c
	if c.R+c.G+c.B < 0.3 { // black: make the light side gray
		light = pdf.Color{R: 0.6, G: 0.6, B: 0.6, A: c.A}
	}
	topLeft := side == 0 || side == 3
	switch style {
	case "inset":
		if topLeft {
			return dark, dark
		}
		return light, light
	case "outset":
		if topLeft {
			return light, light
		}
		return dark, dark
	case "groove":
		if topLeft {
			return dark, light
		}
		return light, dark
	case "ridge":
		if topLeft {
			return light, dark
		}
		return dark, light
	}
	return c, c
}

// paintBorders draws the four borders inside the border box.
func (e *engine) paintBorders(p *pdf.Page, box pdf.Rect, st *style, top, bottom bool) {
	if !st.hasBorder() {
		return
	}
	fg := pdf.Layer{}
	widths := [4]float64{st.bw(0), st.bw(1), st.bw(2), st.bw(3)}
	if !top {
		widths[0] = 0
	}
	if !bottom {
		widths[2] = 0
	}
	// Uniform border: one stroke that follows rounded corners.
	uniform := true
	for i := 1; i < 4; i++ {
		if widths[i] != widths[0] || st.borderStyle[i] != st.borderStyle[0] || st.borderColor[i] != st.borderColor[0] {
			uniform = false
		}
	}
	style0 := st.borderStyle[0]
	if uniform && (style0 == "solid" || style0 == "dashed" || style0 == "dotted") {
		w := widths[0]
		p.StrokeShape(fg, box.Grow(-w/2), w, st.paint(st.borderColor[0]), dashFor(style0, w))
		return
	}
	if uniform && style0 == "double" && widths[0] >= 3 {
		w := widths[0] / 3
		c := st.paint(st.borderColor[0])
		p.StrokeShape(fg, box.Grow(-w/2), w, c, pdf.Dash{})
		p.StrokeShape(fg, box.Grow(-widths[0]+w/2), w, c, pdf.Dash{})
		return
	}
	x0, y0, x1, y1 := box.X, box.Y, box.X+box.W, box.Y+box.H
	for side := 0; side < 4; side++ {
		w := widths[side]
		if w <= 0 {
			continue
		}
		// line along the side, centred at depth d from the outer edge
		line := func(d, lw float64, c pdf.Color, dash pdf.Dash) {
			switch side {
			case 0:
				p.StyledLine(fg, x0, y0+d, x1, y0+d, lw, c, dash)
			case 1:
				p.StyledLine(fg, x1-d, y0, x1-d, y1, lw, c, dash)
			case 2:
				p.StyledLine(fg, x0, y1-d, x1, y1-d, lw, c, dash)
			case 3:
				p.StyledLine(fg, x0+d, y0, x0+d, y1, lw, c, dash)
			}
		}
		c := st.paint(st.borderColor[side])
		switch s := st.borderStyle[side]; s {
		case "double":
			if w >= 3 {
				line(w/6, w/3, c, pdf.Dash{})
				line(w-w/6, w/3, c, pdf.Dash{})
			} else {
				line(w/2, w, c, pdf.Dash{})
			}
		case "inset", "outset", "groove", "ridge":
			outer, inner := sideColors(s, side, c)
			line(w/4, w/2, outer, pdf.Dash{})
			line(w*3/4, w/2, inner, pdf.Dash{})
		default:
			line(w/2, w, c, dashFor(s, w))
		}
	}
}

// ---------------------------------------------------------------------
// Background images and gradients

func (e *engine) paintBackgroundLayer(p *pdf.Page, l pdf.Layer, box pdf.Rect, st *style, i int) {
	img := st.bgImages[i]
	pick := func(list []string, def string) string {
		if len(list) == 0 {
			return def
		}
		return list[i%len(list)]
	}
	lower := strings.ToLower(img)
	if strings.Contains(lower, "gradient(") {
		e.paintGradient(p, l, box, st, img)
		return
	}
	// url(data:...)
	open := strings.IndexByte(img, '(')
	end := strings.LastIndexByte(img, ')')
	if open < 0 || end <= open {
		return
	}
	src := strings.Trim(strings.TrimSpace(img[open+1:end]), `"'`)
	im := e.loadImage(src)
	if im == nil {
		return
	}
	// positioning area: the padding box
	pb := pdf.Rect{X: box.X + st.bw(3), Y: box.Y + st.bw(0), W: box.W - st.bw(1) - st.bw(3), H: box.H - st.bw(0) - st.bw(2)}
	nw, nh := im.w*0.75, im.h*0.75
	iw, ih := nw, nh
	size := pick(st.bgSize, "auto")
	switch size {
	case "cover", "contain":
		sx, sy := pb.W/nw, pb.H/nh
		f := max(sx, sy)
		if size == "contain" {
			f = min(sx, sy)
		}
		iw, ih = nw*f, nh*f
	case "auto", "auto auto":
	default:
		parts := strings.Fields(size)
		wv, wok := e.resolveLength(parts[0], st, pb.W)
		hv, hok := 0.0, false
		if len(parts) > 1 {
			hv, hok = e.resolveLength(parts[1], st, pb.H)
		}
		switch {
		case wok && hok:
			iw, ih = wv, hv
		case wok:
			iw, ih = wv, nh*wv/nw
		case hok:
			iw, ih = nw*hv/nh, hv
		}
	}
	if iw <= 0.1 || ih <= 0.1 {
		return
	}
	px, py := e.bgPosition(pick(st.bgPos, "0% 0%"), st, pb.W-iw, pb.H-ih)
	x0, y0 := pb.X+px, pb.Y+py
	rep := strings.Fields(pick(st.bgRepeat, "repeat"))
	rx, ry := true, true
	switch {
	case len(rep) == 2:
		rx, ry = rep[0] != "no-repeat", rep[1] != "no-repeat"
	case rep[0] == "no-repeat":
		rx, ry = false, false
	case rep[0] == "repeat-x":
		ry = false
	case rep[0] == "repeat-y":
		rx = false
	}
	xs, ys := []float64{x0}, []float64{y0}
	if rx {
		xs = tileStarts(x0, iw, box.X, box.X+box.W)
	}
	if ry {
		ys = tileStarts(y0, ih, box.Y, box.Y+box.H)
	}
	var tiles [][4]float64
	for _, ty := range ys {
		for _, tx := range xs {
			if len(tiles) < 4000 {
				tiles = append(tiles, [4]float64{tx, ty, iw, ih})
			}
		}
	}
	if im.raster != nil {
		p.DrawImageTiles(l, im.raster, box, tiles, st.alpha)
		return
	}
	for _, t := range tiles[:min(len(tiles), 200)] {
		im.draw(e, p, l, &box, t[0], t[1], t[2], t[3], st.alpha)
	}
}

func tileStarts(origin, size, from, to float64) []float64 {
	start := origin - math.Ceil((origin-from)/size)*size
	var out []float64
	for v := start; v < to && len(out) < 400; v += size {
		out = append(out, v)
	}
	return out
}

// bgPosition resolves "x y" (keywords, % or lengths); free is the space
// left over in each direction, which percentages refer to.
func (e *engine) bgPosition(v string, st *style, freeX, freeY float64) (float64, float64) {
	f := strings.Fields(v)
	if len(f) == 1 {
		switch f[0] {
		case "top", "bottom":
			f = []string{"center", f[0]}
		default:
			f = append(f, "center")
		}
	}
	if len(f) >= 2 && (f[0] == "top" || f[0] == "bottom" || f[1] == "left" || f[1] == "right") {
		f[0], f[1] = f[1], f[0]
	}
	resolve := func(tok string, free float64) float64 {
		switch tok {
		case "left", "top":
			return 0
		case "center":
			return free / 2
		case "right", "bottom":
			return free
		}
		if strings.HasSuffix(tok, "%") {
			pct, _ := strconv.ParseFloat(strings.TrimSuffix(tok, "%"), 64)
			return free * pct / 100
		}
		l, _ := e.resolveLength(tok, st, free)
		return l
	}
	if len(f) < 2 {
		return 0, 0
	}
	return resolve(f[0], freeX), resolve(f[1], freeY)
}

// gradientStop is a parsed color stop: offset < 0 means "not given".
type gradientStop struct {
	color  pdf.Color
	offset float64
}

func (e *engine) parseStops(args []string, st *style, length float64) []gradientStop {
	var stops []gradientStop
	for _, a := range args {
		toks := splitValue(strings.TrimSpace(a))
		if len(toks) == 0 {
			continue
		}
		var col *pdf.Color
		var pos []float64
		for _, t := range toks {
			if c, ok := e.color(t, st); ok {
				c2 := c
				col = &c2
			} else if _, isColor := css.ParseColor(t); isColor {
				c2 := pdf.Color{R: 1, G: 1, B: 1, A: 0.001}
				col = &c2
			} else if strings.HasSuffix(t, "%") {
				v, _ := strconv.ParseFloat(strings.TrimSuffix(t, "%"), 64)
				pos = append(pos, v/100)
			} else if l, ok := e.resolveLength(t, st, length); ok && length > 0 {
				pos = append(pos, l/length)
			}
		}
		if col == nil {
			continue // color hints are not supported
		}
		if len(pos) == 0 {
			stops = append(stops, gradientStop{*col, -1})
		}
		for _, p := range pos {
			stops = append(stops, gradientStop{*col, p})
		}
	}
	if len(stops) == 0 {
		return nil
	}
	// fill in missing positions
	if stops[0].offset < 0 {
		stops[0].offset = 0
	}
	if last := len(stops) - 1; stops[last].offset < 0 {
		stops[last].offset = 1
	}
	for i := 1; i < len(stops); i++ {
		if stops[i].offset >= 0 && stops[i].offset < stops[i-1].offset {
			stops[i].offset = stops[i-1].offset
		}
	}
	for i := 1; i < len(stops); {
		if stops[i].offset >= 0 {
			i++
			continue
		}
		j := i
		for stops[j].offset < 0 {
			j++
		}
		a, b := stops[i-1].offset, stops[j].offset
		for k := i; k < j; k++ {
			stops[k].offset = a + (b-a)*float64(k-i+1)/float64(j-i+1)
		}
		i = j
	}
	// Transparent stops take the neighbouring color (PDF shadings have no
	// alpha), then everything is composited on white.
	for i := range stops {
		if stops[i].color.Alpha() < 0.01 {
			if i > 0 {
				stops[i].color = stops[i-1].color
			} else if i+1 < len(stops) {
				stops[i].color = stops[i+1].color
			}
			stops[i].color.A = 0.001
		}
	}
	return stops
}

func toPDFStops(gs []gradientStop, st *style) []pdf.Stop {
	out := make([]pdf.Stop, 0, len(gs)+2)
	for _, g := range gs {
		a := g.color.Alpha() * st.alpha
		if g.color.A == 0.001 {
			a = 0
		}
		c := pdf.Color{R: g.color.R*a + 1 - a, G: g.color.G*a + 1 - a, B: g.color.B*a + 1 - a}
		out = append(out, pdf.Stop{Offset: clamp(g.offset, 0, 1), Color: c})
	}
	if out[0].Offset > 0 {
		out = append([]pdf.Stop{{Offset: 0, Color: out[0].Color}}, out...)
	}
	if last := out[len(out)-1]; last.Offset < 1 {
		out = append(out, pdf.Stop{Offset: 1, Color: last.Color})
	}
	return out
}

// repeatStops tiles a repeating gradient's stop list over 0..1.
func repeatStops(gs []gradientStop) []gradientStop {
	first, last := gs[0].offset, gs[len(gs)-1].offset
	period := last - first
	if period <= 0.001 {
		return gs
	}
	var out []gradientStop
	start := first - math.Ceil(first/period)*period
	for base := start; base < 1 && len(out) < 1000; base += period {
		for _, g := range gs {
			out = append(out, gradientStop{g.color, base + g.offset - first})
		}
	}
	// clip to [0,1]
	var clipped []gradientStop
	for i, g := range out {
		if g.offset < 0 && i+1 < len(out) && out[i+1].offset < 0 {
			continue
		}
		if g.offset > 1 && i > 0 && out[i-1].offset > 1 {
			break
		}
		clipped = append(clipped, g)
	}
	return clipped
}

func parseAngle(t string) (float64, bool) {
	units := map[string]float64{"deg": 1, "grad": 0.9, "rad": 180 / math.Pi, "turn": 360}
	for u, f := range units {
		if strings.HasSuffix(t, u) {
			v, err := strconv.ParseFloat(strings.TrimSuffix(t, u), 64)
			return v * f, err == nil
		}
	}
	return 0, false
}

func (e *engine) paintGradient(p *pdf.Page, l pdf.Layer, box pdf.Rect, st *style, raw string) {
	open := strings.IndexByte(raw, '(')
	end := strings.LastIndexByte(raw, ')')
	if open < 0 || end <= open {
		return
	}
	name := strings.ToLower(strings.TrimSpace(raw[:open]))
	name = strings.TrimPrefix(strings.TrimPrefix(name, "-webkit-"), "-moz-")
	args := splitTopLevelComma(raw[open+1 : end])
	repeating := strings.HasPrefix(name, "repeating-")
	w, h := box.W, box.H
	cx, cy := box.X+w/2, box.Y+h/2

	if strings.HasSuffix(name, "linear-gradient") {
		angle := 180.0
		first := strings.ToLower(strings.TrimSpace(args[0]))
		if a, ok := parseAngle(first); ok {
			angle = a
			args = args[1:]
		} else if strings.HasPrefix(first, "to ") {
			dir := strings.Fields(first[3:])
			has := func(s string) bool {
				for _, d := range dir {
					if d == s {
						return true
					}
				}
				return false
			}
			deg := 180 / math.Pi
			switch {
			case has("top") && has("right"):
				angle = math.Atan2(h, w) * deg
			case has("bottom") && has("right"):
				angle = 180 - math.Atan2(h, w)*deg
			case has("bottom") && has("left"):
				angle = 180 + math.Atan2(h, w)*deg
			case has("top") && has("left"):
				angle = 360 - math.Atan2(h, w)*deg
			case has("top"):
				angle = 0
			case has("right"):
				angle = 90
			case has("left"):
				angle = 270
			}
			args = args[1:]
		}
		rad := angle * math.Pi / 180
		dx, dy := math.Sin(rad), -math.Cos(rad)
		length := math.Abs(w*math.Sin(rad)) + math.Abs(h*math.Cos(rad))
		stops := e.parseStops(args, st, length)
		if len(stops) == 0 {
			return
		}
		if repeating {
			stops = repeatStops(stops)
		}
		// The gradient line runs through the centre along (dx, dy); y grows
		// downwards, so 0deg ("to top") is (0, -1).
		x0, y0 := cx-dx*length/2, cy-dy*length/2
		x1, y1 := cx+dx*length/2, cy+dy*length/2
		sh := e.doc.AddLinearShading(x0, y0, x1, y1, toPDFStops(stops, st))
		p.FillShading(l, box, sh, nil, 1)
		return
	}

	if strings.HasSuffix(name, "radial-gradient") {
		shape, sizeKw := "ellipse", "farthest-corner"
		px, py := cx, cy
		var explicit []float64
		first := strings.ToLower(strings.TrimSpace(args[0]))
		isStop := false
		for _, t := range splitValue(first) {
			if _, ok := e.color(t, st); ok {
				isStop = true
			}
		}
		if !isStop {
			spec, at, hasAt := strings.Cut(first, "at ")
			if strings.HasPrefix(first, "at ") {
				spec, at, hasAt = "", first[3:], true
			}
			for _, t := range strings.Fields(spec) {
				switch t {
				case "circle", "ellipse":
					shape = t
				case "closest-side", "farthest-side", "closest-corner", "farthest-corner":
					sizeKw = t
				default:
					ref := w
					if len(explicit) == 1 {
						ref = h
					}
					if v, ok := e.resolveLength(t, st, ref); ok {
						explicit = append(explicit, v)
					}
				}
			}
			if hasAt {
				ox, oy := e.bgPosition(strings.TrimSpace(at), st, w, h)
				px, py = box.X+ox, box.Y+oy
			}
			args = args[1:]
		}
		left, right := px-box.X, box.X+w-px
		top, bottom := py-box.Y, box.Y+h-py
		var rx, ry float64
		switch {
		case len(explicit) == 1:
			rx, ry = explicit[0], explicit[0]
		case len(explicit) >= 2:
			rx, ry = explicit[0], explicit[1]
		default:
			fx, fy := max(left, right), max(top, bottom)
			nx, ny := min(left, right), min(top, bottom)
			switch sizeKw {
			case "closest-side":
				rx, ry = nx, ny
			case "farthest-side":
				rx, ry = fx, fy
			case "closest-corner":
				rx, ry = nx*math.Sqrt2, ny*math.Sqrt2
			default:
				rx, ry = fx*math.Sqrt2, fy*math.Sqrt2
			}
			if shape == "circle" {
				switch sizeKw {
				case "closest-side":
					rx = min(nx, ny)
				case "farthest-side":
					rx = max(fx, fy)
				case "closest-corner":
					rx = math.Hypot(nx, ny)
				default:
					rx = math.Hypot(fx, fy)
				}
				ry = rx
			}
		}
		if rx <= 0 || ry <= 0 {
			return
		}
		stops := e.parseStops(args, st, rx)
		if len(stops) == 0 {
			return
		}
		if repeating {
			stops = repeatStops(stops)
		}
		sh := e.doc.AddRadialShading(px, py, rx, toPDFStops(stops, st))
		var m []float64
		if math.Abs(rx-ry) > 0.01 {
			s := ry / rx
			pyPDF := e.doc.Height - py
			m = []float64{1, 0, 0, s, 0, pyPDF * (1 - s)}
		}
		p.FillShading(l, box, sh, m, 1)
	}
}

// paints reports whether the box has anything to paint.
func (s *style) paints() bool {
	return s.bg != nil || s.hasBorder() || len(s.bgImages) > 0 || len(s.shadows) > 0 ||
		(s.outlineW > 0 && s.outlineStyle != "" && s.outlineStyle != "none")
}
