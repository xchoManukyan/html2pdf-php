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
	"strconv"
	"strings"
	"testing"
)

// renderRaw returns the whole PDF and its inflated content streams.
func renderRaw(t *testing.T, html string) (string, string) {
	t.Helper()
	out, err := Convert(html, Options{NoSystemFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?s)<< /Filter /FlateDecode /Length (\d+) >>\nstream\n`)
	var content strings.Builder
	for _, m := range re.FindAllSubmatchIndex(out, -1) {
		n, _ := strconv.Atoi(string(out[m[2]:m[3]]))
		zr, err := zlib.NewReader(bytes.NewReader(out[m[1] : m[1]+n]))
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(zr)
		content.Write(data)
	}
	return string(out), content.String()
}

func TestBorderStylesAndRadius(t *testing.T) {
	_, c := renderRaw(t, `
		<div style="border: 2px dashed red; border-radius: 8px">dashed rounded</div>
		<div style="border: 2px dotted blue">dotted</div>
		<div style="border: 6px double green">double</div>
		<div style="border-bottom: 3px groove #888">groove</div>`)
	if !strings.Contains(c, "[4.5 4.5] 0 d") {
		t.Error("dashed border should use a 3w dash pattern")
	}
	if !strings.Contains(c, "[0 3] 0 d 1 J") {
		t.Error("dotted border should use round-capped zero-length dashes")
	}
	if strings.Count(c, " c ") < 4 {
		t.Error("rounded corners should produce Bezier curves")
	}
	if strings.Count(c, " 1.5 w ") < 2 { // double 6px = 4.5pt: two 1.5pt strokes
		t.Errorf("double border should draw two thin strokes")
	}
}

func TestOpacityAndRGBA(t *testing.T) {
	pdf, c := renderRaw(t, `<div style="opacity:.5">half <span style="color: rgba(255,0,0,.5)">quarter</span></div>
		<div style="background: rgba(0,0,255,.2)">tinted</div>`)
	for _, want := range []string{"/ca 0.5 ", "/ca 0.25 ", "/ca 0.2 "} {
		if !strings.Contains(pdf, want) {
			t.Errorf("missing ExtGState %q", want)
		}
	}
	if !strings.Contains(c, "/GS") {
		t.Error("content should select graphics states")
	}
}

func TestGradientsAndShadows(t *testing.T) {
	pdf, c := renderRaw(t, `
		<div style="height:40px; background: linear-gradient(to right, red, yellow 50%, blue)">lin</div>
		<div style="height:40px; background: radial-gradient(circle at top left, #fff, #000)">rad</div>
		<div style="height:40px; background: repeating-linear-gradient(45deg, #eee 0 10px, #ccc 10px 20px)">rep</div>
		<div style="box-shadow: 0 4px 12px rgba(0,0,0,.3); padding: 8px">shadow</div>
		<div style="box-shadow: inset 0 0 0 3px red">inset</div>
		<p style="text-shadow: 1px 1px 0 gray">text shadow</p>`)
	if !strings.Contains(pdf, "/ShadingType 2") || !strings.Contains(pdf, "/ShadingType 3") {
		t.Error("linear and radial shadings expected")
	}
	if !strings.Contains(pdf, "/FunctionType 3") {
		t.Error("multi-stop gradient should use a stitching function")
	}
	if strings.Count(c, " sh Q") < 3 {
		t.Error("gradients should be painted with sh")
	}
	if strings.Count(c, "re f Q") < 4 {
		t.Error("blurred shadow should be drawn as several layers")
	}
	if !strings.Contains(c, "f* Q") {
		t.Error("inset shadow should use an even-odd frame")
	}
	if strings.Count(c, "(text) Tj") < 2 {
		t.Error("text-shadow should draw the text twice")
	}
}

func TestBackgroundImageAndClip(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	img.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	png.Encode(&buf, img)
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	_, c := renderRaw(t, `
		<div style="height:60px; background: url(`+uri+`) repeat">tiled</div>
		<div style="height:60px; background: #eee url('`+uri+`') no-repeat center / 30px">single</div>
		<div style="height:30px; overflow:hidden; border-radius: 10px; background:#ddd">`+strings.Repeat("clipped text ", 40)+`</div>
		<img src="`+uri+`" style="width:40px; border-radius:50%">`)
	if n := strings.Count(c, "/Im1 Do"); n < 20 {
		t.Errorf("repeating background should tile the image, got %d draws", n)
	}
	if !strings.Contains(c, "W n") {
		t.Error("overflow: hidden / rounded image should clip")
	}
}
