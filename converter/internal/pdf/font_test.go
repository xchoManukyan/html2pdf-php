package pdf

import (
	"os"
	"strings"
	"testing"

	"github.com/xchoManukyan/html2pdf/converter/internal/ttf"
)

// systemFont loads a font shipped with the OS, or skips the test.
func systemFont(t *testing.T) *ttf.Font {
	for _, p := range []string{
		"C:/Windows/Fonts/arial.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
		"/System/Library/Fonts/Supplemental/Arial.ttf",
	} {
		if data, err := os.ReadFile(p); err == nil {
			if f, err := ttf.Parse(data, 0); err == nil {
				return f
			}
		}
	}
	t.Skip("no system TrueType font found")
	return nil
}

func TestKerning(t *testing.T) {
	tt := systemFont(t)
	doc := New(595, 842)
	f := doc.TrueType(tt)
	if tt.Kern(tt.GlyphIndex('A'), tt.GlyphIndex('V')) >= 0 {
		t.Skip("font has no AV kerning")
	}
	sep := f.Width("A", 12) + f.Width("V", 12)
	if pair := f.Width("AV", 12); pair >= sep-0.01 {
		t.Errorf("kerned width %.3f should be below %.3f", pair, sep)
	}
	if op := f.show("AVA"); !strings.HasSuffix(op, "] TJ") || !strings.Contains(op, "> ") {
		t.Errorf("kerned text should use a TJ array, got %q", op)
	}
	if op := f.show("ab"); !strings.HasSuffix(op, "> Tj") {
		t.Errorf("unkerned text should stay a plain Tj, got %q", op)
	}
}
