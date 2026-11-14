package render

import (
	"strings"
	"testing"

	"github.com/xchoManukyan/html2pdf/converter/internal/resource"
)

// Bootstrap support and the layout fixes it needed, checked against
// Chromium with the samples/bootstrap*.html pages.

const bs5 = `<html><head><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.3/dist/css/bootstrap.min.css"></head><body>`

func TestBootstrapFromCDNWithoutNetwork(t *testing.T) {
	l := &resource.Loader{} // remote loading off
	_, c := convertWith(t, bs5+`<button class="btn btn-primary">Pay</button>`, Options{Loader: l})
	if !strings.Contains(c, "0.051 0.431 0.992 rg") { // #0d6efd
		t.Error("the bundled Bootstrap should style .btn-primary")
	}
	if len(l.Missing()) != 0 {
		t.Errorf("a CDN Bootstrap link must not be reported missing: %q", l.Missing())
	}
	// a self-hosted copy that is not there falls back to the bundle
	l = &resource.Loader{Files: map[string][]byte{}}
	_, c = convertWith(t, `<link rel="stylesheet" href="css/bootstrap.min.css"><button class="btn btn-primary">Pay</button>`,
		Options{Loader: l, BaseURL: "local:/index.html"})
	if !strings.Contains(c, "0.051 0.431 0.992 rg") || len(l.Missing()) != 0 {
		t.Errorf("missing local bootstrap.min.css should use the bundle (missing %q)", l.Missing())
	}
	// a present local copy wins over the bundle
	l = &resource.Loader{Files: map[string][]byte{"css/bootstrap.min.css": []byte(".btn-primary{background:#ff0000}")}}
	_, c = convertWith(t, `<link rel="stylesheet" href="css/bootstrap.min.css"><button class="btn btn-primary">Pay</button>`,
		Options{Loader: l, BaseURL: "local:/index.html"})
	if !strings.Contains(c, "1 0 0 rg") {
		t.Error("the page's own bootstrap.min.css must be used when it exists")
	}
}

func TestCSSLibOption(t *testing.T) {
	const blue5 = "0.051 0.431 0.992 rg" // Bootstrap 5 .btn-primary
	const blue4 = "0 0.482 1 rg"         // Bootstrap 4 .btn-primary #007bff
	plain := `<button class="btn btn-primary">Pay</button><p class="text-[#00ff00]">t</p>`

	if _, c := convertWith(t, plain, Options{}); strings.Contains(c, blue5) || strings.Contains(c, "0 1 0 rg") {
		t.Error("auto: a page without a framework gets none")
	}
	if _, c := convertWith(t, plain, Options{CSSLib: "bootstrap5"}); !strings.Contains(c, blue5) {
		t.Error("bootstrap5: Bootstrap should apply without a link")
	}
	if _, c := convertWith(t, plain, Options{CSSLib: "bootstrap4"}); !strings.Contains(c, blue4) {
		t.Error("bootstrap4: Bootstrap 4 should apply without a link")
	}
	if _, c := convertWith(t, plain, Options{CSSLib: "tailwind"}); !strings.Contains(c, "0 1 0 rg") {
		t.Error("tailwind: Tailwind classes should apply without the script")
	}
	// the chosen version replaces the page's own Bootstrap link
	l := &resource.Loader{}
	_, c := convertWith(t, bs5+plain, Options{CSSLib: "bootstrap4", Loader: l})
	if !strings.Contains(c, blue4) || strings.Contains(c, blue5) {
		t.Error("bootstrap4 should replace a linked Bootstrap 5")
	}
	if len(l.Missing()) != 0 {
		t.Errorf("replaced links are not missing: %q", l.Missing())
	}
	// the page's own CSS still overrides the framework
	if _, c := convertWith(t, `<style>.btn-primary{background:#ff0000}</style>`+plain, Options{CSSLib: "bootstrap5"}); !strings.Contains(c, "1 0 0 rg") {
		t.Error("the page's CSS must win over the injected framework")
	}
	// none: the CDN link is not replaced by the bundle, Tailwind script ignored
	_, c = convertWith(t, bs5+`<script src="tailwind.js"></script>`+plain, Options{CSSLib: "none", Loader: &resource.Loader{}})
	if strings.Contains(c, blue5) || strings.Contains(c, "0 1 0 rg") {
		t.Error("none: bundled frameworks must not be used")
	}
	if m, l := ParseCSSLibs("Tailwind, bootstrap"); m != "selected" || len(l) != 2 || l[0] != LibBootstrap5 || l[1] != LibTailwind {
		t.Errorf("ParseCSSLibs: %s %q", m, l)
	}
}

