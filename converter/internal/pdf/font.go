package pdf

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/xchoManukyan/html2pdf/converter/internal/ttf"
)

// Font is a font usable on the pages of one document: either one of the
// built-in base-14 fonts or an embedded (subsetted) TrueType font.
type Font struct {
	id   int
	std  StdFont // valid when tt == nil
	tt   *ttf.Font
	used map[uint16]rune // glyph -> character, for subsetting and ToUnicode
	refd bool            // drawn at least once

	objID int // first PDF object number (set while writing)
}

// Std returns the built-in font f.
func (d *Document) Std(f StdFont) *Font {
	if d.stdFonts == nil {
		d.stdFonts = map[StdFont]*Font{}
	}
	if font, ok := d.stdFonts[f]; ok {
		return font
	}
	font := &Font{id: len(d.fonts) + 1, std: f}
	d.fonts = append(d.fonts, font)
	d.stdFonts[f] = font
	return font
}

// TrueType returns the document font for a parsed TrueType face.
func (d *Document) TrueType(t *ttf.Font) *Font {
	if d.ttFonts == nil {
		d.ttFonts = map[*ttf.Font]*Font{}
	}
	if font, ok := d.ttFonts[t]; ok {
		return font
	}
	font := &Font{id: len(d.fonts) + 1, tt: t, used: map[uint16]rune{}}
	d.fonts = append(d.fonts, font)
	d.ttFonts[t] = font
	return font
}

// IsTrueType reports whether the font is embedded.
func (f *Font) IsTrueType() bool { return f.tt != nil }

// HasRune reports whether the font can show r.
func (f *Font) HasRune(r rune) bool {
	if f.tt != nil {
		return f.tt.GlyphIndex(r) != 0
	}
	enc := Encode(string(r))
	return len(enc) == 1 && (enc[0] != '?' || r == '?')
}

// Width returns the advance width of s at the given size (points).
func (f *Font) Width(s string, size float64) float64 {
	if f.tt == nil {
		return TextWidth(f.std, size, s)
	}
	total := 0
	var prev uint16
	for i, r := range []rune(s) {
		g := f.tt.GlyphIndex(r)
		if i > 0 {
			total += f.tt.Kern(prev, g)
		}
		total += f.tt.Advance(g)
		prev = g
	}
	return float64(total) * size / float64(f.tt.UnitsPerEm)
}

// Metrics returns ascent, descent (positive) and line gap per 1pt of size.
func (f *Font) Metrics() (ascent, descent, gap float64) {
	if f.tt == nil {
		if f.std >= Courier {
			return 0.629, 0.157, 0.364 // Courier: 1.15 "normal" line height
		}
		return 0.718, 0.207, 0.225
	}
	u := float64(f.tt.UnitsPerEm)
	return float64(f.tt.Ascent) / u, float64(-f.tt.Descent) / u, float64(f.tt.LineGap) / u
}

// show returns the text-showing operator for s: "<...> Tj", or a TJ array
// when kerning moves glyphs closer together or apart.
func (f *Font) show(s string) string {
	if f.tt == nil {
		return f.encode(s) + " Tj"
	}
	f.refd = true
	var sb strings.Builder
	sb.WriteString("[<")
	kerned := false
	var prev uint16
	for i, r := range []rune(s) {
		g := f.tt.GlyphIndex(r)
		if _, ok := f.used[g]; !ok && g != 0 {
			f.used[g] = r
		}
		if i > 0 {
			if k := f.tt.Kern(prev, g); k != 0 {
				// TJ numbers are thousandths of an em, positive = move left
				fmt.Fprintf(&sb, "> %s <", num(-float64(k)*1000/float64(f.tt.UnitsPerEm)))
				kerned = true
			}
		}
		fmt.Fprintf(&sb, "%04X", g)
		prev = g
	}
	if !kerned {
		return f.encode(s) + " Tj"
	}
	sb.WriteString(">] TJ")
	return sb.String()
}

// encode returns the PDF string operand for s and records glyph usage.
func (f *Font) encode(s string) string {
	f.refd = true
	if f.tt == nil {
		return escape(Encode(s))
	}
	var sb strings.Builder
	sb.WriteByte('<')
	for _, r := range s {
		g := f.tt.GlyphIndex(r)
		if _, ok := f.used[g]; !ok && g != 0 {
			f.used[g] = r
		}
		fmt.Fprintf(&sb, "%04X", g)
	}
	sb.WriteByte('>')
	return sb.String()
}

func (f *Font) objectCount() int {
	if f.tt == nil {
		return 1
	}
	return 5
}

