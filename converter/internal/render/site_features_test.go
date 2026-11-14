package render

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Regression tests for issues found by converting a real Tailwind site and
// comparing the result with Chromium.

// textX returns the x position of the first text run showing word.
func textX(t *testing.T, content, word string) float64 {
	t.Helper()
	re := regexp.MustCompile(`([\d.]+) ([\d.]+) Td \(` + regexp.QuoteMeta(word) + `\)`)
	m := re.FindStringSubmatch(content)
	if m == nil {
		t.Fatalf("text %q not found", word)
	}
	x, _ := strconv.ParseFloat(m[1], 64)
	return x
}

func textY(t *testing.T, content, word string) float64 {
	t.Helper()
	re := regexp.MustCompile(`([\d.]+) ([\d.]+) Td \(` + regexp.QuoteMeta(word) + `\)`)
	m := re.FindStringSubmatch(content)
	if m == nil {
		t.Fatalf("text %q not found", word)
	}
	y, _ := strconv.ParseFloat(m[2], 64)
	return y
}

const twHead = `<html><head><script src="../js/tailwind.js"></script></head><body>`

func TestTailwindScriptGeneratesCSS(t *testing.T) {
	page := `<h2 class="text-3xl text-[#ff0000]">Big</h2><p class="text-sm">small</p>`
	_, c := convertWith(t, twHead+page, Options{})
	if !strings.Contains(c, "22.5 Tf 1 0 0 rg") { // 1.875rem = 30px = 22.5pt, red
		t.Errorf("Tailwind classes should apply:\n%s", c)
	}
	// other scripts named "tailwind..." are not the CSS compiler (the
	// classes may still reveal Tailwind, but not through the script)
	info := &Info{}
	convertWith(t, `<html><head><script src="https://cdn.jsdelivr.net/npm/@tailwindplus/elements@1"></script></head><body>`+page, Options{Info: info})
	if strings.Contains(describe(info), "/script") {
		t.Error("@tailwindplus/elements must not enable Tailwind CSS")
	}
	// Tailwind's style comes after the page's own stylesheets: on equal
	// specificity the utility wins, exactly as in the browser
	_, c = convertWith(t, `<html><head><script src="tailwind.js"></script><style>.c{color:#0000ff}</style></head><body><p class="c text-[#00ff00]">x</p>`, Options{})
	if !strings.Contains(c, "0 1 0 rg") {
		t.Error("utility should override an earlier author rule of equal specificity")
	}
}

func TestViewportWidth(t *testing.T) {
	page := twHead + `<div class="grid grid-cols-1 md:grid-cols-2"><div>Left</div><div>Right</div></div>`
	out, c := convertWith(t, page, Options{})
	if textY(t, c, "Left") == textY(t, c, "Right") {
		t.Error("at paper width (~680px) md: must not apply")
	}
	if !strings.Contains(string(out), "/MediaBox [0 0 595.28 841.89]") {
		t.Error("paper size should be A4")
	}
	out, c = convertWith(t, page, Options{ViewportWidth: 1280})
	if textY(t, c, "Left") != textY(t, c, "Right") {
		t.Error("at a 1280px viewport md:grid-cols-2 must apply")
	}
	if !strings.Contains(string(out), "/MediaBox [0 0 595.28 841.89]") || !strings.Contains(c, " 0 0 cm\n") {
		t.Error("the viewport is scaled onto the same A4 paper")
	}
	// exactly at a breakpoint: (min-width: 1280px) holds for a 1280px viewport
	_, c = convertWith(t, twHead+`<p class="text-sm xl:text-xl">edge</p>`, Options{ViewportWidth: 1280})
	if !strings.Contains(c, "15 Tf") { // 1.25rem = 20px = 15pt
		t.Error("xl: should match at exactly 1280px")
	}
}

func TestPopoverHidden(t *testing.T) {
	_, c := convertWith(t, `<button>Menu</button><div popover><a>Hidden item</a></div>`, Options{})
	if strings.Contains(c, "(Hidden)") {
		t.Error("closed popovers are not displayed")
	}
}

func TestButtonContentWhitespace(t *testing.T) {
	_, c := convertWith(t, `<button style="display:flex">
				<span>Label</span>
			</button>`, Options{})
	// the source indentation must collapse, not be printed as spaces
	btnX := textX(t, c, "Label")
	_, c2 := convertWith(t, `<button style="display:flex"><span>Label</span></button>`, Options{})
	if d := btnX - textX(t, c2, "Label"); d > 0.5 || d < -0.5 {
		t.Errorf("indentation inside <button> moved the text by %.1fpt", d)
	}
}

func TestAbsoluteAutoMarginsCenter(t *testing.T) {
	_, c := convertWith(t, `<div style="position:relative;width:400px;height:100px">
		<div style="position:absolute;left:0;right:0;top:0;max-width:200px;margin:0 auto"><span>Mid</span></div></div>`, Options{})
	// 400px box at x=42.52pt, 200px child centered: starts 100px (75pt) in
	if x := textX(t, c, "Mid"); x < 42.52+75-0.5 || x > 42.52+75+0.5 {
		t.Errorf("box with left:0; right:0; margin:auto should be centered, text at %.2f", x)
	}
}

func TestPercentWidthDoesNotInflateIntrinsicSize(t *testing.T) {
	// a w-full button inside a dropdown must not make its flex item claim the
	// whole row: the nav links stay on one line
	page := twHead + `<div class="flex justify-between" style="width:900px">
		<div>Logo</div>
		<div><ul class="flex gap-5"><li>Buy Vignette</li><li>Help Center</li></ul></div>
		<div class="flex"><span class="inline-block"><button class="inline-flex w-full">EUR</button></span></div></div>`
	_, c := convertWith(t, page, Options{})
	if textY(t, c, "Buy") != textY(t, c, "Vignette") {
		t.Error("percentage width in an inline-block inflated the intrinsic width")
	}
}

func TestFontFaceQuotedURL(t *testing.T) {
	for _, src := range []string{`url("f.ttf") format('truetype')`, `url(f.ttf)`, `url( 'f.ttf' )`} {
		if u, ok := cssURL(src); !ok || u != "f.ttf" {
			t.Errorf("cssURL(%s) = %q, %v", src, u, ok)
		}
	}
}

func TestCustomPageSize(t *testing.T) {
	out, _ := convertWith(t, `<p>x</p>`, Options{PageSize: "100x200"})
	if !strings.Contains(string(out), "/MediaBox [0 0 283.465 566.929]") {
		t.Error("100x200 mm page expected")
	}
}
