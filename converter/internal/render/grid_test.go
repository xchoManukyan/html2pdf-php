package render

import (
	"strings"
	"testing"
)

func TestGridFrAndFixedTracks(t *testing.T) {
	p := textPositions(t, `
		<div style="display:grid; grid-template-columns: 1fr 2fr; column-gap: 20px"><div>f1</div><div>f2</div></div>
		<div style="display:grid; grid-template-columns: 100px auto 50px"><div>a1</div><div>a2</div><div>a3</div></div>`)
	if x := p["f2"][0].x; !near(x, left+(contentW-15)/3+15, 0.5) {
		t.Errorf("1fr 2fr: second column at %.2f", x-left)
	}
	if x := p["a2"][0].x; !near(x, left+75, 0.5) {
		t.Errorf("100px auto: second column at %.2f", x-left)
	}
	if x := p["a3"][0].x; !near(x, left+contentW-37.5, 0.5) {
		t.Errorf("auto track should stretch: third column at %.2f", x-left)
	}
}

func TestGridAutoPlacementAndLines(t *testing.T) {
	p := textPositions(t, `<div style="display:grid; grid-template-columns: repeat(3, 1fr)">
		<div>i1</div><div>i2</div><div>i3</div><div>i4</div>
		<div style="grid-column: 2 / 4; grid-row: 3">wide</div>
		<div style="grid-column: 1 / -1">full</div>
		<div style="grid-column: span 2">sp</div></div>`)
	col := contentW / 3
	if p["i4"][0].x != p["i1"][0].x || p["i4"][0].y >= p["i1"][0].y {
		t.Error("4th item should wrap to the start of row 2")
	}
	if x := p["wide"][0].x; !near(x, left+col, 0.5) {
		t.Errorf("grid-column 2/4 starts at column 2, got %.2f", x-left)
	}
	if !(p["wide"][0].y < p["i4"][0].y) {
		t.Error("grid-row: 3 should be below row 2")
	}
	if x := p["full"][0].x; !near(x, left, 0.5) || p["full"][0].y >= p["wide"][0].y {
		t.Errorf("1 / -1 should span the whole row after row 3: %v", p["full"])
	}
	if x := p["sp"][0].x; !near(x, left, 0.5) {
		t.Errorf("span 2 item at %.2f", x-left)
	}
}

func TestGridTemplateAreas(t *testing.T) {
	p := textPositions(t, `<div style="display:grid; grid-template-columns: 150px 1fr;
		grid-template-areas: 'head head' 'side main' 'foot foot'">
		<div style="grid-area: main">MAIN</div><div style="grid-area: foot">FOOT</div>
		<div style="grid-area: head">HEAD</div><div style="grid-area: side">SIDE</div></div>`)
	h, s, m, f := p["HEAD"][0], p["SIDE"][0], p["MAIN"][0], p["FOOT"][0]
	if !(h.y > s.y && s.y == m.y && m.y > f.y) {
		t.Errorf("areas order wrong: head=%v side=%v main=%v foot=%v", h, s, m, f)
	}
	if !near(m.x, left+112.5, 0.5) || !near(s.x, left, 0.5) {
		t.Errorf("main should start after the 150px sidebar: %.2f", m.x-left)
	}
}

func TestGridAutoFillFitAndDense(t *testing.T) {
	var items strings.Builder
	for i := 0; i < 5; i++ {
		items.WriteString("<div>c</div>")
	}
	p := textPositions(t, `<div style="display:grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr))">`+items.String()+`</div>
		<div style="display:grid; grid-template-columns: repeat(auto-fit, minmax(100px, 1fr))"><div>q1</div><div>q2</div></div>
		<div style="display:grid; grid-template-columns: repeat(3, 100px); grid-auto-flow: row dense">
		<div>d1</div><div style="grid-column: span 3">d2</div><div>d3</div></div>`)
	rowY := map[float64]int{}
	for _, c := range p["c"] {
		rowY[c.y]++
	}
	if rowY[p["c"][0].y] != 4 {
		t.Errorf("auto-fill minmax(150px,1fr) should give 4 columns on A4, got %d in the first row", rowY[p["c"][0].y])
	}
	if x := p["q2"][0].x; !near(x, left+contentW/2, 0.5) {
		t.Errorf("auto-fit should stretch 2 items over the full width, q2 at %.2f", x-left)
	}
	if d1, d3 := p["d1"][0], p["d3"][0]; d3.y != d1.y {
		t.Errorf("dense packing should put d3 next to d1: d1=%v d3=%v", d1, d3)
	}
}

func TestGridAlignmentAndPaging(t *testing.T) {
	p := textPositions(t, `<div style="display:grid; grid-template-columns: 200px 200px; justify-items: center; align-items: center">
		<div style="height:100px">tall</div><div>mid</div></div>`)
	if x := p["mid"][0].x; x < left+150+30 {
		t.Errorf("justify-items: center should centre the text in its 150pt column, x=%.2f", x-left)
	}
	if d := p["tall"][0].y - p["mid"][0].y; d < 25 || d > 40 {
		t.Errorf("align-items: center offset %.1f", d)
	}
	long := strings.Repeat("<p>gridtext</p>", 150)
	q := textPositions(t, `<div style="display:grid; grid-template-columns: 1fr 3fr"><div>side</div><div>`+long+`</div></div><p>after</p>`)
	pages := map[int]bool{}
	for _, w := range q["gridtext"] {
		pages[w.page] = true
	}
	if len(pages) < 3 || q["after"][0].page < len(pages) {
		t.Errorf("tall grid row should flow over pages (pages=%d, after on %d)", len(pages), q["after"][0].page)
	}
}
