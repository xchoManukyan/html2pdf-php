package ttf

import (
	"bytes"
	"os"
	"testing"
)

func TestSystemFont(t *testing.T) {
	data, err := os.ReadFile(`C:\Windows\Fonts\arial.ttf`)
	if err != nil {
		t.Skip("arial.ttf not available")
	}
	f, err := Parse(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s / %s upm=%d glyphs=%d asc=%d desc=%d weight=%d", f.Family, f.Subfamily, f.UnitsPerEm, f.NumGlyphs, f.Ascent, f.Descent, f.Weight)
	for _, r := range "AԱբЖ€" {
		g := f.GlyphIndex(r)
		t.Logf("%c -> glyph %d advance %d", r, g, f.Advance(g))
	}
	if f.GlyphIndex('A') == 0 {
		t.Fatal("A missing")
	}
	sub := f.Subset(map[uint16]bool{f.GlyphIndex('A'): true, f.GlyphIndex('Ա'): true})
	t.Logf("subset %d bytes (full %d)", len(sub), len(data))
	if _, _, err := readDirectory(bytes.NewReader(sub), 0); err != nil {
		t.Fatal("subset is not a valid sfnt:", err)
	}
	if checksum(sub) != 0xB1B0AFBA {
		t.Errorf("bad whole-font checksum %x", checksum(sub))
	}
	if len(sub) > len(data)/4 {
		t.Error("subset too large")
	}
	info, err := ReadInfo(bytes.NewReader(data), 0)
	if err != nil || info.Family != "Arial" || !info.TrueType {
		t.Errorf("info: %+v %v", info, err)
	}
}
