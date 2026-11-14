package render

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/png"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/xchoManukyan/html2pdf/converter/internal/pdf"
	"github.com/xchoManukyan/html2pdf/converter/internal/resource"
)

func pngBytes() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func convertWith(t *testing.T, html string, opt Options) (string, string) {
	t.Helper()
	opt.NoSystemFonts = true
	out, err := Convert(html, opt)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?s)<< /Filter /FlateDecode /Length (\d+) >>\nstream\n`)
	var content strings.Builder
	for _, m := range re.FindAllSubmatchIndex(out, -1) {
		n, _ := strconv.Atoi(string(out[m[2]:m[3]]))
		if zr, err := zlib.NewReader(bytes.NewReader(out[m[1] : m[1]+n])); err == nil {
			data, _ := io.ReadAll(zr)
			content.Write(data)
		}
	}
	return string(out), content.String()
}

func TestExternalStylesheetsAndImages(t *testing.T) {
	l := &resource.Loader{Files: map[string][]byte{
		"site/css/main.css":  []byte(`@import "extra.css"; p { color: #ff0000 } .bg { height: 40px; background: url(../img/bg.png) }`),
		"site/css/extra.css": []byte(`h1 { color: #0000ff }`),
		"site/img/bg.png":    pngBytes(),
		"site/img/logo.png":  pngBytes(),
	}}
	pdfOut, c := convertWith(t, `<html><head><link rel="stylesheet" href="css/main.css"></head>
		<body><h1>Title</h1><p>red</p><div class="bg"></div><img src="img/logo.png" width="40"></body></html>`,
		Options{Loader: l, BaseURL: "local:/site/index.html"})
	if !strings.Contains(c, "1 0 0 rg") || !strings.Contains(c, "0 0 1 rg") {
		t.Error("linked and @imported stylesheets should apply")
	}
	if n := strings.Count(pdfOut, "/Subtype /Image"); n < 2 {
		t.Errorf("relative <img> and CSS background url() should load from the bundle, got %d images", n)
	}
}

func TestSVG(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50" width="200">
		<defs><linearGradient id="g"><stop offset="0" stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient>
		<style>.c { fill: #00ff00 }</style></defs>
		<rect width="100" height="50" rx="5" fill="url(#g)"/>
		<circle class="c" cx="25" cy="25" r="10"/>
		<path d="M60 10 l20 0 a10 10 0 0 1 0 20 z" stroke="black" stroke-width="2" fill="none" stroke-dasharray="3 2"/>
		<g transform="rotate(45 50 25)"><polygon points="45,20 55,20 50,30"/></g>
		<text x="50" y="45" text-anchor="middle" font-size="8">SVG</text></svg>`
	pdfOut, c := convertWith(t, `<p>inline</p>`+svg+`<img src="data:image/svg+xml;base64,`+
		base64.StdEncoding.EncodeToString([]byte(svg))+`">`, Options{})
	if !strings.Contains(pdfOut, "/ShadingType 2") {
		t.Error("SVG linear gradient should become a PDF shading")
	}
	if !strings.Contains(c, "0 1 0 rg") {
		t.Error("SVG <style> class rule should color the circle green")
	}
	if !strings.Contains(c, "[3 2] 0 d") {
		t.Error("stroke-dasharray should be applied")
	}
	if strings.Count(c, "(SVG) Tj") != 2 {
		t.Error("SVG text should be drawn for both the inline and the <img> SVG")
	}
}

func TestPageCountersAndMarginBoxes(t *testing.T) {
	body := strings.Repeat("<p>filler text</p>", 140)
	p := textPositions(t, `<style>@page { @bottom-right { content: "Page " counter(page) " of " counter(pages) }
		@top-center { content: "Report" } }</style>`+body)
	pages := map[int]bool{}
	for _, f := range p["filler"] {
		pages[f.page] = true
	}
	n := len(pages)
	if n < 2 || len(p["Page"]) != n || len(p["Report"]) != n {
		t.Fatalf("margin boxes should appear on all %d pages: Page=%d Report=%d", n, len(p["Page"]), len(p["Report"]))
	}
	total := strconv.Itoa(n)
	if len(p[total]) < n {
		t.Errorf("counter(pages) should print %s on every page", total)
	}
	if two := p["2"]; len(two) == 0 || two[0].page != 2 {
		t.Errorf("counter(page) on page 2 should print 2: %v", two)
	}
}

