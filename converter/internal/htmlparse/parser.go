// Package htmlparse is a small, forgiving HTML parser written with the
// standard library only. It builds a DOM-like tree that is good enough for
// document rendering: it understands void elements, raw-text elements
// (script/style), comments, entities and the most common implicit closing
// rules (p, li, td, tr, ...).
package htmlparse

import (
	"strings"
)

// NodeType tells what a Node represents.
type NodeType int

const (
	DocumentNode NodeType = iota
	ElementNode
	TextNode
)

// Node is one element or text run in the document tree.
type Node struct {
	Type     NodeType
	Tag      string            // lower-case tag name for elements
	Attrs    map[string]string // lower-case attribute names
	Text     string            // decoded text for text nodes
	Parent   *Node
	Children []*Node
}

// Attr returns an attribute value ("" when missing).
func (n *Node) Attr(name string) string {
	if n.Attrs == nil {
		return ""
	}
	return n.Attrs[name]
}

// HasAttr reports whether the attribute is present.
func (n *Node) HasAttr(name string) bool {
	_, ok := n.Attrs[name]
	return ok
}

// Classes returns the class list of an element.
func (n *Node) Classes() []string {
	return splitSpace(n.Attr("class"))
}

// Find returns the first descendant element with the given tag.
func (n *Node) Find(tag string) *Node {
	for _, c := range n.Children {
		if c.Type == ElementNode && c.Tag == tag {
			return c
		}
		if f := c.Find(tag); f != nil {
			return f
		}
	}
	return nil
}

// FindAll returns all descendant elements with the given tag.
func (n *Node) FindAll(tag string) []*Node {
	var out []*Node
	var walk func(*Node)
	walk = func(x *Node) {
		for _, c := range x.Children {
			if c.Type == ElementNode && c.Tag == tag {
				out = append(out, c)
			}
			walk(c)
		}
	}
	walk(n)
	return out
}

// TextContent returns all text below the node, concatenated.
func (n *Node) TextContent() string {
	var b strings.Builder
	var walk func(*Node)
	walk = func(x *Node) {
		if x.Type == TextNode {
			b.WriteString(x.Text)
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

var voidElements = set("area", "base", "br", "col", "embed", "hr", "img", "input",
	"link", "meta", "param", "source", "track", "wbr", "keygen")

var rawTextElements = set("script", "style", "textarea", "title", "xmp", "noscript")

// Elements whose start tag closes an open <p>.
var closesP = set("address", "article", "aside", "blockquote", "div", "dl", "fieldset",
	"figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr",
	"main", "nav", "ol", "p", "pre", "section", "table", "ul", "figcaption", "details")

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}

func splitSpace(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
	})
}

type builder struct {
	root  *Node
	stack []*Node
}

func (b *builder) current() *Node { return b.stack[len(b.stack)-1] }

func (b *builder) appendChild(n *Node) {
	p := b.current()
	n.Parent = p
	p.Children = append(p.Children, n)
}

func (b *builder) addText(s string) {
	if s == "" {
		return
	}
	if b.current().Tag == "head" && strings.TrimSpace(s) != "" {
		b.leaveHead()
	}
	p := b.current()
	if k := len(p.Children); k > 0 && p.Children[k-1].Type == TextNode {
		p.Children[k-1].Text += s
		return
	}
	b.appendChild(&Node{Type: TextNode, Text: s})
}

// headElements may appear inside <head>; anything else (a missing </head>
// followed by <body> or content) ends the head, as in browsers.
var headElements = set("head", "html", "meta", "link", "title", "style", "script", "base", "noscript", "template")

// leaveHead closes an open <head> element and everything inside it.
func (b *builder) leaveHead() {
	for i := len(b.stack) - 1; i > 0; i-- {
		if b.stack[i].Tag == "head" {
			b.stack = b.stack[:i]
			return
		}
	}
}

// openIndex finds the topmost open element with the given tag, stopping at
// any tag in the boundary set. Returns -1 when not found.
func (b *builder) openIndex(tag string, boundary map[string]bool) int {
	for i := len(b.stack) - 1; i > 0; i-- {
		t := b.stack[i].Tag
		if t == tag {
			return i
		}
		if boundary[t] {
			return -1
		}
	}
	return -1
}

func (b *builder) popTo(i int) {
	if i > 0 {
		b.stack = b.stack[:i]
	}
}

var (
	listBoundary  = set("ul", "ol", "table", "div", "td", "th", "body", "blockquote")
	cellBoundary  = set("tr", "table")
	rowBoundary   = set("table", "thead", "tbody", "tfoot")
	sectBoundary  = set("table")
	pBoundary     = set("td", "th", "table", "li", "button", "body", "blockquote", "div", "section", "article")
	dlBoundary    = set("dl", "table", "body")
	tableSections = set("thead", "tbody", "tfoot")
)

func (b *builder) implicitClose(tag string) {
	if !headElements[tag] {
		b.leaveHead()
	}
	if closesP[tag] {
		if i := b.openIndex("p", pBoundary); i > 0 {
			b.popTo(i)
		}
	}
	switch tag {
	case "li":
		if i := b.openIndex("li", listBoundary); i > 0 {
			b.popTo(i)
		}
	case "dt", "dd":
		if i := b.openIndex("dt", dlBoundary); i > 0 {
			b.popTo(i)
		}
		if i := b.openIndex("dd", dlBoundary); i > 0 {
			b.popTo(i)
		}
	case "td", "th":
		if i := b.openIndex("td", cellBoundary); i > 0 {
			b.popTo(i)
		}
		if i := b.openIndex("th", cellBoundary); i > 0 {
			b.popTo(i)
		}
	case "tr":
		for _, t := range []string{"td", "th", "tr"} {
			if i := b.openIndex(t, rowBoundary); i > 0 {
				b.popTo(i)
			}
		}
	case "thead", "tbody", "tfoot":
		for _, t := range []string{"td", "th", "tr", "thead", "tbody", "tfoot"} {
			if i := b.openIndex(t, sectBoundary); i > 0 {
				b.popTo(i)
			}
		}
	case "option":
		if i := b.openIndex("option", set("select")); i > 0 {
			b.popTo(i)
		}
	}
	_ = tableSections
}

