package css

import (
	"math"
	"strconv"
	"strings"
)

// Color is an RGBA color with components in 0..1.
type Color struct{ R, G, B, A float64 }

// The 148 CSS named colors.
var namedColors = map[string]uint32{
	"aliceblue": 0xf0f8ff, "antiquewhite": 0xfaebd7, "aqua": 0x00ffff, "aquamarine": 0x7fffd4,
	"azure": 0xf0ffff, "beige": 0xf5f5dc, "bisque": 0xffe4c4, "black": 0x000000,
	"blanchedalmond": 0xffebcd, "blue": 0x0000ff, "blueviolet": 0x8a2be2, "brown": 0xa52a2a,
	"burlywood": 0xdeb887, "cadetblue": 0x5f9ea0, "chartreuse": 0x7fff00, "chocolate": 0xd2691e,
	"coral": 0xff7f50, "cornflowerblue": 0x6495ed, "cornsilk": 0xfff8dc, "crimson": 0xdc143c,
	"cyan": 0x00ffff, "darkblue": 0x00008b, "darkcyan": 0x008b8b, "darkgoldenrod": 0xb8860b,
	"darkgray": 0xa9a9a9, "darkgreen": 0x006400, "darkgrey": 0xa9a9a9, "darkkhaki": 0xbdb76b,
	"darkmagenta": 0x8b008b, "darkolivegreen": 0x556b2f, "darkorange": 0xff8c00, "darkorchid": 0x9932cc,
	"darkred": 0x8b0000, "darksalmon": 0xe9967a, "darkseagreen": 0x8fbc8f, "darkslateblue": 0x483d8b,
	"darkslategray": 0x2f4f4f, "darkslategrey": 0x2f4f4f, "darkturquoise": 0x00ced1, "darkviolet": 0x9400d3,
	"deeppink": 0xff1493, "deepskyblue": 0x00bfff, "dimgray": 0x696969, "dimgrey": 0x696969,
	"dodgerblue": 0x1e90ff, "firebrick": 0xb22222, "floralwhite": 0xfffaf0, "forestgreen": 0x228b22,
	"fuchsia": 0xff00ff, "gainsboro": 0xdcdcdc, "ghostwhite": 0xf8f8ff, "gold": 0xffd700,
	"goldenrod": 0xdaa520, "gray": 0x808080, "green": 0x008000, "greenyellow": 0xadff2f,
	"grey": 0x808080, "honeydew": 0xf0fff0, "hotpink": 0xff69b4, "indianred": 0xcd5c5c,
	"indigo": 0x4b0082, "ivory": 0xfffff0, "khaki": 0xf0e68c, "lavender": 0xe6e6fa,
	"lavenderblush": 0xfff0f5, "lawngreen": 0x7cfc00, "lemonchiffon": 0xfffacd, "lightblue": 0xadd8e6,
	"lightcoral": 0xf08080, "lightcyan": 0xe0ffff, "lightgoldenrodyellow": 0xfafad2, "lightgray": 0xd3d3d3,
	"lightgreen": 0x90ee90, "lightgrey": 0xd3d3d3, "lightpink": 0xffb6c1, "lightsalmon": 0xffa07a,
	"lightseagreen": 0x20b2aa, "lightskyblue": 0x87cefa, "lightslategray": 0x778899, "lightslategrey": 0x778899,
	"lightsteelblue": 0xb0c4de, "lightyellow": 0xffffe0, "lime": 0x00ff00, "limegreen": 0x32cd32,
	"linen": 0xfaf0e6, "magenta": 0xff00ff, "maroon": 0x800000, "mediumaquamarine": 0x66cdaa,
	"mediumblue": 0x0000cd, "mediumorchid": 0xba55d3, "mediumpurple": 0x9370db, "mediumseagreen": 0x3cb371,
	"mediumslateblue": 0x7b68ee, "mediumspringgreen": 0x00fa9a, "mediumturquoise": 0x48d1cc, "mediumvioletred": 0xc71585,
	"midnightblue": 0x191970, "mintcream": 0xf5fffa, "mistyrose": 0xffe4e1, "moccasin": 0xffe4b5,
	"navajowhite": 0xffdead, "navy": 0x000080, "oldlace": 0xfdf5e6, "olive": 0x808000,
	"olivedrab": 0x6b8e23, "orange": 0xffa500, "orangered": 0xff4500, "orchid": 0xda70d6,
	"palegoldenrod": 0xeee8aa, "palegreen": 0x98fb98, "paleturquoise": 0xafeeee, "palevioletred": 0xdb7093,
	"papayawhip": 0xffefd5, "peachpuff": 0xffdab9, "peru": 0xcd853f, "pink": 0xffc0cb,
	"plum": 0xdda0dd, "powderblue": 0xb0e0e6, "purple": 0x800080, "rebeccapurple": 0x663399,
	"red": 0xff0000, "rosybrown": 0xbc8f8f, "royalblue": 0x4169e1, "saddlebrown": 0x8b4513,
	"salmon": 0xfa8072, "sandybrown": 0xf4a460, "seagreen": 0x2e8b57, "seashell": 0xfff5ee,
	"sienna": 0xa0522d, "silver": 0xc0c0c0, "skyblue": 0x87ceeb, "slateblue": 0x6a5acd,
	"slategray": 0x708090, "slategrey": 0x708090, "snow": 0xfffafa, "springgreen": 0x00ff7f,
	"steelblue": 0x4682b4, "tan": 0xd2b48c, "teal": 0x008080, "thistle": 0xd8bfd8,
	"tomato": 0xff6347, "turquoise": 0x40e0d0, "violet": 0xee82ee, "wheat": 0xf5deb3,
	"white": 0xffffff, "whitesmoke": 0xf5f5f5, "yellow": 0xffff00, "yellowgreen": 0x9acd32,
}

