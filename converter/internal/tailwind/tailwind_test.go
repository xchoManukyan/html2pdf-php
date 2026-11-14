package tailwind

import (
	"strings"
	"testing"

	"github.com/xchoManukyan/html2pdf/converter/internal/css"
	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
)

func gen(t *testing.T, class string) string {
	t.Helper()
	r, ok := build(class)
	if !ok {
		t.Fatalf("%s: no rule", class)
	}
	var sb strings.Builder
	for _, d := range r.decls {
		sb.WriteString(d.prop + ": " + d.value + "; ")
	}
	out := strings.TrimSpace(sb.String())
	if r.media != "" {
		out = "@" + r.media + " " + out
	}
	return out
}

func TestUtilities(t *testing.T) {
	cases := map[string]string{
		"p-4":                         "padding: 1rem;",
		"px-2.5":                      "padding-left: 0.625rem; padding-right: 0.625rem;",
		"-mt-px":                      "margin-top: -1px;",
		"mx-auto":                     "margin-left: auto; margin-right: auto;",
		"ps-4":                        "padding-left: 1rem;",
		"w-1/2":                       "width: 50%;",
		"w-full":                      "width: 100%;",
		"h-[170px]":                   "height: 170px;",
		"max-w-3xl":                   "max-width: 48rem;",
		"max-w-screen-xl":             "max-width: 1280px;",
		"size-6":                      "width: 1.5rem; height: 1.5rem;",
		"text-3xl":                    "font-size: 1.875rem; line-height: 2.25rem;",
		"text-sm/6":                   "font-size: 0.875rem; line-height: 1.5rem;",
		"text-[11px]":                 "font-size: 11px;",
		"text-[#0A458F]":              "color: #0A458F;",
		"text-gray-700":               "color: #374151;",
		"bg-black/5":                  "background-color: rgba(0, 0, 0, 0.05);",
		"bg-[rgba(245,247,250,1)]":    "background-color: rgba(245,247,250,1);",
		"font-medium":                 "font-weight: 500;",
		"font-['Poppins',sans-serif]": "font-family: 'Poppins',sans-serif;",
		"grid-cols-3":                 "grid-template-columns: repeat(3, minmax(0, 1fr));",
		"lg:grid-cols-[2fr_1fr]":      "@(min-width: 1024px) grid-template-columns: 2fr 1fr;",
		"col-span-2":                  "grid-column: span 2 / span 2;",
		"md:py-12":                    "@(min-width: 768px) padding-top: 3rem; padding-bottom: 3rem;",
		"max-md:hidden":               "@(max-width: 767.98px) display: none;",
		"!font-medium":                "font-weight: 500 !important;",
		"rounded-xl":                  "border-radius: 0.75rem;",
		"rounded-s-lg":                "border-top-left-radius: 0.5rem; border-bottom-left-radius: 0.5rem;",
		"border":                      "border-width: 1px;",
		"border-b-2":                  "border-bottom-width: 2px;",
		"border-gray-800":             "border-color: #1f2937;",
		"shadow-lg":                   "box-shadow: 0 10px 15px -3px rgba(0, 0, 0, 0.1), 0 4px 6px -4px rgba(0, 0, 0, 0.1);",
		"shadow-[0_8px_20px_0_rgba(11,11,11,.04)]": "box-shadow: 0 8px 20px 0 rgba(11,11,11,.04);",
		"opacity-80":            "opacity: 0.8;",
		"-translate-y-1/2":      "--tw-translate-y: -50%; transform: " + transformValue + ";",
		"justify-between":       "justify-content: space-between;",
		"items-center":          "align-items: center;",
		"inset-0":               "top: 0px; right: 0px; bottom: 0px; left: 0px;",
		"top-1/2":               "top: 50%;",
		"list-[lower-alpha]":    "list-style-type: lower-alpha;",
		"[mask-type:luminance]": "mask-type: luminance;",
	}
	for class, want := range cases {
		if got := gen(t, class); got != want {
			t.Errorf("%s:\n got %s\nwant %s", class, got, want)
		}
	}
}

func TestSkipped(t *testing.T) {
	// interaction variants never match on paper; values outside the v3
	// scale and unknown names produce nothing, as in Tailwind 3.4
	for _, c := range []string{"hover:bg-white", "focus:outline-none", "dark:text-white", "[&:not([hidden])]:block",
		"w-30", "text-gray", "my-custom-class", "bg-[rgba(255,", "data-closed:opacity-0"} {
		if _, ok := build(c); ok {
			t.Errorf("%s should not generate CSS", c)
		}
	}
}

func TestEscapingParses(t *testing.T) {
	classes := []string{"md:py-12", "w-1/2", "!text-[rgba(14,14,14,1)]", "mt-2.5", "lg:grid-cols-[2fr_1fr]",
		"2xl:p-4", "text-[#0A458F]", "space-y-4", "bg-black/5", "container"}
	sheet := css.NewStylesheet()
	sheet.Add(Generate(classes), css.Env{Width: 2000 * 0.75, Height: 1000})
	// every escaped selector must parse and match an element with the class
	for _, c := range classes {
		if c == "space-y-4" {
			continue
		}
		doc := htmlparse.Parse(`<div class="` + c + `">x</div>`)
		if len(sheet.Match(doc.Find("div"), "")) == 0 {
			t.Errorf("%s: generated rule does not match", c)
		}
	}
	doc := htmlparse.Parse(`<ul class="space-y-4"><li>a</li><li>b</li></ul>`)
	lis := doc.FindAll("li")
	hasMargin := func(i int) bool {
		for _, d := range sheet.Match(lis[i], "") {
			if d.Prop == "margin-top" {
				return true
			}
		}
		return false
	}
	if hasMargin(0) || !hasMargin(1) {
		t.Error("space-y-4 must give a margin to every child but the first")
	}
}

func TestGradientAndSpace(t *testing.T) {
	out := Generate([]string{"bg-gradient-to-b", "from-[#FFFFFF]", "to-[#FFFFFF80]", "space-y-2"})
	for _, want := range []string{
		"background-image: linear-gradient(to bottom, var(--tw-gradient-stops))",
		"--tw-gradient-from: #FFFFFF;", "--tw-gradient-to: rgba(255, 255, 255, 0);", "--tw-gradient-to: #FFFFFF80;",
		".space-y-2 > :not([hidden]) ~ :not([hidden]) { margin-top: 0.5rem;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	// "to" must come after "from" so it wins
	if strings.Index(out, "--tw-gradient-to: #FFFFFF80") < strings.Index(out, "--tw-gradient-from: #FFFFFF") {
		t.Error("to-* must follow from-*")
	}
}

func TestOrderBreakpointsLast(t *testing.T) {
	out := Generate([]string{"md:p-8", "p-2", "lg:p-10", "sm:p-4"})
	i2, i4, i8, i10 := strings.Index(out, ".p-2 "), strings.Index(out, "sm\\3a p-4"), strings.Index(out, "md\\3a p-8"), strings.Index(out, "lg\\3a p-10")
	if !(i2 < i4 && i4 < i8 && i8 < i10) {
		t.Errorf("rules must be ordered base, sm, md, lg: %d %d %d %d", i2, i4, i8, i10)
	}
}