func (b *builder) open(tag string, attrs map[string]string, selfClosing bool) {
	b.implicitClose(tag)
	n := &Node{Type: ElementNode, Tag: tag, Attrs: attrs}
	b.appendChild(n)
	if !voidElements[tag] && !selfClosing {
		b.stack = append(b.stack, n)
	}
}

func (b *builder) close(tag string) {
	if tag == "p" && b.openIndex("p", pBoundary) < 0 {
		// A stray </p> creates an empty paragraph in browsers.
		b.appendChild(&Node{Type: ElementNode, Tag: "p"})
		return
	}
	if tag == "br" {
		b.appendChild(&Node{Type: ElementNode, Tag: "br"})
		return
	}
	for i := len(b.stack) - 1; i > 0; i-- {
		if b.stack[i].Tag == tag {
			b.stack = b.stack[:i]
			return
		}
	}
	// unmatched end tag: ignore
}

// Parse parses an HTML document and returns its root (a DocumentNode).
func Parse(src string) *Node {
	root := &Node{Type: DocumentNode}
	b := &builder{root: root, stack: []*Node{root}}
	n := len(src)
	i := 0
	for i < n {
		lt := strings.IndexByte(src[i:], '<')
		if lt < 0 {
			b.addText(DecodeEntities(src[i:]))
			break
		}
		if lt > 0 {
			b.addText(DecodeEntities(src[i : i+lt]))
			i += lt
		}
		// src[i] == '<'
		rest := src[i:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			end := strings.Index(rest[4:], "-->")
			if end < 0 {
				i = n
			} else {
				i += 4 + end + 3
			}
		case strings.HasPrefix(rest, "<!") || strings.HasPrefix(rest, "<?"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				i = n
			} else {
				i += end + 1
			}
		case strings.HasPrefix(rest, "</"):
			j := 2
			for j < len(rest) && isNameChar(rest[j]) {
				j++
			}
			name := strings.ToLower(rest[2:j])
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				i = n
			} else {
				i += end + 1
			}
			if name != "" {
				b.close(name)
			}
		case len(rest) > 1 && isLetter(rest[1]):
			tag, attrs, selfClosing, consumed := parseStartTag(rest)
			i += consumed
			b.open(tag, attrs, selfClosing)
			if rawTextElements[tag] && !selfClosing {
				// Read verbatim until the matching end tag.
				lower := strings.ToLower(src[i:])
				end := strings.Index(lower, "</"+tag)
				var raw string
				if end < 0 {
					raw = src[i:]
					i = n
				} else {
					raw = src[i : i+end]
					i += end
					if gt := strings.IndexByte(src[i:], '>'); gt >= 0 {
						i += gt + 1
					} else {
						i = n
					}
				}
				if tag == "textarea" || tag == "title" {
					raw = DecodeEntities(raw)
				}
				if raw != "" {
					b.appendChild(&Node{Type: TextNode, Text: raw})
				}
				b.close(tag)
			}
		default:
			b.addText("<")
			i++
		}
	}
	return root
}

func isLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func isNameChar(c byte) bool {
	return isLetter(c) || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == ':'
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

// parseStartTag parses "<tag attr=value ...>" at the start of s.
func parseStartTag(s string) (tag string, attrs map[string]string, selfClosing bool, consumed int) {
	j := 1
	for j < len(s) && isNameChar(s[j]) {
		j++
	}
	tag = strings.ToLower(s[1:j])
	attrs = map[string]string{}
	for j < len(s) {
		for j < len(s) && isSpace(s[j]) {
			j++
		}
		if j >= len(s) {
			break
		}
		if s[j] == '>' {
			return tag, attrs, selfClosing, j + 1
		}
		if s[j] == '/' {
			selfClosing = true
			j++
			continue
		}
		// attribute name
		k := j
		for k < len(s) && !isSpace(s[k]) && s[k] != '=' && s[k] != '>' && !(s[k] == '/' && k+1 < len(s) && s[k+1] == '>') {
			k++
		}
		name := strings.ToLower(s[j:k])
		j = k
		for j < len(s) && isSpace(s[j]) {
			j++
		}
		val := ""
		if j < len(s) && s[j] == '=' {
			j++
			for j < len(s) && isSpace(s[j]) {
				j++
			}
			if j < len(s) && (s[j] == '"' || s[j] == '\'') {
				q := s[j]
				end := strings.IndexByte(s[j+1:], q)
				if end < 0 {
					val = s[j+1:]
					j = len(s)
				} else {
					val = s[j+1 : j+1+end]
					j += end + 2
				}
			} else {
				k = j
				for k < len(s) && !isSpace(s[k]) && s[k] != '>' {
					k++
				}
				val = s[j:k]
				j = k
			}
		}
		if name != "" {
			if _, exists := attrs[name]; !exists {
				attrs[name] = DecodeEntities(val)
			}
		}
		selfClosing = false
	}
	return tag, attrs, selfClosing, len(s)
}
