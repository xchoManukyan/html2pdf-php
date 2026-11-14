package css

import (
	"math"
	"testing"

	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
)

var a4 = Env{Width: 510, Height: 756}

func matches(sel string, n *htmlparse.Node, pseudo string) bool {
	ss := ParseStylesheet(sel+" { color: red }", a4)
	return len(ss.Match(n, pseudo)) > 0
}

func TestSelectors(t *testing.T) {
	doc := htmlparse.Parse(`<html><body>
<table class="items"><tr class="total"><td id="x" class="num big" data-kind="price-eur">1</td><td>2</td><td>3</td></tr></table>
<div lang="en-US"><p>a</p><p class="note"><span>s</span></p><p>c</p><em></em></div>
<a href="https://example.com/file.pdf">link</a></body></html>`)
	td := doc.Find("td")
	tds := doc.FindAll("td")
	ps := doc.FindAll("p")
	span := doc.Find("span")
	em := doc.Find("em")
	a := doc.Find("a")
	div := doc.Find("div")
	cases := []struct {
		sel  string
		node interface{ Attr(string) string }
		n    *htmlparse.Node
		want bool
	}{
		{"td", nil, td, true},
		{"td.num.big", nil, td, true},
		{"#x", nil, td, true},
		{"table.items td", nil, td, true},
		{"tr > td", nil, td, true},
		{"table > td", nil, td, false},
		{"div > p > span", nil, span, true},
		{"td:first-child", nil, tds[0], true},
		{"td:first-child", nil, tds[1], false},
		{"td:last-child", nil, tds[2], true},
		{"td:nth-child(2)", nil, tds[1], true},
		{"td:nth-child(odd)", nil, tds[2], true},
		{"td:nth-child(even)", nil, tds[2], false},
		{"td:nth-child(2n+1)", nil, tds[0], true},
		{"td:nth-last-child(1)", nil, tds[2], true},
		{"p:nth-of-type(2)", nil, ps[1], true},
		{"p:last-of-type", nil, ps[2], true},
		{"p:not(.note)", nil, ps[0], true},
		{"p:not(.note)", nil, ps[1], false},
		{":is(h1, p).note", nil, ps[1], true},
		{"p + p", nil, ps[1], true},
		{"p + p", nil, ps[0], false},
		{".note ~ p", nil, ps[2], true},
		{"div:has(.note)", nil, div, true},
		{"div:has(> span)", nil, div, false},
		{"em:empty", nil, em, true},
		{"[data-kind]", nil, td, true},
		{"[data-kind=price-eur]", nil, td, true},
		{`[data-kind^="price"]`, nil, td, true},
		{`[data-kind$="eur"]`, nil, td, true},
		{`[data-kind*="ce-e"]`, nil, td, true},
		{`[lang|="en"]`, nil, div, true},
		{`a[href$=".PDF" i]`, nil, a, true},
		{"a:link", nil, a, true},
		{"a:hover", nil, a, false},
		{":root", nil, doc.Find("html"), true},
		{"td:frobnicate", nil, td, false}, // invalid -> rule dropped
	}
	for _, c := range cases {
		if got := matches(c.sel, c.n, ""); got != c.want {
			t.Errorf("%q: got %v, want %v", c.sel, got, c.want)
		}
	}
	if !matches("p.note::before", ps[1], "before") || matches("p.note::before", ps[1], "") {
		t.Error("::before matching")
	}
}

func TestSpecificityAndOrder(t *testing.T) {
	doc := htmlparse.Parse(`<p id="a" class="b">x</p>`)
	p := doc.Find("p")
	ss := ParseStylesheet(`#a { color: red } .b { color: green } p { color: blue } .b { color: black !important }`, a4)
	d := ss.Match(p, "")
	if len(d) != 4 || d[0].Value != "blue" || d[len(d)-1].Value != "red" {
		t.Errorf("order: %+v", d)
	}
	if !d[2].Important && !d[1].Important {
		t.Errorf("important flag lost: %+v", d)
	}
}

func TestMediaAndNesting(t *testing.T) {
	doc := htmlparse.Parse(`<div class="card"><h2>t</h2></div>`)
	h2 := doc.Find("h2")
	css := `
@media screen { h2 { color: red } }
@media print and (min-width: 300px) { h2 { margin: 1px } }
@media (max-width: 200px) { h2 { padding: 1px } }
@media (width >= 400px) { h2 { border: 1px solid } }
.card { & h2 { font-weight: bold } }
@supports (display: grid) { h2 { display: grid } }`
	RegisterSupported("display")
	got := map[string]bool{}
	for _, d := range ParseStylesheet(css, a4).Match(h2, "") {
		got[d.Prop] = true
	}
	want := map[string]bool{"margin": true, "border": true, "font-weight": true, "display": true}
	for p := range want {
		if !got[p] {
			t.Errorf("missing %s", p)
		}
	}
	for _, p := range []string{"color", "padding"} {
		if got[p] {
			t.Errorf("%s should not apply", p)
		}
	}
}

func TestColorsAndLengths(t *testing.T) {
	if c, ok := ParseColor("#f00"); !ok || c.R != 1 || c.G != 0 || c.A != 1 {
		t.Errorf("#f00 -> %v", c)
	}
	if c, ok := ParseColor("rebeccapurple"); !ok || math.Abs(c.R-0.4) > 0.01 {
		t.Errorf("named -> %v", c)
	}
	if c, ok := ParseColor("hsl(120, 100%, 25%)"); !ok || math.Abs(c.G-0.5) > 0.01 || c.R != 0 {
		t.Errorf("hsl -> %v", c)
	}
	if c, ok := ParseColor("rgb(0 0 0 / 50%)"); !ok || c.A != 0.5 {
		t.Errorf("rgb alpha -> %v", c)
	}
	ctx := LengthCtx{Em: 10, Rem: 12, Percent: 200, VW: 500, VH: 800}
	for in, want := range map[string]float64{
		"16px": 12, "2em": 20, "1.5rem": 18, "50%": 100, "10vw": 50,
		"calc(100% - 20px)": 185, "calc(2 * (1em + 5pt))": 30, "min(10pt, 2em)": 10,
		"max(10pt, 2em)": 20, "clamp(5pt, 50%, 30pt)": 30, "calc(10px*3)": 22.5,
	} {
		if v, ok := ParseLength(in, ctx); !ok || math.Abs(v-want) > 0.001 {
			t.Errorf("%s = %v (ok=%v), want %v", in, v, ok, want)
		}
	}
}
