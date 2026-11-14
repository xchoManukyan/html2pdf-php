package render

import (
	"strings"
	"testing"
)

const pageTop = 841.89 - 42.52 // PDF y of the top of the content area (A4, 15 mm)

func TestFloatLeftWrapsText(t *testing.T) {
	words := strings.Repeat("word ", 400)
	p := textPositions(t, `<div style="float:left; width:150px; height:100px; background:#eee">FLOAT</div><p style="margin:0">`+words+`</p>`)
	if x := p["FLOAT"][0].x; !near(x, left, 0.5) {
		t.Errorf("float at x=%.2f", x)
	}
	var beside, below bool
	for _, w := range p["word"] {
		switch {
		case w.y > pageTop-75 && near(w.x, left+112.5, 0.5):
			beside = true // first lines start right of the 150px (112.5pt) float
		case w.y < pageTop-80 && near(w.x, left, 0.5):
			below = true // below the 100px (75pt) float lines use the full width
		case w.y > pageTop-70 && w.x < left+112:
			t.Fatalf("word overlaps the float at x=%.2f y=%.2f", w.x, w.y)
		}
	}
	if !beside || !below {
		t.Errorf("text should wrap beside then below the float (beside=%v below=%v)", beside, below)
	}
}

func TestFloatRightClearAndClearfix(t *testing.T) {
	p := textPositions(t, `
		<div style="float:right; width:100px; height:60px">RIGHT</div>
		<p style="margin:0">short</p>
		<p style="clear:both; margin:0">cleared</p>
		<div class="row"><div style="float:left;width:50px;height:80px">A</div><div style="float:left;width:50px;height:80px">B</div></div>
		<style>.row::after{content:"";display:table;clear:both}</style>
		<p style="margin:0">after</p>`)
	r := p["RIGHT"][0]
	if r.x < left+contentW-76 {
		t.Errorf("right float at x=%.2f", r.x)
	}
	if c := p["cleared"][0]; c.y > pageTop-45 {
		t.Errorf("cleared paragraph should be below the 60px float, y=%.2f", c.y)
	}
	a, b := p["A"][0], p["B"][0]
	if a.y != b.y || !near(b.x-a.x, 37.5, 0.5) {
		t.Errorf("floats should line up side by side: A=%v B=%v", a, b)
	}
	if after := p["after"][0]; after.y > a.y-55 {
		t.Errorf("clearfix should push following content below the floats: after=%.2f A=%.2f", after.y, a.y)
	}
}

func TestBFCBesideFloat(t *testing.T) {
	p := textPositions(t, `
		<div style="float:left;width:80px;height:80px">F</div>
		<div style="overflow:hidden">beside the float</div>`)
	if x := p["beside"][0].x; !near(x, left+60, 0.5) {
		t.Errorf("BFC box should start after the float, x=%.2f", x)
	}
}

func TestPositioning(t *testing.T) {
	long := strings.Repeat("<p>filler</p>", 120)
	p := textPositions(t, `
		<div style="position:relative; height:100px">
			<span>static</span>
			<div style="position:absolute; top:20px; right:0; width:100px">abs</div>
		</div>
		<div style="position:relative; left:20px; top:10px">shifted</div>
		<div style="position:fixed; bottom:0; left:0">footer</div>`+long)
	abs := p["abs"][0]
	if !near(abs.x, left+contentW-75, 0.5) {
		t.Errorf("absolute right:0 width:100px should end at the right edge, x=%.2f", abs.x)
	}
	if st := p["static"][0]; !near(st.y-abs.y, 15, 1) {
		t.Errorf("absolute top:20px should be 15pt below the container top: static=%.2f abs=%.2f", st.y, abs.y)
	}
	if x := p["shifted"][0].x; !near(x, left+15, 0.5) {
		t.Errorf("relative left:20px should shift by 15pt, x=%.2f", x)
	}
	pages := map[int]bool{}
	for _, f := range p["filler"] {
		pages[f.page] = true
	}
	if len(p["footer"]) != len(pages) || len(pages) < 2 {
		t.Errorf("fixed footer should repeat on all %d pages, got %d", len(pages), len(p["footer"]))
	}
	for _, f := range p["footer"] {
		if f.y > 42.52+20 {
			t.Errorf("fixed footer should sit at the bottom of the page area, y=%.2f", f.y)
		}
	}
}
