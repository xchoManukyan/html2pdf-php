package render

import (
	"regexp"
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
)

// Detection tells which CSS framework was applied and why.
type Detection struct {
	Lib    string // LibTailwind, LibBootstrap5, LibBootstrap4 or "" (none)
	Source string // "selected", "script", "link" or "classes"
}

// Info receives facts about a conversion (see Options.Info).
type Info struct {
	CSS []Detection // the frameworks applied, in the order found
}

// Class names that only one framework uses. Shared or generic names
// (container, row, card, flex, text-center, px-4 ...) are left out, so a page
// with its own CSS is not mistaken for a framework page.
var (
	bsClass = regexp.MustCompile(`^(?:` +
		`btn-(?:outline-)?(?:primary|secondary|success|danger|warning|info|light|dark|link)|btn-(?:sm|lg|close|group|toolbar)|` +
		`col-(?:sm-|md-|lg-|xl-|xxl-)?(?:\d{1,2}|auto)|col-(?:sm|md|lg|xl|xxl)|row-cols-\d|offset-(?:sm-|md-|lg-|xl-)?\d+|` +
		`d-(?:sm-|md-|lg-|xl-|xxl-|print-)?(?:none|flex|block|inline|inline-block|inline-flex|grid|table)|` +
		`justify-content-(?:sm-|md-|lg-|xl-)?(?:start|end|center|between|around|evenly)|align-items-(?:sm-|md-|lg-|xl-)?(?:start|end|center|baseline|stretch)|` +
		`align-self-\w+|flex-(?:sm-|md-|lg-)?(?:column|wrap|nowrap|fill)|` +
		`[mp][tbsexylr]?-(?:sm|md|lg|xl|xxl)-(?:[0-5]|auto)|` +
		`navbar-(?:expand(?:-\w+)?|brand|nav|toggler|toggler-icon|collapse|light|dark|text)|nav-(?:link|item|tabs|pills|fill)|` +
		`form-(?:control|control-lg|control-sm|select|label|text|check|check-input|check-label|switch|floating|range|group|row|inline)|` +
		`input-group(?:-text|-append|-prepend|-sm|-lg)?|` +
		`(?:alert|badge|text-bg|list-group-item|table|border|link)-(?:primary|secondary|success|danger|warning|info|light|dark)|` +
		`(?:bg|text)-(?:primary|secondary|success|danger|warning|info|body|body-secondary|body-tertiary)(?:-subtle|-emphasis)?|` +
		`text-muted|text-body|lead|display-[1-6]|h[1-6]|small|mark|` +
		`card-(?:body|title|text|header|footer|subtitle|img-top|link|group|deck)|list-group(?:-item|-flush|-numbered)?|` +
		`table-(?:striped|hover|bordered|sm|responsive|dark|borderless)|dropdown-(?:menu|item|toggle|divider)|` +
		`modal-(?:dialog|content|header|body|footer|title)|breadcrumb-item|page-(?:item|link)|pagination|` +
		`accordion-(?:item|header|button|body|collapse)|progress-bar|spinner-border|placeholder-glow|` +
		`fw-(?:bold|bolder|semibold|medium|normal|light|lighter)|fs-[1-6]|g[xy]?-[0-5]|[mp][se]-(?:[0-5]|auto)|` +
		`font-weight-\w+|ml-(?:[0-5]|auto)|mr-(?:[0-5]|auto)|pl-[0-5]|pr-[0-5]|float-(?:sm-|md-|lg-)?(?:left|right)|` +
		`custom-(?:select|control|control-input|control-label|checkbox|switch|file)|btn-block|no-gutters|jumbotron|` +
		`visually-hidden|rounded-pill|text-(?:start|end)|shadow-sm|vstack|hstack|ratio|ratio-16x9|img-fluid|img-thumbnail` +
		`)$`)
	// Bootstrap 4 only (ml-/mr-, pl-/pr-, float-left are Tailwind names too,
	// but only count once Bootstrap is the winner).
	bs4Class = regexp.MustCompile(`^(?:font-weight-\w+|[mp][lr]-(?:sm-|md-|lg-|xl-)?(?:[0-5]|auto)|float-(?:sm-|md-|lg-)?(?:left|right)|` +
		`custom-\w+(?:-\w+)*|btn-block|no-gutters|jumbotron|form-group|form-row|form-inline|input-group-(?:append|prepend)|` +
		`badge-(?:primary|secondary|success|danger|warning|info|light|dark)|card-deck|text-(?:left|right)|sr-only|media|media-body)$`)
	bs5Class = regexp.MustCompile(`^(?:[mp][se]-(?:sm-|md-|lg-|xl-|xxl-)?(?:[0-5]|auto)|fw-\w+|fs-[1-6]|g[xy]?-[0-5]|form-select|form-label|` +
		`form-floating|form-switch|visually-hidden|text-bg-\w+|btn-close|d-grid|vstack|hstack|ratio|row-cols-\d|` +
		`(?:bg|text)-\w+-(?:subtle|emphasis)|text-(?:start|end)|float-(?:start|end)|accordion-\w+|placeholder-\w+)$`)

	twColors = `(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)`
	twClass  = regexp.MustCompile(`^(?:` +
		`(?:bg|text|border|ring|from|via|to|fill|stroke|divide|outline|decoration|placeholder|accent|shadow)-` + twColors + `-(?:50|[1-9]00|950)(?:/\d+)?|` +
		`-?[mp][xytblrse]?-(?:[6-9]|1[0-9]|2[0-4]|28|3[26]|4[048]|5[26]|6[04]|7[2]|80|96|px|0\.5|1\.5|2\.5|3\.5)|` +
		`[wh]-(?:full|screen|auto|min|max|fit|px|\d+|\d+/\d+)|min-h-screen|max-w-(?:xs|sm|md|lg|xl|[2-7]xl|prose|screen-\w+|full|none)|` +
		`grid-cols-\d+|col-span-(?:\d+|full)|gap(?:-[xy])?-\d+(?:\.5)?|space-[xy]-\d+|divide-[xy]|` +
		`items-(?:center|start|end|baseline|stretch)|justify-(?:between|center|start|end|around|evenly)|content-(?:center|between)|` +
		`flex-(?:col|row|1|auto|none|grow|shrink)|inline-flex|` +
		`rounded-(?:md|xl|2xl|3xl|full|none|[tblr]-\w+)|shadow-(?:md|xl|2xl|inner)|` +
		`font-(?:thin|extralight|light|normal|medium|semibold|extrabold|black|sans|serif|mono)|` +
		`text-(?:xs|sm|base|lg|xl|[2-9]xl)|leading-\w+|tracking-\w+|uppercase|antialiased|truncate|` +
		`inset-(?:0|x-0|y-0)|z-\d+|opacity-\d+|transition(?:-\w+)?|duration-\d+|ease-\w+|object-(?:cover|contain)|` +
		`ring(?:-\d)?|border-[tblrxy]?-?[248]?|cursor-pointer|select-none|sr-only|whitespace-\w+|overflow-(?:x|y)-\w+` +
		`)$`)
)

