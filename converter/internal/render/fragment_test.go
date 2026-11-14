package render

import (
	"fmt"
	"strings"
	"testing"
)

// spacer returns a block that leaves `room` points free at the bottom of
// the first A4 page (15 mm margins: 756.85pt of content height).
func spacer(room float64) string {
	return fmt.Sprintf(`<div style="height:%.3fpt; background:#eee"></div>`, 756.85-room)
}

func lines(prefix string, n int) string {
	var parts []string
	for i := 1; i <= n; i++ {
		parts = append(parts, fmt.Sprintf("%s%d", prefix, i))
	}
	return strings.Join(parts, "<br>")
}

func TestOrphansAndWidows(t *testing.T) {
	const lh = 13.8 // one line of 12pt Helvetica
	// Only one line would fit: orphans: 2 moves the whole paragraph.
	p := textPositions(t, spacer(lh+1)+`<p style="margin:0">`+lines("O", 6)+`</p>`)
	if p["O1"][0].page != 2 {
		t.Errorf("a single orphan line must move to the next page, O1 on page %d", p["O1"][0].page)
	}
	// Five of six lines fit: widows: 2 pulls one more line over.
	p = textPositions(t, spacer(5*lh+1)+`<p style="margin:0">`+lines("W", 6)+`</p>`)
	if p["W4"][0].page != 1 || p["W5"][0].page != 2 {
		t.Errorf("widows: W4 page %d (want 1), W5 page %d (want 2)", p["W4"][0].page, p["W5"][0].page)
	}
	// widows: 1 / orphans: 1 turns the control off.
	p = textPositions(t, spacer(5*lh+1)+`<p style="margin:0; widows:1; orphans:1">`+lines("N", 6)+`</p>`)
	if p["N5"][0].page != 1 {
		t.Errorf("with widows:1 five lines should stay on page 1, N5 on page %d", p["N5"][0].page)
	}
}

func TestBreakInsideAndAfterAvoid(t *testing.T) {
	p := textPositions(t, spacer(50)+`<div style="break-inside: avoid">`+lines("K", 6)+`</div>`)
	if p["K1"][0].page != 2 {
		t.Errorf("break-inside: avoid block should move whole to page 2, K1 on page %d", p["K1"][0].page)
	}
	p = textPositions(t, spacer(50)+`<div>`+lines("S", 6)+`</div>`)
	if p["S1"][0].page != 1 {
		t.Error("without break-inside: avoid the block starts on page 1")
	}
	p = textPositions(t, spacer(42)+`<h2>Heading</h2><p>`+lines("B", 4)+`</p>`)
	if p["Heading"][0].page != 2 {
		t.Errorf("a heading must not be left alone at the page bottom (break-after: avoid), page %d", p["Heading"][0].page)
	}
}

func TestTransforms(t *testing.T) {
	_, c := renderRaw(t, `<div style="width:100px; height:40px; background:#eee; transform: rotate(90deg)">rot</div>
		<div style="transform: translate(20px, 10px) scale(2)">moved</div>
		<span style="display:inline-block; transform: skewX(30deg)">skew</span>`)
	if !strings.Contains(c, "q 0 -1 1 0 ") {
		t.Errorf("rotate(90deg) should produce a rotation matrix, content:\n%.400s", c)
	}
	if !strings.Contains(c, "q 2 0 0 2 ") {
		t.Error("scale(2) should produce a scaling matrix")
	}
	if !strings.Contains(c, "q 1 0 -0.577 1 ") {
		t.Error("skewX(30deg) should produce a skew matrix")
	}
}