func pdfName(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r > 32 && r < 127 && !strings.ContainsRune("()<>[]{}/%#", r) {
			sb.WriteRune(r)
		}
	}
	if sb.Len() == 0 {
		return "Font"
	}
	return sb.String()
}

func (f *Font) write(w *writer) {
	if f.tt == nil {
		w.object(f.objID, fmt.Sprintf(
			"<< /Type /Font /Subtype /Type1 /BaseFont /%s /Encoding /WinAnsiEncoding >>", stdNames[f.std]))
		return
	}
	t := f.tt
	glyphs := make([]int, 0, len(f.used))
	keep := map[uint16]bool{}
	for g := range f.used {
		glyphs = append(glyphs, int(g))
		keep[g] = true
	}
	sort.Ints(glyphs)

	// Subset tag: six letters derived from the glyph set.
	h := fnv.New32a()
	fmt.Fprint(h, t.PostScriptName, glyphs)
	sum := h.Sum32()
	tag := make([]byte, 6)
	for i := range tag {
		tag[i] = 'A' + byte(sum%26)
		sum /= 26
	}
	ps := t.PostScriptName
	if ps == "" {
		ps = t.Family
	}
	base := string(tag) + "+" + pdfName(ps)

	scale := 1000 / float64(t.UnitsPerEm)
	var widths strings.Builder
	for _, g := range glyphs {
		fmt.Fprintf(&widths, "%d [%s] ", g, num(float64(t.Advance(uint16(g)))*scale))
	}
	type0, cid, desc, file, toUni := f.objID, f.objID+1, f.objID+2, f.objID+3, f.objID+4

	w.object(type0, fmt.Sprintf("<< /Type /Font /Subtype /Type0 /BaseFont /%s /Encoding /Identity-H /DescendantFonts [%d 0 R] /ToUnicode %d 0 R >>",
		base, cid, toUni))
	w.object(cid, fmt.Sprintf("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /%s /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor %d 0 R /DW %s /W [ %s] /CIDToGIDMap /Identity >>",
		base, desc, num(float64(t.Advance(0))*scale), widths.String()))
	flags := 32 // nonsymbolic
	if t.FixedPitch {
		flags |= 1
	}
	if t.Italic {
		flags |= 64
	}
	stemV := 80
	if t.Weight >= 600 {
		stemV = 140
	}
	w.object(desc, fmt.Sprintf("<< /Type /FontDescriptor /FontName /%s /Flags %d /FontBBox [%s %s %s %s] /ItalicAngle %s /Ascent %s /Descent %s /CapHeight %s /StemV %d /FontFile2 %d 0 R >>",
		base, flags, num(float64(t.BBox[0])*scale), num(float64(t.BBox[1])*scale), num(float64(t.BBox[2])*scale), num(float64(t.BBox[3])*scale),
		num(t.ItalicAngle), num(float64(t.Ascent)*scale), num(float64(t.Descent)*scale), num(float64(t.CapHeight)*scale), stemV, file))
	sub := t.Subset(keep)
	w.stream(file, fmt.Sprintf("/Filter /FlateDecode /Length1 %d", len(sub)), deflate(sub))
	w.stream(toUni, "/Filter /FlateDecode", deflate([]byte(f.toUnicode(glyphs))))
}

func (f *Font) toUnicode(glyphs []int) string {
	var sb strings.Builder
	sb.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n" +
		"/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	for i := 0; i < len(glyphs); i += 100 {
		end := min(i+100, len(glyphs))
		fmt.Fprintf(&sb, "%d beginbfchar\n", end-i)
		for _, g := range glyphs[i:end] {
			fmt.Fprintf(&sb, "<%04X> <", g)
			for _, u := range utf16.Encode([]rune{f.used[uint16(g)]}) {
				fmt.Fprintf(&sb, "%04X", u)
			}
			sb.WriteString(">\n")
		}
		sb.WriteString("endbfchar\n")
	}
	sb.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	return sb.String()
}

// TextStyle describes how a run of text is painted.
type TextStyle struct {
	Font        *Font
	Size        float64
	Color       Color
	FakeBold    bool    // emulate bold by stroking the outline
	FakeItalic  bool    // emulate italic by slanting
	CharSpacing float64 // extra space after every character (points)
}

// pdfText encodes a UTF-16BE text string for document metadata.
func pdfText(s string) string {
	ascii := true
	for _, r := range s {
		if r > 126 {
			ascii = false
			break
		}
	}
	if ascii {
		return escape([]byte(s))
	}
	var sb strings.Builder
	sb.WriteString("<FEFF")
	for _, u := range utf16.Encode([]rune(s)) {
		fmt.Fprintf(&sb, "%04X", u)
	}
	sb.WriteByte('>')
	return sb.String()
}