func TestNegativeMarginsCollapse(t *testing.T) {
	// Bootstrap rows: margin-top: calc(-1 * gutter) cancels the gutter
	// above; CSS collapses margins as max(positive) + min(negative)
	_, c := convertWith(t, `<p style="margin:0 0 16px">A</p><div style="margin-top:calc(-1 * 16px)">B</div>`+
		`<div style="margin-top:calc(-.5 * 1.5rem)">C</div>`, Options{})
	a, b, cc := textY(t, c, "A"), textY(t, c, "B"), textY(t, c, "C")
	line := 13.8 // "normal" line height of 12pt Helvetica
	if d := a - b; d < line-0.5 || d > line+0.5 {
		t.Errorf("16px and -16px should cancel: A-B = %.1f", d)
	}
	if d := b - cc; d < line-9-0.5 || d > line-9+0.5 {
		t.Errorf("a -12px margin after a cancelled pair should pull C up 9pt: B-C = %.1f", d)
	}
}

func TestListItemFlexItemWidth(t *testing.T) {
	// an <li> in a flex row is as wide as its content: no room for a marker
	_, c := convertWith(t, `<ul style="display:flex;list-style:none;padding:0"><li>1</li><li>2</li></ul>`, Options{})
	if d := textX(t, c, "2") - textX(t, c, "1"); d > 7.5 {
		t.Errorf("flex <li> items should sit side by side without marker space, gap %.1f", d)
	}
}

func TestBootstrapFormControls(t *testing.T) {
	_, c := convertWith(t, bs5+`<input class="form-control" value="alex@example.com">
		<select class="form-select"><option>Austria</option><option selected>Slovakia</option></select>
		<input class="form-check-input" type="checkbox" checked>`, Options{})
	if !strings.Contains(c, "(alex@example.com)") || !strings.Contains(c, "(Slovakia)") {
		t.Error("display:block inputs and selects must show their value")
	}
	if strings.Contains(c, "(Slovakia  ") {
		t.Error("appearance:none selects get no extra arrow (Bootstrap draws one)")
	}
	if !strings.Contains(c, "0.051 0.431 0.992 rg") {
		t.Error(":checked should color the checked box")
	}
}

func TestTextareaRows(t *testing.T) {
	_, c := convertWith(t, `<p style="margin:0">A</p><textarea rows="4" style="font:12px Arial;line-height:20px;padding:0;border:0">t</textarea><p style="margin:0">B</p>`, Options{})
	if h := textY(t, c, "A") - textY(t, c, "B"); h < 4*15 {
		t.Errorf("4 rows of 20px expected between A and B, got %.1fpt", h)
	}
}

func TestTextOverflowEllipsis(t *testing.T) {
	_, c := convertWith(t, `<div style="width:100px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">A long text that does not fit</div>`, Options{})
	if !strings.Contains(c, "\\205)") && !strings.Contains(c, "…") {
		t.Errorf("text should end with an ellipsis:\n%s", c)
	}
	if strings.Count(c, " Tj") != 1 {
		t.Error("nowrap text must stay on one line")
	}
}

func TestPseudoElementBox(t *testing.T) {
	// Bootstrap's breadcrumb divider: a floated ::before with padding
	_, c := convertWith(t, `<style>li+li::before{float:left;padding-right:20px;content:"/"}</style>
		<ol style="display:flex;list-style:none;padding:0"><li>Home</li><li>Orders</li></ol>`, Options{})
	if d := textX(t, c, "Orders") - textX(t, c, "/"); d < 15 {
		t.Errorf("the divider's padding should separate it from the text, gap %.1fpt", d)
	}
	if textY(t, c, "Orders") != textY(t, c, "Home") {
		t.Error("the float must fit next to the text on the same line")
	}
}
