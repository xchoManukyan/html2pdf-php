package render

import (
	"encoding/base64"
	"os"
	"regexp"
	"strings"
	"testing"
)

func testSVG(body string) string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="120" viewBox="0 0 200 120">` + body + `</svg>`
}

func TestSVGMarkers(t *testing.T) {
	_, c := convertWith(t, testSVG(`<defs>
		<marker id="m" viewBox="0 0 10 10" refX="5" refY="5" markerWidth="4" markerHeight="4">
			<circle cx="5" cy="5" r="4" fill="#0000ff"/></marker></defs>
		<polyline points="10,10 50,60 90,10 130,60" fill="none" stroke="#000" marker-start="url(#m)" marker-mid="url(#m)" marker-end="url(#m)"/>`), Options{})
	if n := strings.Count(c, "0 0 1 rg"); n != 4 {
		t.Errorf("expected a marker on each of the 4 vertices, got %d", n)
	}
}

func TestSVGPattern(t *testing.T) {
	_, c := convertWith(t, testSVG(`<defs><pattern id="p" width="20" height="20" patternUnits="userSpaceOnUse">
		<circle cx="10" cy="10" r="5" fill="#00ff00"/></pattern></defs>
		<rect width="100" height="40" fill="url(#p)"/>`), Options{})
	// 100x40 area tiled by 20x20 cells: at least 5x2 tiles
	if n := strings.Count(c, "0 1 0 rg"); n < 10 {
		t.Errorf("pattern should be tiled across the shape, got %d tiles", n)
	}
	if strings.Count(c, " W n") == 0 {
		t.Error("pattern tiles should be clipped to the shape")
	}
}

func TestSVGMask(t *testing.T) {
	pdfOut, _ := convertWith(t, testSVG(`<defs>
		<linearGradient id="g"><stop offset="0" stop-color="white"/><stop offset="1" stop-color="black"/></linearGradient>
		<mask id="m"><rect width="200" height="120" fill="url(#g)"/></mask>
		<mask id="a" mask-type="alpha"><circle cx="50" cy="50" r="20"/></mask></defs>
		<rect width="200" height="120" fill="red" mask="url(#m)"/>
		<rect width="100" height="100" fill="blue" mask="url(#a)"/>`), Options{})
	if !strings.Contains(pdfOut, "/S /Luminosity") {
		t.Error("luminance mask should become a PDF luminosity soft mask")
	}
	if !strings.Contains(pdfOut, "/S /Alpha") {
		t.Error("mask-type=alpha should become a PDF alpha soft mask")
	}
}

func TestSVGFilters(t *testing.T) {
	_, c := convertWith(t, testSVG(`<defs>
		<filter id="gray"><feColorMatrix type="saturate" values="0"/></filter>
		<filter id="shadow"><feDropShadow dx="3" dy="3" stdDeviation="2" flood-color="#ff00ff"/></filter>
		<filter id="blur"><feGaussianBlur stdDeviation="3"/></filter></defs>
		<rect width="40" height="40" fill="#ff0000" filter="url(#gray)"/>
		<rect x="50" width="40" height="40" fill="#00ff00" filter="url(#shadow)"/>
		<circle cx="150" cy="40" r="20" fill="#0000ff" filter="url(#blur)"/>`), Options{})
	if strings.Contains(c, "1 0 0 rg") {
		t.Error("saturate(0) should remove the red")
	}
	if !regexp.MustCompile(`(0\.\d+) (0\.\d+) (0\.\d+) rg`).MatchString(c) {
		t.Error("grayscale fill expected")
	}
	if !strings.Contains(c, "1 0 1 rg") || !strings.Contains(c, "0 1 0 rg") {
		t.Error("drop shadow should paint the flood color and then the source")
	}
	if n := strings.Count(c, "0 0 1 rg"); n < 5 {
		t.Errorf("blur should be approximated by several offset copies, got %d", n)
	}
}

func TestWebPImages(t *testing.T) {
	var html strings.Builder
	for _, name := range []string{"lossless", "lossy", "alpha"} {
		data, err := os.ReadFile("../webp/testdata/" + name + ".webp")
		if err != nil {
			t.Fatal(err)
		}
		html.WriteString(`<img width="80" src="data:image/webp;base64,` + base64.StdEncoding.EncodeToString(data) + `">`)
	}
	pdfOut, _ := convertWith(t, html.String(), Options{})
	if n := strings.Count(pdfOut, "/Subtype /Image /Width 40 /Height 30 /ColorSpace /DeviceRGB"); n != 3 {
		t.Errorf("expected 3 decoded WebP images, got %d", n)
	}
	if n := strings.Count(pdfOut, "/SMask "); n < 2 {
		t.Errorf("transparent WebPs should carry a soft mask, got %d", n)
	}
}
