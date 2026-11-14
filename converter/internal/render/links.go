package render

import (
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
	"github.com/xchoManukyan/html2pdf/converter/internal/pdf"
)

// anchor registers an element's id (or <a name>) as a link destination.
func (e *engine) anchor(n *htmlparse.Node, y float64) {
	if e.dry || n.Type != htmlparse.ElementNode {
		return
	}
	for _, name := range []string{n.Attr("id"), map[bool]string{true: n.Attr("name")}[n.Tag == "a"]} {
		if name != "" {
			e.doc.AddDest(name, e.pageIdx, y)
		}
	}
}

// addLink makes a rectangle clickable: external URLs open in a browser,
// "#id" jumps inside the document.
func (e *engine) addLink(p *pdf.Page, href string, x, y, w, h float64) {
	l := strings.TrimSpace(href)
	lower := strings.ToLower(l)
	switch {
	case l == "" || e.dry:
	case strings.HasPrefix(l, "#") && len(l) > 1:
		p.LinkDest(x, y, w, h, l[1:])
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "mailto:"), strings.HasPrefix(lower, "tel:"):
		p.Link(x, y, w, h, l)
	}
}

// documentMeta copies <meta> information into the PDF properties.
func documentMeta(root *htmlparse.Node, doc *pdf.Document) {
	for _, m := range root.FindAll("meta") {
		content := strings.TrimSpace(m.Attr("content"))
		if content == "" {
			continue
		}
		switch strings.ToLower(m.Attr("name")) {
		case "author":
			doc.Author = content
		case "description", "subject":
			doc.Subject = content
		case "keywords":
			doc.Keywords = content
		case "generator", "creator":
			doc.Creator = content
		}
	}
}

// urlOf extracts the reference from url("...").
func urlOf(v string) string {
	v = strings.TrimSpace(v)
	if open := strings.IndexByte(v, '('); open >= 0 && strings.HasSuffix(v, ")") {
		v = v[open+1 : len(v)-1]
	}
	return strings.Trim(strings.TrimSpace(v), `"'`)
}