func TestLinksMetadataAndEncryption(t *testing.T) {
	html := `<head><meta name="author" content="Ann Writer"><meta name="keywords" content="go, pdf"></head>
		<a href="#sec2">jump</a>` + strings.Repeat("<p>x</p>", 80) + `<h2 id="sec2">Second</h2>`
	out, _ := convertWith(t, html, Options{})
	if !strings.Contains(out, "/Dest [") {
		t.Error("#fragment links should become internal destinations")
	}
	if !strings.Contains(out, "/Author (Ann Writer)") || !strings.Contains(out, "/Keywords (go, pdf)") {
		t.Error("<meta> author/keywords should be written to the document info")
	}
	enc, _ := convertWith(t, `<p>Secret text</p>`, Options{Encrypt: &pdf.Encryption{UserPassword: "pw", AllowPrint: true}})
	if !strings.Contains(enc, "/Filter /Standard /V 2 /R 3 /Length 128") || !strings.Contains(enc, "/Encrypt ") {
		t.Error("encryption dictionary missing")
	}
	if strings.Contains(enc, "Secret") {
		t.Error("content must not be readable in an encrypted PDF")
	}
}

func bmp24(w, h int) []byte {
	stride := (w*3 + 3) &^ 3
	size := 54 + stride*h
	b := make([]byte, size)
	copy(b, "BM")
	binary.LittleEndian.PutUint32(b[2:], uint32(size))
	binary.LittleEndian.PutUint32(b[10:], 54)
	binary.LittleEndian.PutUint32(b[14:], 40)
	binary.LittleEndian.PutUint32(b[18:], uint32(w))
	binary.LittleEndian.PutUint32(b[22:], uint32(h))
	binary.LittleEndian.PutUint16(b[26:], 1)
	binary.LittleEndian.PutUint16(b[28:], 24)
	for i := 54; i < size; i++ {
		b[i] = 0x40
	}
	return b
}

func TestBMPListImageAndForms(t *testing.T) {
	bmp := "data:image/bmp;base64," + base64.StdEncoding.EncodeToString(bmp24(3, 2))
	icon := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes())
	out, c := convertWith(t, `<img src="`+bmp+`" width="30">
		<ul style="list-style-image: url(`+icon+`)"><li>item</li></ul>
		<form><input type="text" value="Jane"> <input type="checkbox" checked> <select><option>One</option><option selected>Two</option></select>
		<input type="submit" value="Send"> <input type="hidden" value="nope"></form>`, Options{})
	if strings.Count(out, "/Subtype /Image") < 2 {
		t.Error("BMP image and list-style-image should be embedded")
	}
	for _, want := range []string{"(Jane) Tj", "(Two", "(Send) Tj"} {
		if !strings.Contains(c, want) {
			t.Errorf("form control text %q missing", want)
		}
	}
	if strings.Contains(c, "nope") {
		t.Error("hidden inputs must not be printed")
	}
}

func TestTallImageContinuesAcrossPages(t *testing.T) {
	tall := image.NewNRGBA(image.Rect(0, 0, 100, 1200))
	for i := range tall.Pix {
		tall.Pix[i] = 200
		if i%4 == 3 {
			tall.Pix[i] = 255
		}
	}
	var b bytes.Buffer
	png.Encode(&b, tall)
	src := "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
	pdfOut, _ := convertWith(t, `<h1>Long</h1><img style="display:block;width:100%" src="`+src+`">`, Options{})
	pages := strings.Count(string(pdfOut), "/Type /Page ")
	if pages < 4 {
		t.Errorf("a 1:12 image at full width should run over several pages, got %d", pages)
	}
	if n := strings.Count(string(pdfOut), "/Subtype /Image"); n != 1 {
		t.Errorf("the image should be embedded once and reused on each page, got %d", n)
	}
	// inside a flex item it cannot break: it is scaled to one page instead
	pdfOut, _ = convertWith(t, `<div style="display:flex"><img style="display:block;width:100%" src="`+src+`"></div>`, Options{})
	if n := strings.Count(string(pdfOut), "/Type /Page "); n != 1 {
		t.Errorf("unbreakable context should keep the image on one page, got %d", n)
	}
}
