package render

import (
	"path"
	"regexp"
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/bootstrap"
	"github.com/xchoManukyan/html2pdf/converter/internal/css"
	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
	"github.com/xchoManukyan/html2pdf/converter/internal/resource"
	"github.com/xchoManukyan/html2pdf/converter/internal/tailwind"
)

type fetchFunc func(abs string) ([]byte, string, error)

var (
	importRe = regexp.MustCompile(`(?i)@import\s+(?:url\(\s*(?:"([^"]*)"|'([^']*)'|([^)\s]*))\s*\)|"([^"]*)"|'([^']*)')\s*([^;]*);`)
	urlRe    = regexp.MustCompile(`(?i)url\(\s*(?:"([^"]*)"|'([^']*)'|([^)"'\s]*))\s*\)`)
)

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// prepareCSS inlines @import rules (up to a small depth) and rewrites
// url(...) references so they are absolute: relative URLs in a stylesheet
// refer to the stylesheet's own location, not the document's.
func prepareCSS(text, sheetURL string, fetch fetchFunc, env css.Env, depth int) string {
	text = importRe.ReplaceAllStringFunc(text, func(m string) string {
		sub := importRe.FindStringSubmatch(m)
		ref := firstNonEmpty(sub[1], sub[2], sub[3], sub[4], sub[5])
		media := strings.TrimSpace(sub[6])
		if depth >= 5 || ref == "" || (media != "" && !css.EvalMedia(media, env)) {
			return ""
		}
		abs := resource.Resolve(sheetURL, ref)
		data, _, err := fetch(abs)
		if err != nil {
			return ""
		}
		return prepareCSS(string(data), abs, fetch, env, depth+1)
	})
	return urlRe.ReplaceAllStringFunc(text, func(m string) string {
		sub := urlRe.FindStringSubmatch(m)
		ref := firstNonEmpty(sub[1], sub[2], sub[3])
		if ref == "" || strings.HasPrefix(ref, "#") {
			return m
		}
		return `url("` + strings.ReplaceAll(resource.Resolve(sheetURL, ref), `"`, `%22`) + `")`
	})
}

// CSS framework names for Options.CSSLib.
const (
	LibAuto       = "auto"
	LibNone       = "none"
	LibTailwind   = "tailwind"
	LibBootstrap5 = "bootstrap5"
	LibBootstrap4 = "bootstrap4"
)

// libOrder is the order bundled frameworks are applied in when several are
// chosen: Bootstrap before the page's styles, Tailwind after them.
var libOrder = []string{LibBootstrap4, LibBootstrap5, LibTailwind}

var bootstrapLibs = map[string]int{LibBootstrap5: 5, LibBootstrap4: 4}

// ParseCSSLibs reads Options.CSSLib: a comma or space separated list of
// frameworks. It returns the mode LibAuto (empty, "auto" or nothing known:
// detect from the page), LibNone ("none" alone: no bundled framework), or
// "selected" with the chosen frameworks in libOrder.
func ParseCSSLibs(s string) (mode string, libs []string) {
	want := map[string]bool{}
	none := false
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		switch f {
		case "none", "off":
			none = true
		case "bootstrap":
			want[LibBootstrap5] = true
		case LibTailwind, LibBootstrap5, LibBootstrap4:
			want[f] = true
		}
	}
	for _, l := range libOrder {
		if want[l] {
			libs = append(libs, l)
		}
	}
	switch {
	case len(libs) > 0:
		return "selected", libs
	case none:
		return LibNone, nil
	}
	return LibAuto, nil
}

