package pdf

// StdFont is one of the PDF base-14 fonts that every viewer has built in, so
// no font files need to be embedded.
type StdFont int

const (
	Helvetica StdFont = iota
	HelveticaBold
	HelveticaOblique
	HelveticaBoldOblique
	Courier
	CourierBold
	CourierOblique
	CourierBoldOblique
	fontCount
)

var stdNames = [fontCount]string{
	"Helvetica", "Helvetica-Bold", "Helvetica-Oblique", "Helvetica-BoldOblique",
	"Courier", "Courier-Bold", "Courier-Oblique", "Courier-BoldOblique",
}

// PickStd returns the built-in font for a family/weight/style combination.
func PickStd(mono, bold, italic bool) StdFont {
	f := Helvetica
	if mono {
		f = Courier
	}
	switch {
	case bold && italic:
		f += 3
	case bold:
		f += 1
	case italic:
		f += 2
	}
	return f
}

func (f StdFont) isBold() bool { return f == HelveticaBold || f == HelveticaBoldOblique }
func (f StdFont) isMono() bool { return f >= Courier }

// Glyph widths (1/1000 em) of ASCII 32..126, from the Adobe AFM files.
var helveticaWidths = [95]int{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278, // space ... /
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, // 0-9
	278, 278, 584, 584, 584, 556, 1015, // : ; < = > ? @
	667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, // A-M
	722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, // N-Z
	278, 278, 278, 469, 556, 333, // [ \ ] ^ _ `
	556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, // a-m
	556, 556, 556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, // n-z
	334, 260, 334, 584, // { | } ~
}

var helveticaBoldWidths = [95]int{
	278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556,
	333, 333, 584, 584, 584, 611, 975,
	722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833,
	722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611,
	333, 278, 333, 584, 556, 333,
	556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889,
	611, 611, 611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500,
	389, 280, 389, 584,
}

// Widths of the WinAnsi bytes 0x80..0xFF that are not plain letters.
var extraWidths = map[byte][2]int{ // regular, bold
	0x80: {556, 556}, 0x82: {222, 278}, 0x83: {556, 556}, 0x84: {333, 500}, 0x85: {1000, 1000},
	0x86: {556, 556}, 0x87: {556, 556}, 0x88: {333, 333}, 0x89: {1000, 1000}, 0x8B: {333, 333},
	0x8C: {1000, 1000}, 0x91: {222, 278}, 0x92: {222, 278}, 0x93: {333, 500}, 0x94: {333, 500},
	0x95: {350, 350}, 0x96: {556, 556}, 0x97: {1000, 1000}, 0x98: {333, 333}, 0x99: {1000, 1000},
	0x9B: {333, 333}, 0x9C: {944, 944}, 0xA0: {278, 278}, 0xA1: {333, 333}, 0xA6: {260, 280},
	0xA8: {333, 333}, 0xA9: {737, 737}, 0xAA: {370, 370}, 0xAB: {556, 556}, 0xAC: {584, 584},
	0xAD: {333, 333}, 0xAE: {737, 737}, 0xAF: {333, 333}, 0xB0: {400, 400}, 0xB1: {584, 584},
	0xB2: {333, 333}, 0xB3: {333, 333}, 0xB4: {333, 333}, 0xB5: {556, 611}, 0xB6: {537, 556},
	0xB7: {278, 278}, 0xB8: {333, 333}, 0xB9: {333, 333}, 0xBA: {365, 365}, 0xBB: {556, 556},
	0xBC: {834, 834}, 0xBD: {834, 834}, 0xBE: {834, 834}, 0xBF: {611, 611},
	0xC6: {1000, 1000}, 0xD7: {584, 584}, 0xDF: {611, 611}, 0xE6: {889, 889}, 0xF7: {584, 584},
	0xEC: {278, 278}, 0xED: {278, 278}, 0xEE: {278, 278}, 0xEF: {278, 278},
}

// Accented Latin-1 letters (0xC0..0xFF) take the width of their base letter.
const latinBase = "AAAAAA?CEEEEIIIIDNOOOOO?OUUUUYP?aaaaaa?ceeeeiiiionooooo?ouuuuypy"

func glyphWidth(f StdFont, b byte) int {
	if f.isMono() {
		return 600
	}
	bold := 0
	table := &helveticaWidths
	if f.isBold() {
		bold = 1
		table = &helveticaBoldWidths
	}
	if b >= 32 && b <= 126 {
		return table[b-32]
	}
	if w, ok := extraWidths[b]; ok {
		return w[bold]
	}
	if b >= 0xC0 {
		base := latinBase[b-0xC0]
		if base != '?' {
			return table[base-32]
		}
	}
	if b == 0x8A || b == 0x9A || b == 0x8E || b == 0x9E || b == 0x9F {
		// Š š Ž ž Ÿ
		return map[byte]int{0x8A: 667, 0x9A: 500, 0x8E: 611, 0x9E: 500, 0x9F: 667}[b]
	}
	return 556
}

// TextWidth returns the width in points of s set in font f at size pt.
func TextWidth(f StdFont, size float64, s string) float64 {
	total := 0
	for _, b := range Encode(s) {
		total += glyphWidth(f, b)
	}
	return float64(total) * size / 1000
}

var cp1252 = map[rune]byte{
	'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84, '…': 0x85, '†': 0x86, '‡': 0x87, 'ˆ': 0x88,
	'‰': 0x89, 'Š': 0x8A, '‹': 0x8B, 'Œ': 0x8C, 'Ž': 0x8E, '‘': 0x91, '’': 0x92, '“': 0x93,
	'”': 0x94, '•': 0x95, '–': 0x96, '—': 0x97, '˜': 0x98, '™': 0x99, 'š': 0x9A, '›': 0x9B,
	'œ': 0x9C, 'ž': 0x9E, 'Ÿ': 0x9F,
	// Fallbacks for common characters the standard fonts cannot show.
	'‐': '-', '‑': '-', '‒': '-', '−': '-', '′': '\'', '″': '"',
	'←': '<', '→': '>', '●': 0x95, '▪': 0x95, '✓': 'v',
	'✔': 'v', '✕': 'x', '✗': 'x',
}

// Encode converts UTF-8 text to WinAnsi (Windows-1252) bytes, the encoding
// used for the standard fonts. Characters outside it become '?'.
func Encode(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			out = append(out, ' ')
		case r < 0x20:
			// control characters are dropped
		case r < 0x80:
			out = append(out, byte(r))
		case r >= 0xA0 && r <= 0xFF:
			out = append(out, byte(r))
		case r >= 0x2000 && r <= 0x200A, r == 0x202F, r == 0x205F, r == 0x3000:
			out = append(out, ' ')
		case r == 0x200B || r == 0x200C || r == 0x200D || r == 0xFEFF || r == 0x2060:
			// zero-width characters are dropped
		default:
			if b, ok := cp1252[r]; ok {
				out = append(out, b)
			} else {
				out = append(out, '?')
			}
		}
	}
	return out
}
