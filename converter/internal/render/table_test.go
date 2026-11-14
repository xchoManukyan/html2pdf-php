package render

import (
	"strings"
	"testing"
)

func TestRowspanGrid(t *testing.T) {
	p := textPositions(t, `<table style="border-collapse:collapse">
		<tr><td rowspan="2">span</td><td>r1c2</td><td>r1c3</td></tr>
		<tr><td>r2c2</td><td>r2c3</td></tr>
		<tr><td colspan="2">wide</td><td>r3c3</td></tr></table>`)
	if !near(p["r2c2"][0].x, p["r1c2"][0].x, 0.01) || !near(p["r2c3"][0].x, p["r1c3"][0].x, 0.01) {
		t.Errorf("row 2 cells must skip the column taken by rowspan: %v %v", p["r2c2"], p["r1c2"])
	}
	if !near(p["r3c3"][0].x, p["r1c3"][0].x, 0.01) {
		t.Errorf("colspan=2 must push r3c3 to column 3")
	}
	// the rowspan cell is vertically centred across both rows
	span, r1, r2 := p["span"][0].y, p["r1c2"][0].y, p["r2c2"][0].y
	if !(span < r1 && span > r2) {
		t.Errorf("rowspan cell should be centred between rows: span=%.1f r1=%.1f r2=%.1f", span, r1, r2)
	}
}

func TestBorderSpacingAndCollapse(t *testing.T) {
	p := textPositions(t, `
		<table style="border-spacing:20px 0"><tr><td style="width:100px;padding:0">s1</td><td style="padding:0">s2</td></tr></table>
		<table style="border-collapse:collapse"><tr><td style="width:100px;padding:0">c1</td><td style="padding:0">c2</td></tr></table>`)
	// separate: 15pt spacing before, between: c2 at left+15+75+15
	if x := p["s2"][0].x; !near(x, left+15+75+15, 0.5) {
		t.Errorf("border-spacing: s2 at %.2f, want %.2f", x, left+105)
	}
	if x := p["c2"][0].x; !near(x, left+75, 0.5) {
		t.Errorf("collapse: c2 at %.2f, want %.2f", x, left+75)
	}
}

func TestFixedLayoutAndCols(t *testing.T) {
	p := textPositions(t, `<table style="width:400px; table-layout:fixed; border-collapse:collapse">
		<colgroup><col style="width:100px"><col></colgroup>
		<tr><td style="padding:0">aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa</td><td style="padding:0">b</td></tr></table>`)
	if x := p["b"][0].x; !near(x, left+75, 0.5) {
		t.Errorf("fixed layout: second column must start at 75pt, got %.2f", x-left)
	}
	_, c := renderRaw(t, `<table><col style="background:#ff0000"><tr><td>x</td></tr></table>`)
	if !strings.Contains(c, "1 0 0 rg") {
		t.Error("<col> background should be painted under its cells")
	}
}

func TestTableHeaderFooterRepeatAndCaption(t *testing.T) {
	var rows strings.Builder
	for i := 0; i < 120; i++ {
		rows.WriteString("<tr><td>row</td></tr>")
	}
	p := textPositions(t, `<table><caption style="caption-side:bottom">cap</caption>
		<thead><tr><th>HEAD</th></tr></thead><tfoot><tr><td>FOOT</td></tr></tfoot><tbody>`+rows.String()+`</tbody></table>`)
	pages := map[int]bool{}
	for _, r := range p["row"] {
		pages[r.page] = true
	}
	if len(pages) < 2 {
		t.Fatal("table should span pages")
	}
	if len(p["HEAD"]) != len(pages) || len(p["FOOT"]) != len(pages) {
		t.Errorf("thead/tfoot should repeat on all %d pages: head=%d foot=%d", len(pages), len(p["HEAD"]), len(p["FOOT"]))
	}
	last := p["row"][len(p["row"])-1]
	if c := p["cap"][0]; c.page != last.page || c.y > last.y {
		t.Errorf("bottom caption should follow the table: cap=%v last row=%v", c, last)
	}
}

func TestTallRowSplitsAcrossPages(t *testing.T) {
	long := strings.Repeat("<p>tall cell text</p>", 120)
	p := textPositions(t, `<table><tr><td>side</td><td>`+long+`</td></tr><tr><td>next</td><td>row</td></tr></table>`)
	pages := map[int]bool{}
	for _, w := range p["tall"] {
		pages[w.page] = true
	}
	if len(pages) < 3 {
		t.Errorf("tall cell should flow over pages, got %d", len(pages))
	}
	if n := p["next"][0]; n.page < len(pages) {
		t.Errorf("the next row must come after the split row, page %d", n.page)
	}
}
