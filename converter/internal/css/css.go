// Package css parses stylesheets and matches them against the document:
// full selector syntax, the cascade order (specificity, source order,
// !important), @media (evaluated for print), @supports, @page,
// @font-face, CSS nesting, and value parsing (colors, lengths, calc()).
package css

import (
	"sort"
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
)

// Decl is one "property: value" pair.
type Decl struct {
	Prop      string
	Value     string // original case is kept (content strings, font names, urls)
	Important bool
}

// ParseDecls parses a declaration block ("color: red; margin: 0 !important").
func ParseDecls(s string) []Decl {
	var out []Decl
	for _, part := range splitTopLevel(stripComments(s), ';') {
		k, v, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if !strings.HasPrefix(k, "--") {
			k = strings.ToLower(k)
		}
		v = strings.TrimSpace(v)
		imp := false
		if i := strings.LastIndexByte(v, '!'); i >= 0 && strings.EqualFold(strings.TrimSpace(v[i+1:]), "important") {
			imp = true
			v = strings.TrimSpace(v[:i])
		}
		if k != "" && (v != "" || strings.HasPrefix(k, "--")) {
			out = append(out, Decl{k, v, imp})
		}
	}
	return out
}

type entry struct {
	sel   *complexSel
	decls []Decl
	order int
}

// Stylesheet is a set of rules ready for matching.
type Stylesheet struct {
	entries   []entry
	index     map[string][]int
	order     int
	Page      []Decl            // declarations of @page rules
	FontFaces [][]Decl          // one declaration list per @font-face
	PageBoxes map[string][]Decl // @page margin boxes: "top-center" -> declarations
}

// NewStylesheet returns an empty stylesheet.
func NewStylesheet() *Stylesheet { return &Stylesheet{index: map[string][]int{}} }

// ParseStylesheet parses CSS text into a new stylesheet.
func ParseStylesheet(src string, env Env) *Stylesheet {
	ss := NewStylesheet()
	ss.Add(src, env)
	return ss
}

// Add parses more CSS into the stylesheet (later rules win ties).
func (ss *Stylesheet) Add(src string, env Env) {
	ss.parse(stripComments(src), env, nil)
}

func stripComments(s string) string {
	if !strings.Contains(s, "/*") {
		return s
	}
	var sb strings.Builder
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			sb.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				sb.WriteByte(s[i])
			} else if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '*' {
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				break
			}
			i += end + 3
			continue
		}
		sb.WriteByte(c)
	}
	return sb.String()
}

// scanTo finds the first top-level byte from set, skipping quoted strings
// and parentheses. Returns -1 when not found.
func scanTo(s string, from int, set string) int {
	depth := 0
	var quote byte
	for i := from; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && strings.IndexByte(set, c) >= 0:
			return i
		}
	}
	return -1
}

// block returns the contents of the {...} starting at s[open] and the index
// of the closing brace.
func block(s string, open int) (string, int) {
	depth := 0
	var quote byte
	for i := open; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[open+1 : i], i
			}
		}
	}
	return s[open+1:], len(s)
}

// parse handles a list of rules. parents is non-nil for rules nested
// inside a style rule (CSS nesting).
func (ss *Stylesheet) parse(src string, env Env, parents []string) {
	i := 0
	for i < len(src) {
		for i < len(src) && (isWSByte(src[i]) || src[i] == ';') {
			i++
		}
		if i >= len(src) {
			return
		}
		if src[i] == '@' {
			j := scanTo(src, i, "{;")
			if j < 0 {
				return
			}
			nameEnd := i + 1
			for nameEnd < len(src) && isIdentByte(src[nameEnd]) {
				nameEnd++
			}
			name := strings.ToLower(src[i+1 : nameEnd])
			prelude := strings.TrimSpace(src[nameEnd:j])
			if src[j] == ';' {
				i = j + 1
				continue
			}
			body, end := block(src, j)
			i = end + 1
			switch name {
			case "media":
				if EvalMedia(prelude, env) {
					ss.parseBody(body, env, parents)
				}
			case "supports":
				if EvalSupports(prelude) {
					ss.parseBody(body, env, parents)
				}
			case "layer", "container", "scope", "starting-style":
				if name == "starting-style" {
					continue
				}
				ss.parseBody(body, env, parents)
			case "page":
				if prelude == "" || !strings.Contains(prelude, ":") {
					ss.Page = append(ss.Page, ParseDecls(stripNested(body))...)
					ss.parseMarginBoxes(body)
				}
			case "font-face":
				ss.FontFaces = append(ss.FontFaces, ParseDecls(body))
			}
			continue
		}
		j := scanTo(src, i, "{;}")
		if j < 0 {
			return
		}
		if src[j] != '{' { // stray declaration or garbage
			i = j + 1
			continue
		}
		prelude := strings.TrimSpace(src[i:j])
		body, end := block(src, j)
		i = end + 1
		selText := prelude
		if parents != nil {
			selText = nestSelectors(parents, prelude)
		}
		ss.addRule(selText, body, env)
	}
}

