package render

import (
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/css"
	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
)

// @page margin boxes (CSS Paged Media): @top-left, @top-center,
// @top-right, @bottom-*, @left-*, @right-* and the four corners, with
// content such as "Page " counter(page) " of " counter(pages).

type marginBox struct {
	x, y, w, h float64
	align      string
}

func (e *engine) marginBoxGeometry(name string, pw, ph float64, m [4]float64) (marginBox, bool) {
	cw, ch := pw-m[1]-m[3], ph-m[0]-m[2]
	third, vthird := cw/3, ch/3
	switch name {
	case "top-left":
		return marginBox{m[3], 0, third, m[0], "left"}, true
	case "top-center":
		return marginBox{m[3] + third, 0, third, m[0], "center"}, true
	case "top-right":
		return marginBox{m[3] + 2*third, 0, third, m[0], "right"}, true
	case "bottom-left":
		return marginBox{m[3], ph - m[2], third, m[2], "left"}, true
	case "bottom-center":
		return marginBox{m[3] + third, ph - m[2], third, m[2], "center"}, true
	case "bottom-right":
		return marginBox{m[3] + 2*third, ph - m[2], third, m[2], "right"}, true
	case "top-left-corner":
		return marginBox{0, 0, m[3], m[0], "right"}, true
	case "top-right-corner":
		return marginBox{pw - m[1], 0, m[1], m[0], "left"}, true
	case "bottom-left-corner":
		return marginBox{0, ph - m[2], m[3], m[2], "right"}, true
	case "bottom-right-corner":
		return marginBox{pw - m[1], ph - m[2], m[1], m[2], "left"}, true
	case "left-top", "left-middle", "left-bottom", "right-top", "right-middle", "right-bottom":
		x, w := 0.0, m[3]
		if strings.HasPrefix(name, "right") {
			x, w = pw-m[1], m[1]
		}
		k := map[string]float64{"top": 0, "middle": 1, "bottom": 2}[name[strings.IndexByte(name, '-')+1:]]
		return marginBox{x, m[0] + k*vthird, w, vthird, "center"}, true
	}
	return marginBox{}, false
}

// layoutMarginBoxes paints the margin boxes on every page.
func (e *engine) layoutMarginBoxes(boxes map[string][]css.Decl, pw, ph float64, m [4]float64, base *style) {
	for name, decls := range boxes {
		geo, ok := e.marginBoxGeometry(name, pw, ph, m)
		if !ok || geo.w <= 1 || geo.h <= 1 {
			continue
		}
		st := inheritFrom(base)
		st.display = "block"
		st.align = geo.align
		st.valign = "middle"
		e.applyCascade(st, base, decls)
		if !st.hasContent || st.display == "none" {
			continue
		}
		dummy := &htmlparse.Node{Type: htmlparse.ElementNode, Tag: "#page-margin"}
		text := e.evalContent(st.content, dummy, st)
		if strings.TrimSpace(text) == "" && !st.paints() {
			continue
		}
		for p := range e.pages {
			node := &htmlparse.Node{Type: htmlparse.ElementNode, Tag: "#page-margin",
				Children: []*htmlparse.Node{{Type: htmlparse.TextNode, Text: text}}}
			e.styles[node] = st
			saved := e.pageIdx
			e.pageIdx = p
			bw := geo.w - st.margin[1] - st.margin[3]
			h, _ := e.measureBox(node, st, bw)
			y := geo.y + st.margin[0]
			free := geo.h - h - st.margin[0] - st.margin[2]
			switch st.valign {
			case "top":
			case "bottom":
				y += free
			default:
				y += free / 2
			}
			e.renderPositioned(node, st, p, geo.x+st.margin[3], y, bw, h)
			e.pageIdx = saved
		}
	}
}
