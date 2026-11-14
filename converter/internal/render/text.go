package render

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/xchoManukyan/html2pdf/converter/internal/fonts"
	"github.com/xchoManukyan/html2pdf/converter/internal/pdf"
)

// chainFont is one font of a fallback chain, with synthesis flags for
// weights/styles the installed face lacks.
type chainFont struct {
	f                    *pdf.Font
	fakeBold, fakeItalic bool
}

// fontChain is the ordered list of fonts used for one CSS font request:
// the font-family list, then system fallbacks, then a built-in PDF font.
type fontChain struct {
	e      *engine
	fonts  []chainFont
	weight int
	italic bool
	byRune map[rune]int
}

// parseFamilies splits a font-family value into names.
func parseFamilies(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		name := strings.TrimSpace(part)
		name = strings.Trim(name, `"'`)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func isMonoFamily(v string) bool {
	v = strings.ToLower(v)
	for _, m := range []string{"mono", "courier", "consolas", "menlo", "monaco", "code"} {
		if strings.Contains(v, m) {
			return true
		}
	}
	return false
}

func (e *engine) lookupFace(name string, weight int, italic bool) *fonts.Face {
	if f := e.docFonts.Match(name, weight, italic); f != nil {
		return f
	}
	return e.sysFonts.Match(name, weight, italic)
}

func (e *engine) chain(st *style) *fontChain {
	key := st.family + "|" + strconv.Itoa(st.weight) + "|" + strconv.FormatBool(st.italic)
	if c, ok := e.chains[key]; ok {
		return c
	}
	c := &fontChain{e: e, weight: st.weight, italic: st.italic, byRune: map[rune]int{}}
	seen := map[*fonts.Face]bool{}
	add := func(face *fonts.Face) bool {
		if face == nil || seen[face] {
			return false
		}
		t, err := face.Load()
		if err != nil {
			return false
		}
		seen[face] = true
		c.fonts = append(c.fonts, chainFont{
			f:          e.doc.TrueType(t),
			fakeBold:   st.weight >= 600 && face.Weight < 600,
			fakeItalic: st.italic && !face.Italic,
		})
		return true
	}
	families := append(parseFamilies(st.family), "serif")
	for _, fam := range families {
		for _, name := range fonts.Expand(fam) {
			if add(e.lookupFace(name, st.weight, st.italic)) {
				break
			}
		}
	}
	for _, name := range fonts.FallbackFamilies {
		add(e.lookupFace(name, st.weight, st.italic))
	}
	mono := isMonoFamily(st.family)
	std := pdf.PickStd(mono, st.weight >= 600, st.italic)
	c.fonts = append(c.fonts, chainFont{f: e.doc.Std(std)})
	e.chains[key] = c
	return c
}

func ignorable(r rune) bool {
	return r >= 0x200B && r <= 0x200F || r >= 0xFE00 && r <= 0xFE0F || r == 0xFEFF || r == 0x2060 || r == 0xAD
}

// pick returns the index of the first font that can show r.
func (c *fontChain) pick(r rune) int {
	if i, ok := c.byRune[r]; ok {
		return i
	}
	idx := len(c.fonts) - 1
	found := false
	for i, cf := range c.fonts {
		if cf.f.HasRune(r) {
			idx, found = i, true
			break
		}
	}
	if !found && r > 0x7F {
		// Search every installed font for this character.
		if face := c.e.sysFonts.Fallback(r); face != nil {
			if t, err := face.Load(); err == nil {
				cf := chainFont{f: c.e.doc.TrueType(t), fakeBold: c.weight >= 600 && face.Weight < 600,
					fakeItalic: c.italic && !face.Italic}
				last := c.fonts[len(c.fonts)-1]
				c.fonts = append(c.fonts[:len(c.fonts)-1], cf, last)
				idx = len(c.fonts) - 2
				for k, v := range c.byRune { // the std font moved one slot down
					if v == len(c.fonts)-2 {
						c.byRune[k] = len(c.fonts) - 1
					}
				}
			}
		}
	}
	c.byRune[r] = idx
	return idx
}

type textSeg struct {
	cf   chainFont
	text string
}

// segments splits s into runs that each use a single font.
func (c *fontChain) segments(s string) []textSeg {
	var segs []textSeg
	cur := -1
	var sb strings.Builder
	for _, r := range s {
		if ignorable(r) {
			continue
		}
		i := c.pick(r)
		if i != cur && sb.Len() > 0 {
			segs = append(segs, textSeg{c.fonts[cur], sb.String()})
			sb.Reset()
		}
		cur = i
		sb.WriteRune(r)
	}
	if sb.Len() > 0 {
		segs = append(segs, textSeg{c.fonts[cur], sb.String()})
	}
	return segs
}

// textWidth measures s in the style's font chain, with letter spacing.
func (e *engine) textWidth(st *style, s string) float64 {
	if s == "" {
		return 0
	}
	w := 0.0
	for _, seg := range e.chain(st).segments(s) {
		w += seg.cf.f.Width(seg.text, st.size)
	}
	if st.letterSpacing != 0 {
		w += st.letterSpacing * float64(utf8.RuneCountInString(s))
	}
	return w
}

func (e *engine) spaceWidth(st *style) float64 {
	return e.textWidth(st, " ") + st.wordSpacing
}

// drawText paints s with its baseline at y; returns the advance.
func (e *engine) drawText(p *pdf.Page, x, baseline float64, st *style, s string) float64 {
	segs := e.chain(st).segments(s)
	if sh := st.textShadow; sh != nil && st.visible {
		c := sh.color
		if sh.blur > 0 {
			c.A = c.Alpha() * 0.6 // no blur in PDF: a softer, solid shadow
		}
		e.drawSegs(p, segs, x+sh.x, baseline+sh.y, st, st.paint(c))
	}
	return e.drawSegs(p, segs, x, baseline, st, st.paint(st.color))
}

func (e *engine) drawSegs(p *pdf.Page, segs []textSeg, x, baseline float64, st *style, c pdf.Color) float64 {
	start := x
	for _, seg := range segs {
		if st.visible {
			p.Text(x, baseline, pdf.TextStyle{
				Font: seg.cf.f, Size: st.size, Color: c,
				FakeBold: seg.cf.fakeBold, FakeItalic: seg.cf.fakeItalic, CharSpacing: st.letterSpacing,
			}, seg.text)
		}
		x += seg.cf.f.Width(seg.text, st.size) + st.letterSpacing*float64(utf8.RuneCountInString(seg.text))
	}
	return x - start
}

// vmetrics returns the space an inline box of this style needs above and
// below the baseline (CSS line-height model: half-leading on each side).
func (e *engine) vmetrics(st *style) (above, below float64) {
	asc, desc, gap := e.chain(st).fonts[0].f.Metrics()
	content := (asc + desc) * st.size
	lh := st.lineHeight * st.size
	if st.lineHeight <= 0 { // "normal"
		lh = (asc + desc + gap) * st.size
	}
	half := (lh - content) / 2
	above = asc*st.size + half
	below = lh - above
	return
}