// sharedClass matches names both Bootstrap and Tailwind define (ml-2,
// float-right, w-100, gap-3, shadow-sm ...): they tell Bootstrap 4 from 5
// but don't count as evidence for either framework.
var sharedClass = regexp.MustCompile(`^(?:[mp][lrse]-(?:[0-5]|auto)|float-(?:left|right|start|end|none)|text-(?:start|end)|` +
	`shadow(?:-sm|-lg|-none)?|border(?:-0)?|rounded(?:-0|-sm|-lg|-full|-none)?|[wh]-(?:25|50|75|100|auto)|gap-[0-5]|z-[0-3]|` +
	`opacity-(?:0|25|50|75|100)|overflow-(?:auto|hidden|visible|scroll)|overflow-[xy]-\w+|sr-only|visible|invisible|` +
	`flex-(?:row|wrap|nowrap|fill)|fixed-top|sticky-top)$`)

// detectFromClasses finds the frameworks a page uses without loading them:
// it counts distinct class names only Bootstrap or only Tailwind uses,
// ignoring classes the page's own CSS defines. A variant (md:flex) or an
// arbitrary value (w-[300px]) counts twice: that syntax is Tailwind's. Each
// framework needs 3 points and at least an eighth of the other's, so a
// few look-alike names on a page of the other framework don't count; a page
// that really mixes both gets both, as in the browser.
func detectFromClasses(root *htmlparse.Node, pageCSS []string) []string {
	defined := map[string]bool{}
	for _, t := range pageCSS {
		for _, m := range definedClassRe.FindAllStringSubmatch(t, -1) {
			defined[strings.ReplaceAll(m[1], `\`, "")] = true
		}
	}
	seen := map[string]bool{}
	bs, bs4, bs5, tw := 0, 0, 0, 0
	bsAttr4, bsAttr5 := false, false
	var walk func(n *htmlparse.Node)
	walk = func(n *htmlparse.Node) {
		if n.Type == htmlparse.ElementNode {
			for k := range n.Attrs {
				switch {
				case strings.HasPrefix(k, "data-bs-"):
					bsAttr5 = true
				case k == "data-toggle" || k == "data-target" || k == "data-dismiss":
					bsAttr4 = true
				}
			}
			for _, c := range strings.Fields(n.Attr("class")) {
				if seen[c] || defined[c] {
					continue
				}
				seen[c] = true
				if strings.ContainsAny(c, ":[") {
					tw += 2
					continue
				}
				if bs4Class.MatchString(c) {
					bs4++
				}
				if bs5Class.MatchString(c) {
					bs5++
				}
				isBS, isTW := bsClass.MatchString(c), twClass.MatchString(c)
				if sharedClass.MatchString(c) || isBS && isTW {
					continue
				}
				if isBS {
					bs++
				}
				if isTW {
					tw++
				}
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if bsAttr5 {
		bs, bs5 = bs+2, bs5+3
	}
	if bsAttr4 {
		bs, bs4 = bs+2, bs4+3
	}
	var libs []string
	if bs >= 3 && bs*8 >= tw {
		if bs4 > bs5 {
			libs = append(libs, LibBootstrap4)
		} else {
			libs = append(libs, LibBootstrap5)
		}
	}
	if tw >= 3 && tw*8 >= bs {
		libs = append(libs, LibTailwind)
	}
	return libs
}

// definedClassRe finds class selectors in a stylesheet, e.g. ".btn" or
// ".md\:flex"; escaped characters stay part of the name.
var definedClassRe = regexp.MustCompile(`\.((?:[-_a-zA-Z0-9]|\\.)+)`)