// collectStylesheets returns the author CSS of the document in document
// order: <style> elements and <link rel="stylesheet"> files, plus the
// bundled CSS frameworks the page uses or that were chosen (see
// Options.CSSLib), and which frameworks those are.
func collectStylesheets(root *htmlparse.Node, base string, fetch fetchFunc, forget func(string), env css.Env, lib string) ([]string, []Detection) {
	mode, chosen := ParseCSSLibs(lib)
	auto := mode == LibAuto
	var dets []Detection
	add := func(l, source string) {
		for _, d := range dets {
			if d.Lib == l {
				return
			}
		}
		dets = append(dets, Detection{l, source})
	}
	var bsVersions []int // Bootstrap versions to apply from the bundle
	useTW := false
	for _, l := range chosen {
		if v := bootstrapLibs[l]; v != 0 {
			bsVersions = append(bsVersions, v)
		} else if l == LibTailwind {
			useTW = true
		}
	}
	bsDone := false    // bsVersions already placed at a Bootstrap link
	pageHasBS := false // the page links some bootstrap.css
	headEnd := -1      // index in out at the end of <head>
	tailwindAt := -1   // where Tailwind's runtime would insert its <style>
	var out []string
	sheet := func(major int, at string) string {
		s, _ := bootstrap.CSS(major)
		return prepareCSS(s, at, fetch, env, 0)
	}
	var walk func(n *htmlparse.Node)
	walk = func(n *htmlparse.Node) {
		for _, c := range n.Children {
			if c.Type != htmlparse.ElementNode {
				continue
			}
			if c.Tag == "script" && auto && tailwindAt < 0 && isTailwindScript(c.Attr("src")) {
				tailwindAt = len(out)
				add(LibTailwind, "script")
			}
			if c.Tag == "head" {
				walk(c)
				headEnd = len(out)
				if tailwindAt >= 0 {
					tailwindAt = len(out) // appended at the end of <head>
				}
				continue
			}
			switch c.Tag {
			case "style":
				if m := c.Attr("media"); m == "" || css.EvalMedia(m, env) {
					out = append(out, prepareCSS(c.TextContent(), base, fetch, env, 0))
				}
				continue
			case "link":
				rel := strings.ToLower(c.Attr("rel"))
				if !strings.Contains(" "+rel+" ", " stylesheet ") || strings.Contains(rel, "alternate") {
					continue
				}
				if m := c.Attr("media"); m != "" && !css.EvalMedia(m, env) {
					continue
				}
				abs := resource.Resolve(base, c.Attr("href"))
				major, fromCDN, isBootstrap := bootstrap.Match(abs)
				if isBootstrapName(abs) {
					pageHasBS = true
					if len(bsVersions) > 0 {
						// the chosen versions replace the page's Bootstrap links
						if !bsDone {
							for _, v := range bsVersions {
								out = append(out, sheet(v, abs))
							}
							bsDone = true
						}
						if forget != nil {
							forget(abs)
						}
						continue
					}
				}
				// Bootstrap from a public CDN: use the bundled copy (works
				// offline and with remote loading off)
				isBootstrap = isBootstrap && auto
				if isBootstrap {
					add(map[int]string{5: LibBootstrap5, 4: LibBootstrap4}[major], "link")
					if fromCDN {
						out = append(out, sheet(major, abs))
						continue
					}
				}
				if data, _, err := fetch(abs); err == nil {
					out = append(out, prepareCSS(string(data), abs, fetch, env, 0))
				} else if isBootstrap {
					// a local or self-hosted copy that is not available
					out = append(out, sheet(major, abs))
					if forget != nil {
						forget(abs) // replaced, so not reported as missing
					}
				}
				continue
			case "svg", "template", "noscript", "script":
				continue
			}
			walk(c)
		}
	}
	walk(root)
	if auto {
		// frameworks the page uses without loading them: recognised by
		// their class names (several can be in use at once)
		for _, guess := range detectFromClasses(root, out) {
			switch {
			case bootstrapLibs[guess] != 0 && !pageHasBS:
				bsVersions = append(bsVersions, bootstrapLibs[guess])
				add(guess, "classes")
			case guess == LibTailwind && tailwindAt < 0:
				useTW = true
				add(guess, "classes")
			}
		}
	}
	for _, l := range chosen {
		add(l, "selected")
	}
	if useTW && tailwindAt < 0 {
		tailwindAt = headEnd
		if tailwindAt < 0 {
			tailwindAt = len(out) // no <head>: after the page's stylesheets
		}
	}
	if len(bsVersions) > 0 && !bsDone {
		// not linked by the page: load them first, so the page's own CSS wins
		var pre []string
		for _, v := range bsVersions {
			pre = append(pre, sheet(v, base))
		}
		out = append(pre, out...)
		if tailwindAt >= 0 {
			tailwindAt += len(pre)
		}
	}
	if tailwindAt >= 0 {
		var classes []string
		var collect func(n *htmlparse.Node)
		collect = func(n *htmlparse.Node) {
			if n.Type == htmlparse.ElementNode {
				classes = append(classes, strings.Fields(n.Attr("class"))...)
			}
			for _, c := range n.Children {
				collect(c)
			}
		}
		collect(root)
		tw := tailwind.Generate(classes)
		out = append(out[:tailwindAt], append([]string{tw}, out[tailwindAt:]...)...)
	}
	return out, dets
}

// isBootstrapName matches bootstrap(.min).css of any version, so a chosen
// bundled version also replaces, say, a Bootstrap 3 link.
func isBootstrapName(href string) bool {
	h := strings.ToLower(strings.TrimSpace(href))
	if i := strings.IndexAny(h, "?#"); i >= 0 {
		h = h[:i]
	}
	b := path.Base(h)
	return b == "bootstrap.min.css" || b == "bootstrap.css"
}

// isTailwindScript recognises Tailwind's in-browser compiler: the Play CDN
// (cdn.tailwindcss.com), @tailwindcss/browser, or a local copy named
// tailwind.js / tailwindcss.js. Other "tailwind*" packages (such as
// @tailwindplus/elements) are not CSS generators.
func isTailwindScript(src string) bool {
	s := strings.ToLower(strings.TrimSpace(src))
	if s == "" {
		return false
	}
	if strings.Contains(s, "cdn.tailwindcss.com") || strings.Contains(s, "@tailwindcss/browser") {
		return true
	}
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	switch path.Base(s) {
	case "tailwind.js", "tailwind.min.js", "tailwindcss.js", "tailwindcss.min.js":
		return true
	}
	return false
}

// documentBase returns the base URL of the document (<base href> wins).
func documentBase(root *htmlparse.Node, base string) string {
	if b := root.Find("base"); b != nil && strings.TrimSpace(b.Attr("href")) != "" {
		return resource.Resolve(base, b.Attr("href"))
	}
	return base
}