func rgb24(v uint32) Color {
	return Color{float64(v>>16&0xff) / 255, float64(v>>8&0xff) / 255, float64(v&0xff) / 255, 1}
}

// ParseColor parses hex (#rgb, #rgba, #rrggbb, #rrggbbaa), rgb()/rgba(),
// hsl()/hsla(), named colors and "transparent". "currentcolor" is not
// handled here (ok = false), the caller resolves it.
func ParseColor(s string) (Color, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if v, ok := namedColors[s]; ok {
		return rgb24(v), true
	}
	if s == "transparent" {
		return Color{0, 0, 0, 0}, true
	}
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) == 3 || len(h) == 4 {
			b := make([]byte, 0, 8)
			for i := 0; i < len(h); i++ {
				b = append(b, h[i], h[i])
			}
			h = string(b)
		}
		if len(h) != 6 && len(h) != 8 {
			return Color{}, false
		}
		v, err := strconv.ParseUint(h, 16, 64)
		if err != nil {
			return Color{}, false
		}
		if len(h) == 8 {
			c := rgb24(uint32(v >> 8))
			c.A = float64(v&0xff) / 255
			return c, true
		}
		return rgb24(uint32(v)), true
	}
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return Color{}, false
	}
	fn := s[:open]
	parts := strings.FieldsFunc(s[open+1:len(s)-1], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
	if len(parts) < 3 {
		return Color{}, false
	}
	alpha := 1.0
	if len(parts) >= 4 {
		alpha = numOrPercent(parts[3], 1)
	}
	switch fn {
	case "rgb", "rgba":
		var c [3]float64
		for i := 0; i < 3; i++ {
			c[i] = clamp01(numOrPercent(parts[i], 255) / 255) // "50%" -> 127.5 -> 0.5
		}
		return Color{c[0], c[1], c[2], clamp01(alpha)}, true
	case "hsl", "hsla":
		h, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSuffix(parts[0], "deg"), "turn"), 64)
		if strings.HasSuffix(parts[0], "turn") {
			h *= 360
		}
		sat := clamp01(numOrPercent(parts[1], 100) / 100)
		l := clamp01(numOrPercent(parts[2], 100) / 100)
		r, g, b := hslToRGB(h, sat, l)
		return Color{r, g, b, clamp01(alpha)}, true
	}
	return Color{}, false
}

// numOrPercent parses a number or a percentage of scale:
// "50%" -> 0.5*scale, "128" -> 128.
func numOrPercent(s string, scale float64) float64 {
	if strings.HasSuffix(s, "%") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		return v / 100 * scale
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func hslToRGB(h, s, l float64) (float64, float64, float64) {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return r + m, g + m, b + m
}

func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

// LengthCtx holds what relative units resolve against. All values are in
// points.
type LengthCtx struct {
	Em      float64 // current font size
	Rem     float64 // root font size
	Percent float64 // reference length for %
	VW, VH  float64 // viewport (page content area) size
}

var unitFactors = map[string]float64{
	"px": 0.75, "pt": 1, "pc": 12, "in": 72, "cm": 72 / 2.54, "mm": 72 / 25.4, "q": 72 / 101.6,
}

func (ctx LengthCtx) unit(u string) (float64, bool) {
	if f, ok := unitFactors[u]; ok {
		return f, true
	}
	switch u {
	case "em":
		return ctx.Em, true
	case "rem":
		return ctx.Rem, true
	case "ex", "ch":
		return ctx.Em / 2, true
	case "lh":
		return ctx.Em * 1.2, true
	case "%":
		return ctx.Percent / 100, true
	case "vw":
		return ctx.VW / 100, true
	case "vh":
		return ctx.VH / 100, true
	case "vmin":
		return math.Min(ctx.VW, ctx.VH) / 100, true
	case "vmax":
		return math.Max(ctx.VW, ctx.VH) / 100, true
	}
	return 0, false
}

// ParseLength converts a CSS length (including calc(), min(), max(),
// clamp()) to points. Unitless numbers are treated as px, like HTML
// attributes.
func ParseLength(s string, ctx LengthCtx) (float64, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "auto" || s == "none" {
		return 0, false
	}
	if strings.ContainsRune(s, '(') {
		c := &calcParser{s: s, ctx: ctx}
		v, ok := c.sum()
		c.ws()
		if !ok || c.i != len(c.s) {
			return 0, false
		}
		return v.v, true
	}
	num, unit := splitNumber(s)
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, false
	}
	if unit == "" {
		return v * 0.75, true
	}
	f, ok := ctx.unit(unit)
	if !ok {
		return 0, false
	}
	return v * f, true
}

