package render

import (
	"bytes"
	"compress/zlib"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type placedText struct {
	x, y float64 // PDF coordinates (y up)
	page int
}

// textPositions renders html with the built-in fonts and returns where each
// text run was drawn.
func textPositions(t *testing.T, html string) map[string][]placedText {
	t.Helper()
	out, err := Convert(html, Options{NoSystemFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?s)<< /Filter /FlateDecode /Length (\d+) >>\nstream\n`)
	tj := regexp.MustCompile(`([\d.\-]+) ([\d.\-]+) Td \((.*?)\) Tj`)
	res := map[string][]placedText{}
	page := 0
	for _, m := range re.FindAllSubmatchIndex(out, -1) {
		n, _ := strconv.Atoi(string(out[m[2]:m[3]]))
		zr, err := zlib.NewReader(bytes.NewReader(out[m[1] : m[1]+n]))
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(zr)
		if !bytes.Contains(data, []byte("BT")) && !bytes.Contains(data, []byte(" re f")) {
			continue
		}
		page++
		for _, s := range tj.FindAllSubmatch(data, -1) {
			x, _ := strconv.ParseFloat(string(s[1]), 64)
			y, _ := strconv.ParseFloat(string(s[2]), 64)
			res[string(s[3])] = append(res[string(s[3])], placedText{x, y, page})
		}
	}
	return res
}

const left, contentW = 42.52, 510.24 // A4 with 15 mm margins

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestFlexJustify(t *testing.T) {
	p := textPositions(t, `<div style="display:flex; justify-content:space-between; font: 12px Helvetica">
		<span>AAA</span><span>BBB</span><span>CCC</span></div>`)
	a, c := p["AAA"][0], p["CCC"][0]
	if !near(a.x, left, 0.5) {
		t.Errorf("first item at %.2f, want %.2f", a.x, left)
	}
	cw := 3 * 0.722 * 9 // "CCC" in 12px (=9pt) Helvetica
	if !near(c.x+cw, left+contentW, 1) {
		t.Errorf("last item ends at %.2f, want %.2f", c.x+cw, left+contentW)
	}
	b := p["BBB"][0]
	if b.y != a.y || c.y != a.y {
		t.Error("items should share one line")
	}
}

func TestFlexGrowAndGap(t *testing.T) {
	p := textPositions(t, `<div style="display:flex; gap: 20px">
		<div style="flex:1">one</div><div style="flex:2">two</div></div>`)
	gap := 15.0 // 20px
	unit := (contentW - gap) / 3
	if x := p["two"][0].x; !near(x, left+unit+gap, 0.5) {
		t.Errorf("second item at %.2f, want %.2f", x, left+unit+gap)
	}
}

func TestFlexAlignCenterAndWrap(t *testing.T) {
	p := textPositions(t, `<div style="display:flex; align-items:center">
		<div style="height:200px">tall</div><div>mid</div></div>
		<div style="display:flex; flex-wrap:wrap">
		<div style="width:40%">w1</div><div style="width:40%">w2</div><div style="width:40%">w3</div></div>`)
	tall, mid := p["tall"][0], p["mid"][0]
	// tall item text at top of a 150pt line, mid centred: ~ 75pt lower minus half a line
	if d := tall.y - mid.y; d < 55 || d > 75 {
		t.Errorf("centred item offset %.1f, want ~65", d)
	}
	w1, w2, w3 := p["w1"][0], p["w2"][0], p["w3"][0]
	if w1.y != w2.y || w3.y >= w1.y || !near(w3.x, left, 0.5) {
		t.Errorf("wrap: w1=%v w2=%v w3=%v", w1, w2, w3)
	}
}

func TestFlexColumnAutoMargin(t *testing.T) {
	p := textPositions(t, `<div style="display:flex; flex-direction:column; height:300px">
		<div>top</div><div style="margin-top:auto">bottom</div></div>`)
	if d := p["top"][0].y - p["bottom"][0].y; !near(d, 225-13.8, 3) {
		t.Errorf("footer pushed by %.1f, want ~211", d)
	}
}

func TestInlineBlockAndAutoMargins(t *testing.T) {
	p := textPositions(t, `<p><span style="display:inline-block; width:100px">ib1</span><span style="display:inline-block; width:100px">ib2</span></p>
		<div style="width:200px; margin: 0 auto">centred</div>
		<div style="box-sizing:border-box; width:200px; padding-left:100px">padded</div>`)
	ib1, ib2 := p["ib1"][0], p["ib2"][0]
	if ib1.y != ib2.y || !near(ib2.x-ib1.x, 75, 0.5) {
		t.Errorf("inline-blocks: %v %v", ib1, ib2)
	}
	if x := p["centred"][0].x; !near(x, left+(contentW-150)/2, 0.5) {
		t.Errorf("auto margins: x=%.2f", x)
	}
	if x := p["padded"][0].x; !near(x, left+75, 0.5) {
		t.Errorf("border-box padding: x=%.2f", x)
	}
}

func TestFlexRowAcrossPages(t *testing.T) {
	long := strings.Repeat("<p>long column text</p>", 150)
	p := textPositions(t, `<div style="display:flex"><div style="width:30%">side</div><div style="flex:1">`+long+`</div></div><p>after</p>`)
	if p["side"][0].page != 1 {
		t.Error("sidebar should start on page 1")
	}
	pages := map[int]bool{}
	for _, pt := range p["long"] {
		pages[pt.page] = true
	}
	if len(pages) < 3 {
		t.Errorf("long flex item should flow over pages, got %d", len(pages))
	}
	if after := p["after"][0]; after.page < len(pages) {
		t.Errorf("content after the flex row should follow it, got page %d", after.page)
	}
}
