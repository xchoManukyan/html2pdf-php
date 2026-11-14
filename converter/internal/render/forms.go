package render

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/css"
	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
	"github.com/xchoManukyan/html2pdf/converter/internal/pdf"
)

// Form controls are printed the way they look: inputs, checkboxes, radio
// buttons and selects become inline boxes showing their current value.

// formNode returns a stand-in node (and style) whose text is what the
// control displays, or nil when the element is not a form control.
func (e *engine) formNode(n *htmlparse.Node, st *style) (*htmlparse.Node, *style) {
	if n.Tag != "input" && n.Tag != "select" {
		return nil, nil
	}
	if fn, ok := e.formNodes[n]; ok {
		return fn, e.styles[fn]
	}
	text := ""
	cs := *st
	switch n.Tag {
	case "input":
		typ := strings.ToLower(strings.TrimSpace(n.Attr("type")))
		switch typ {
		case "checkbox":
			if n.HasAttr("checked") && !st.noAppearance {
				text = "✔"
			}
		case "radio":
			if n.HasAttr("checked") && !st.noAppearance {
				text = "●"
			}
		case "submit":
			text = orDefault(n.Attr("value"), "Submit")
		case "reset":
			text = orDefault(n.Attr("value"), "Reset")
		case "button":
			text = n.Attr("value")
		case "image":
			text = n.Attr("alt")
		case "color":
			if c, ok := css.ParseColor(orDefault(n.Attr("value"), "#000000")); ok {
				col := toPDF(c)
				cs.bg = &col
			}
			cs.widthExpr = "44px"
			cs.heightExpr = "23px"
		case "range":
			text = strings.Repeat("─", 8)
		default: // text, email, number, password, date, search, tel, url, ...
			text = n.Attr("value")
			if typ == "password" {
				text = strings.Repeat("•", len([]rune(text)))
			}
			if text == "" && n.Attr("placeholder") != "" {
				text = n.Attr("placeholder")
				cs.color.R, cs.color.G, cs.color.B = 0.46, 0.46, 0.46
			}
			if cs.widthExpr == "" {
				size, err := strconv.Atoi(n.Attr("size"))
				if err != nil || size <= 0 {
					size = 20
				}
				cs.widthExpr = fmt.Sprintf("%.2fem", float64(size)*0.55)
			}
		}
	case "select":
		var first, chosen string
		var walk func(*htmlparse.Node)
		walk = func(x *htmlparse.Node) {
			for _, c := range x.Children {
				if c.Type != htmlparse.ElementNode {
					continue
				}
				if c.Tag == "option" {
					label := strings.Join(strings.Fields(orDefault(c.Attr("label"), c.TextContent())), " ")
					if first == "" {
						first = label
					}
					if c.HasAttr("selected") && chosen == "" {
						chosen = label
					}
				}
				walk(c)
			}
		}
		walk(n)
		text = orDefault(chosen, first)
		if !st.noAppearance {
			text += "  ▾" // native arrow; with appearance:none the page draws its own
		}
	}
	fn := &htmlparse.Node{Type: htmlparse.ElementNode, Tag: "#" + n.Tag, Attrs: n.Attrs, Parent: n.Parent,
		Children: []*htmlparse.Node{{Type: htmlparse.TextNode, Text: text}}}
	e.formNodes[n] = fn
	e.styles[fn] = &cs
	return fn, &cs
}

func toPDF(c css.Color) pdf.Color { return pdf.Color{R: c.R, G: c.G, B: c.B} }

// isToggle reports checkbox and radio stand-ins, which align on their
// bottom edge whether they are checked or not.
func isToggle(n *htmlparse.Node) bool {
	if n.Tag != "#input" {
		return false
	}
	t := strings.ToLower(n.Attrs["type"])
	return t == "checkbox" || t == "radio"
}