func splitNumber(s string) (string, string) {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') && i+1 < len(s) && (s[i+1] >= '0' && s[i+1] <= '9' || s[i+1] == '-') {
		i += 2
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	return s[:i], s[i:]
}

type calcVal struct {
	v      float64
	length bool
}

type calcParser struct {
	s   string
	i   int
	ctx LengthCtx
}

func (c *calcParser) ws() {
	for c.i < len(c.s) && isWSByte(c.s[c.i]) {
		c.i++
	}
}

func (c *calcParser) sum() (calcVal, bool) {
	a, ok := c.product()
	if !ok {
		return a, false
	}
	for {
		c.ws()
		if c.i >= len(c.s) || (c.s[c.i] != '+' && c.s[c.i] != '-') {
			return a, true
		}
		op := c.s[c.i]
		c.i++
		b, ok := c.product()
		if !ok {
			return a, false
		}
		if op == '+' {
			a.v += b.v
		} else {
			a.v -= b.v
		}
		a.length = a.length || b.length
	}
}

func (c *calcParser) product() (calcVal, bool) {
	a, ok := c.unary()
	if !ok {
		return a, false
	}
	for {
		c.ws()
		if c.i >= len(c.s) || (c.s[c.i] != '*' && c.s[c.i] != '/') {
			return a, true
		}
		op := c.s[c.i]
		c.i++
		b, ok := c.unary()
		if !ok {
			return a, false
		}
		if op == '*' {
			a.v *= b.v
			a.length = a.length || b.length
		} else {
			if b.v == 0 {
				return a, false
			}
			a.v /= b.v
		}
	}
}

func (c *calcParser) unary() (calcVal, bool) {
	c.ws()
	if c.i < len(c.s) && c.s[c.i] == '-' && (c.i+1 >= len(c.s) || !(c.s[c.i+1] >= '0' && c.s[c.i+1] <= '9' || c.s[c.i+1] == '.')) {
		c.i++
		v, ok := c.unary()
		v.v = -v.v
		return v, ok
	}
	return c.primary()
}

func (c *calcParser) primary() (calcVal, bool) {
	c.ws()
	if c.i >= len(c.s) {
		return calcVal{}, false
	}
	if c.s[c.i] == '(' {
		c.i++
		v, ok := c.sum()
		c.ws()
		if !ok || c.i >= len(c.s) || c.s[c.i] != ')' {
			return v, false
		}
		c.i++
		return v, true
	}
	// function?
	j := c.i
	for j < len(c.s) && (c.s[j] >= 'a' && c.s[j] <= 'z' || c.s[j] == '-') {
		j++
	}
	if j < len(c.s) && c.s[j] == '(' && j > c.i {
		name := c.s[c.i:j]
		c.i = j + 1
		var args []calcVal
		for {
			v, ok := c.sum()
			if !ok {
				return v, false
			}
			args = append(args, v)
			c.ws()
			if c.i < len(c.s) && c.s[c.i] == ',' {
				c.i++
				continue
			}
			break
		}
		if c.i >= len(c.s) || c.s[c.i] != ')' {
			return calcVal{}, false
		}
		c.i++
		switch name {
		case "calc", "-webkit-calc":
			return args[0], len(args) == 1
		case "min", "max":
			r := args[0]
			for _, a := range args[1:] {
				if name == "min" && a.v < r.v || name == "max" && a.v > r.v {
					r = a
				}
			}
			return r, true
		case "clamp":
			if len(args) != 3 {
				return calcVal{}, false
			}
			return calcVal{math.Max(args[0].v, math.Min(args[1].v, args[2].v)), true}, true
		}
		return calcVal{}, false
	}
	// number with optional unit
	num, rest := splitNumber(c.s[c.i:])
	if num == "" || num == "+" || num == "-" {
		return calcVal{}, false
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return calcVal{}, false
	}
	c.i += len(num)
	k := 0
	for k < len(rest) && (rest[k] >= 'a' && rest[k] <= 'z' || rest[k] == '%') {
		k++
	}
	unit := rest[:k]
	c.i += k
	if unit == "" {
		return calcVal{v, false}, true
	}
	f, ok := c.ctx.unit(unit)
	if !ok {
		return calcVal{}, false
	}
	return calcVal{v * f, true}, true
}
