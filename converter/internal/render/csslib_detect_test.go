package render

import (
	"strings"
	"testing"

	"github.com/xchoManukyan/html2pdf/converter/internal/resource"
)

// describe lists the detected frameworks as "lib/source+lib/source".
func describe(info *Info) string {
	var parts []string
	for _, d := range info.CSS {
		parts = append(parts, d.Lib+"/"+d.Source)
	}
	return strings.Join(parts, "+")
}

const (
	bs5Markup = `<div class="container"><div class="row"><div class="col-md-6"><button class="btn btn-primary">A</button>
		<span class="badge text-bg-success">B</span></div><div class="col-md-6 d-flex justify-content-between ms-3">C</div></div></div>`
	twMarkup = `<div class="flex items-center justify-between px-6 py-4 bg-white shadow-md">
		<h1 class="text-2xl font-semibold text-gray-800">T</h1><a class="text-blue-600 hover:underline">x</a></div>`
	twScript = `<script src="https://cdn.tailwindcss.com"></script>`
	bs4Link  = `<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/bootstrap@4.6.2/dist/css/bootstrap.min.css">`
)

func TestDetectCSSLib(t *testing.T) {
	cases := []struct{ name, html, want string }{
		{"bootstrap 5 classes", bs5Markup, "bootstrap5/classes"},
		{"bootstrap 4 classes", `<div class="row no-gutters"><div class="col-sm-4 ml-2"><span class="badge badge-primary">1</span>
			<p class="font-weight-bold float-right">x</p><button class="btn btn-secondary btn-block">B</button></div></div>`, "bootstrap4/classes"},
		{"bootstrap 5 by data-bs", `<button class="btn btn-primary" data-bs-toggle="modal">A</button><div class="modal-dialog">x</div>`, "bootstrap5/classes"},
		{"tailwind classes", twMarkup, "tailwind/classes"},
		{"tailwind script", twScript + `<p class="p-4">x</p>`, "tailwind/script"},
		{"bootstrap link", bs4Link + `<p>x</p>`, "bootstrap4/link"},
		// several at once, as some sites do
		{"script and link", twScript + bs4Link + `<p>x</p>`, "tailwind/script+bootstrap4/link"},
		{"script and bootstrap classes", twScript + bs5Markup, "tailwind/script+bootstrap5/classes"},
		{"link and tailwind classes", bs4Link + twMarkup, "bootstrap4/link+tailwind/classes"},
		{"both by classes", bs5Markup + twMarkup, "bootstrap5/classes+tailwind/classes"},
		// pages with their own CSS for the same names are left alone
		{"own css", `<style>.btn{} .btn-primary{} .col-md-6{} .row{} .d-flex{} .justify-content-between{}</style>
			<div class="row"><div class="col-md-6 d-flex justify-content-between"><a class="btn btn-primary">x</a></div></div>`, ""},
		{"generic names", `<div class="container"><div class="row"><div class="card header nav content">x</div></div></div>`, ""},
		{"a stray class", `<p class="text-[#00ff00] btn-primary">x</p>`, ""},
		// names both frameworks have are no evidence
		{"shared names", `<p class="ml-2 mr-3 pl-1 float-right w-100 shadow-sm border rounded gap-3">x</p>`, ""},
	}
	for _, c := range cases {
		info := &Info{}
		convertWith(t, c.html, Options{Info: info, Loader: &resource.Loader{}})
		if got := describe(info); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	// a Tailwind page with a few Bootstrap look-alike names stays Tailwind
	var many []string
	for _, c := range []string{"red", "blue", "green", "gray", "amber", "slate", "rose", "teal", "sky", "lime"} {
		for _, n := range []string{"100", "500", "900"} {
			many = append(many, "text-"+c+"-"+n)
		}
	}
	info := &Info{}
	convertWith(t, `<p class="`+strings.Join(many, " ")+` btn-primary col-md-6 d-flex">x</p>`, Options{Info: info})
	if got := describe(info); got != "tailwind/classes" {
		t.Errorf("look-alikes on a Tailwind page: got %q", got)
	}

	// detected frameworks are applied: Bootstrap 5's .btn-primary blue and
	// Tailwind's text-gray-800 together
	_, pdf := convertWith(t, bs5Markup+twMarkup, Options{})
	if !strings.Contains(pdf, "0.051 0.431 0.992 rg") || !strings.Contains(pdf, "0.122 0.161 0.216 rg") {
		t.Error("both detected frameworks should style the page")
	}
	// a choice turns detection off: "none", or the chosen frameworks only
	for lib, want := range map[string]string{
		"none":                 "",
		"bootstrap4":           "bootstrap4/selected",
		"tailwind, bootstrap5": "bootstrap5/selected+tailwind/selected",
		"":                     "tailwind/script+bootstrap5/classes",
	} {
		info := &Info{}
		convertWith(t, twScript+bs5Markup, Options{CSSLib: lib, Info: info})
		if got := describe(info); got != want {
			t.Errorf("CSSLib %q: got %q, want %q", lib, got, want)
		}
	}
}

func TestSeveralChosenLibraries(t *testing.T) {
	page := `<button class="btn btn-primary">Pay</button><p class="text-[#00ff00]">t</p>`
	_, c := convertWith(t, page, Options{CSSLib: "bootstrap5,tailwind"})
	if !strings.Contains(c, "0.051 0.431 0.992 rg") || !strings.Contains(c, "0 1 0 rg") {
		t.Error("Bootstrap and Tailwind chosen together should both apply")
	}
	// both Bootstrap versions: 5 comes last, so it wins on equal selectors
	_, c = convertWith(t, page, Options{CSSLib: "bootstrap4 bootstrap5"})
	if !strings.Contains(c, "0.051 0.431 0.992 rg") {
		t.Error("Bootstrap 5 should come after Bootstrap 4")
	}
}