// parseBody parses the inside of a conditional group rule: inside a style
// rule it may contain declarations as well as rules.
func (ss *Stylesheet) parseBody(body string, env Env, parents []string) {
	if parents == nil {
		ss.parse(body, env, nil)
		return
	}
	ss.addRule(strings.Join(parents, ","), body, env)
}

func (ss *Stylesheet) addRule(selText, body string, env Env) {
	decls, nested := splitNested(body)
	sels, err := parseSelectorList(selText)
	if err == nil && len(decls) > 0 {
		ss.order++
		for _, s := range sels {
			ss.entries = append(ss.entries, entry{s, decls, ss.order})
			k := len(ss.entries) - 1
			key := indexKey(s)
			ss.index[key] = append(ss.index[key], k)
		}
	}
	if nested != "" {
		ss.parse(nested, env, splitTopLevel(selText, ','))
	}
}

// splitNested separates the declarations of a block from rules nested in it.
func splitNested(body string) ([]Decl, string) {
	if !strings.Contains(body, "{") {
		return ParseDecls(body), ""
	}
	var decls, nested strings.Builder
	i := 0
	for i < len(body) {
		j := scanTo(body, i, ";{")
		if j < 0 {
			decls.WriteString(body[i:])
			break
		}
		if body[j] == ';' {
			decls.WriteString(body[i : j+1])
			i = j + 1
			continue
		}
		_, end := block(body, j)
		nested.WriteString(body[i : end+1])
		nested.WriteByte('\n')
		i = end + 1
	}
	return ParseDecls(decls.String()), nested.String()
}

func stripNested(body string) string {
	d, _ := splitNested(body)
	var sb strings.Builder
	for _, x := range d {
		sb.WriteString(x.Prop + ":" + x.Value + ";")
	}
	return sb.String()
}

// nestSelectors resolves a nested selector against its parents: "&" is
// replaced by :is(parent), otherwise the rule is a descendant of it.
func nestSelectors(parents []string, child string) string {
	parent := ":is(" + strings.Join(parents, ",") + ")"
	var out []string
	for _, c := range splitTopLevel(child, ',') {
		c = strings.TrimSpace(c)
		if strings.Contains(c, "&") {
			out = append(out, strings.ReplaceAll(c, "&", parent))
		} else {
			out = append(out, parent+" "+c)
		}
	}
	return strings.Join(out, ",")
}

func indexKey(s *complexSel) string {
	last := s.parts[len(s.parts)-1]
	prefix := s.pseudoElem + "|"
	switch {
	case last.id != "":
		return prefix + "#" + last.id
	case len(last.classes) > 0:
		return prefix + "." + last.classes[0]
	case last.tag != "" && last.tag != "*":
		return prefix + last.tag
	}
	return prefix + "*"
}

// Match returns the declarations of all rules matching element n (or its
// pseudo-element when pseudo is "before"/"after"), in cascade order:
// lower specificity first, then source order. Importance is left to the
// caller, which merges several origins.
func (ss *Stylesheet) Match(n *htmlparse.Node, pseudo string) []Decl {
	if n.Type != htmlparse.ElementNode || len(ss.entries) == 0 {
		return nil
	}
	prefix := pseudo + "|"
	var cand []int
	add := func(key string) { cand = append(cand, ss.index[prefix+key]...) }
	if id := n.Attr("id"); id != "" {
		add("#" + id)
	}
	for _, c := range n.Classes() {
		add("." + c)
	}
	add(n.Tag)
	add("*")
	var hits []int
	seen := map[int]bool{}
	for _, k := range cand {
		if seen[k] {
			continue
		}
		seen[k] = true
		if ss.entries[k].sel.matches(n) {
			hits = append(hits, k)
		}
	}
	sort.Slice(hits, func(a, b int) bool {
		ea, eb := ss.entries[hits[a]], ss.entries[hits[b]]
		if ea.sel.spec != eb.sel.spec {
			return ea.sel.spec < eb.sel.spec
		}
		return ea.order < eb.order
	})
	var out []Decl
	for _, k := range hits {
		out = append(out, ss.entries[k].decls...)
	}
	return out
}

// HasPseudo reports whether any rule targets the given pseudo-element.
func (ss *Stylesheet) HasPseudo(pseudo string) bool {
	for k := range ss.index {
		if strings.HasPrefix(k, pseudo+"|") {
			return true
		}
	}
	return false
}

// parseMarginBoxes collects "@top-center { ... }" style rules nested in
// an @page rule.
func (ss *Stylesheet) parseMarginBoxes(body string) {
	_, nested := splitNested(body)
	for i := 0; i < len(nested); {
		at := strings.IndexByte(nested[i:], '@')
		if at < 0 {
			return
		}
		i += at
		open := strings.IndexByte(nested[i:], '{')
		if open < 0 {
			return
		}
		name := strings.ToLower(strings.TrimSpace(nested[i+1 : i+open]))
		inner, end := block(nested, i+open)
		if ss.PageBoxes == nil {
			ss.PageBoxes = map[string][]Decl{}
		}
		ss.PageBoxes[name] = append(ss.PageBoxes[name], ParseDecls(inner)...)
		i = end + 1
	}
}
