package render

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"io"
	"regexp"
	"strings"
	"testing"
)

// pageTexts inflates every content stream and returns the text drawn with Tj.
func pageTexts(t *testing.T, pdf []byte) []string {
	t.Helper()
	re := regexp.MustCompile(`(?s)<< /Filter /FlateDecode /Length (\d+) >>\nstream\n`)
	var out []string
	for _, m := range re.FindAllSubmatchIndex(pdf, -1) {
		n := 0
		for _, c := range pdf[m[2]:m[3]] {
			n = n*10 + int(c-'0')
		}
		zr, err := zlib.NewReader(bytes.NewReader(pdf[m[1] : m[1]+n]))
		if err != nil {
			continue // image stream, not a content stream
		}
		data, _ := io.ReadAll(zr)
		var sb strings.Builder
		for _, tj := range regexp.MustCompile(`\((.*?)\) Tj`).FindAllSubmatch(data, -1) {
			sb.Write(tj[1])
			sb.WriteByte(' ')
		}
		out = append(out, sb.String())
	}
	return out
}

func TestConvertBasics(t *testing.T) {
	html := `<!doctype html><html><head><title>Test Doc</title>
<style>.big { font-size: 20px; color: #c00 } td { border: 1px solid #ccc }</style></head>
<body>
<h1>Hello &amp; welcome</h1>
<p class="big">Styled <b>bold</b> and <i>italic</i> text.</p>
<ul><li>One<li>Two</ul>
<ol start="3"><li>Three</li></ol>
<table><tr><th>Name</th><th>Price</th></tr><tr><td>Coffee</td><td>3.50 €</td></tr></table>
<script>alert("x")</script>
</body></html>`
	out, err := Convert(html, Options{PageNumbers: true, NoSystemFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-1.4")) || !bytes.HasSuffix(out, []byte("%%EOF\n")) {
		t.Fatal("not a PDF")
	}
	if !bytes.Contains(out, []byte("/Title (Test Doc)")) {
		t.Error("title missing")
	}
	text := strings.Join(pageTexts(t, out), " ")
	for _, want := range []string{"Hello", "&", "welcome", "bold", "One", "Two", "3.", "Coffee", `\200`, "Page 1 of 1"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in output text: %s", want, text)
		}
	}
	if strings.Contains(text, "alert") {
		t.Error("script content must not be rendered")
	}
}

func TestPagination(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 300; i++ {
		sb.WriteString("<p>Paragraph number with some text to fill the page.</p>")
	}
	sb.WriteString(`<div style="page-break-before: always">Last</div>`)
	out, err := Convert(sb.String(), Options{NoSystemFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`/Count (\d+)`).FindSubmatch(out)
	if m == nil || string(m[1]) == "1" {
		t.Fatalf("expected several pages, got %s", m)
	}
}

func TestEmbeddedImage(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(0, 0, color.NRGBA{255, 0, 0, 0}) // transparent pixel -> soft mask
	var buf bytes.Buffer
	png.Encode(&buf, img)
	src := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	out, err := Convert(`<p>Logo:</p><img src="`+src+`" width="100">`, Options{NoSystemFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("/Subtype /Image")) || !bytes.Contains(out, []byte("/SMask")) {
		t.Error("image or alpha mask not embedded")
	}
}

func TestMalformedHTML(t *testing.T) {
	inputs := []string{
		"<p>unclosed <b>bold <i>both",
		"</div></span>text<<>>",
		"<table><td>no row<td>cells</table>",
		"<ul><li><ul><li><ul><li>deep",
		strings.Repeat("<div>", 500) + "x",
		"averyveryveryverylongwordwithoutanyspaces" + strings.Repeat("x", 500),
	}
	for _, in := range inputs {
		if _, err := Convert(in, Options{}); err != nil {
			t.Errorf("%q: %v", in[:min(len(in), 30)], err)
		}
	}
}

func TestSystemFontsAndArmenian(t *testing.T) {
	html := `<html><head><style>
  :root { --brand: #2f6fed; --gap: 8px; }
  body { font-family: Arial, sans-serif; }
  h1 { color: var(--brand); margin-bottom: calc(var(--gap) * 2); }
  li:nth-child(odd) { font-weight: bold }
  .tag::before { content: "[" attr(data-x) "] "; color: gray }
  @media screen { p { display: none } }
  </style></head><body>
  <h1>Բարև, աշխարհ! Hello</h1>
  <p>Visible in print</p>
  <ul><li>Մեկ</li><li>Երկու</li><li>Երեք</li></ul>
  <span class="tag" data-x="42">tagged</span>
  </body></html>`
	out, err := Convert(html, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("/Subtype /Type0")) {
		t.Skip("no system TrueType fonts available")
	}
	if !bytes.Contains(out, []byte("/FontFile2")) || !bytes.Contains(out, []byte("/ToUnicode")) {
		t.Error("TrueType font not embedded with ToUnicode")
	}
	if len(out) > 400_000 {
		t.Errorf("PDF too large (%d bytes): subsetting not effective", len(out))
	}
}
