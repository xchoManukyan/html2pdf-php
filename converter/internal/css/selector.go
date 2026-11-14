package css

import (
	"errors"
	"strconv"
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
)

// Selectors Level 4 subset: type, universal, #id, .class, [attr] with all
// operators, structural pseudo-classes, :not/:is/:where/:has, combinators
// (descendant, >, +, ~) and the ::before / ::after pseudo-elements.

type attrSel struct {
	name, op, val string
	ci            bool
}

type pseudoSel struct {
	name string
	a, b int           // an+b for nth-*
	list []*complexSel // :not / :is / :where / :has arguments
	rel  []byte        // :has leading combinators, parallel to list
}

type compound struct {
	tag     string
	id      string
	classes []string
	attrs   []attrSel
	pseudos []pseudoSel
	never   bool // dynamic state (:hover, ...) that never applies to print
}

type complexSel struct {
	parts      []compound
	combs      []byte // combs[i] joins parts[i] and parts[i+1]
	pseudoElem string // "before", "after", ... ("" for the element itself)
	spec       int
}

var errSelector = errors.New("css: invalid selector")

type selParser struct {
	s string
	i int
}

func (p *selParser) peek() byte {
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

func (p *selParser) skipWS() bool {
	start := p.i
	for p.i < len(p.s) && isWSByte(p.s[p.i]) {
		p.i++
	}
	return p.i > start
}

func isWSByte(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func isIdentByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c >= 0x80
}

func (p *selParser) ident() string {
	var sb strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == '\\' && p.i+1 < len(p.s):
			p.i++
			hex := 0
			for hex < 6 && p.i+hex < len(p.s) && strings.IndexByte("0123456789abcdefABCDEF", p.s[p.i+hex]) >= 0 {
				hex++
			}
			if hex > 0 {
				v, _ := strconv.ParseUint(p.s[p.i:p.i+hex], 16, 32)
				sb.WriteRune(rune(v))
				p.i += hex
				if p.i < len(p.s) && p.s[p.i] == ' ' {
					p.i++
				}
			} else {
				sb.WriteByte(p.s[p.i])
				p.i++
			}
		case isIdentByte(c):
			sb.WriteByte(c)
			p.i++
		default:
			return sb.String()
		}
	}
	return sb.String()
}

// balanced reads "( ... )" and returns the inside.
func (p *selParser) balanced() (string, error) {
	if p.peek() != '(' {
		return "", errSelector
	}
	depth, start := 0, p.i+1
	var quote byte
	for ; p.i < len(p.s); p.i++ {
		c := p.s[p.i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				p.i++
				return p.s[start : p.i-1], nil
			}
		}
	}
	return "", errSelector
}

// ParseSelectorList parses a comma separated selector list. It fails if any
// selector is invalid (like browsers, which then drop the whole rule).
func parseSelectorList(s string) ([]*complexSel, error) {
	var out []*complexSel
	for _, part := range splitTopLevel(s, ',') {
		c, err := parseComplexSel(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, errSelector
	}
	return out, nil
}

func parseComplexSel(s string) (*complexSel, error) {
	if s == "" {
		return nil, errSelector
	}
	p := &selParser{s: s}
	c := &complexSel{}
	p.skipWS()
	for {
		comp, pe, err := p.compound()
		if err != nil {
			return nil, err
		}
		if c.pseudoElem != "" {
			return nil, errSelector // nothing may follow a pseudo-element
		}
		c.parts = append(c.parts, comp)
		c.pseudoElem = pe
		ws := p.skipWS()
		if p.i >= len(p.s) {
			break
		}
		switch ch := p.peek(); ch {
		case '>', '+', '~':
			p.i++
			p.skipWS()
			c.combs = append(c.combs, ch)
		default:
			if !ws {
				return nil, errSelector
			}
			c.combs = append(c.combs, ' ')
		}
	}
	c.spec = c.specificity()
	return c, nil
}

var neverPseudo = map[string]bool{
	"hover": true, "active": true, "focus": true, "focus-within": true, "focus-visible": true,
	"visited": true, "target": true, "target-within": true, "indeterminate": true,
	"invalid": true, "user-invalid": true, "autofill": true,
	"fullscreen": true, "modal": true, "popover-open": true, "playing": true, "paused": true,
	"current": true, "past": true, "future": true,
	"-webkit-autofill": true, "-moz-focusring": true, "defined": false,
}

var simplePseudo = map[string]bool{
	"first-child": true, "last-child": true, "only-child": true, "first-of-type": true,
	"last-of-type": true, "only-of-type": true, "empty": true, "root": true, "link": true,
	"any-link": true, "enabled": true, "disabled": true, "required": true, "optional": true,
	"read-only": true, "valid": true, "defined": true, "first": true, "left": true, "right": true,
	"scope": true, "blank": true,
	"checked": true, "default": true, "placeholder-shown": true, "read-write": true,
}

func (p *selParser) compound() (compound, string, error) {
	var c compound
	pseudoElem := ""
	start := p.i
	switch ch := p.peek(); {
	case ch == '*':
		c.tag = "*"
		p.i++
	case isIdentByte(ch) || ch == '\\':
		c.tag = strings.ToLower(p.ident())
	}
	// namespace prefix "svg|circle" or "*|a" -> drop the namespace
	if p.peek() == '|' {
		p.i++
		c.tag = strings.ToLower(p.ident())
	}
loop:
	for p.i < len(p.s) {
		switch p.peek() {
		case '#':
			p.i++
			c.id = p.ident()
			if c.id == "" {
				return c, "", errSelector
			}
		case '.':
			p.i++
			cl := p.ident()
			if cl == "" {
				return c, "", errSelector
			}
			c.classes = append(c.classes, cl)
		case '[':
			end := strings.IndexByte(p.s[p.i:], ']')
			if end < 0 {
				return c, "", errSelector
			}
			a, err := parseAttrSel(p.s[p.i+1 : p.i+end])
			if err != nil {
				return c, "", err
			}
			c.attrs = append(c.attrs, a)
			p.i += end + 1
		case ':':
			p.i++
			double := p.peek() == ':'
			if double {
				p.i++
			}
			name := strings.ToLower(p.ident())
			if double || name == "before" || name == "after" || name == "first-line" || name == "first-letter" {
				pseudoElem = name
				if p.peek() == '(' { // ::part(), ::slotted() ...
					if _, err := p.balanced(); err != nil {
						return c, "", err
					}
					c.never = true
				}
				switch name {
				case "before", "after", "marker", "first-line", "first-letter":
				default:
					c.never = true // ::placeholder, ::selection, ... never rendered
				}
				continue
			}
			ps := pseudoSel{name: name}
			if p.peek() == '(' {
				arg, err := p.balanced()
				if err != nil {
					return c, "", err
				}
				if err := ps.parseArg(arg); err != nil {
					return c, "", err
				}
			} else if neverPseudo[name] {
				c.never = true
				continue
			} else if !simplePseudo[name] {
				return c, "", errSelector
			}
			c.pseudos = append(c.pseudos, ps)
		default:
			break loop
		}
	}
	if p.i == start {
		return c, "", errSelector
	}
	return c, pseudoElem, nil
}

func (ps *pseudoSel) parseArg(arg string) error {
	switch ps.name {
	case "nth-child", "nth-last-child", "nth-of-type", "nth-last-of-type":
		if i := strings.Index(strings.ToLower(arg), " of "); i >= 0 {
			arg = arg[:i]
		}
		a, b, ok := parseNth(arg)
		if !ok {
			return errSelector
		}
		ps.a, ps.b = a, b
	case "not", "is", "where", "matches", "any", "-webkit-any":
		for _, part := range splitTopLevel(arg, ',') {
			c, err := parseComplexSel(strings.TrimSpace(part))
			if err != nil {
				if ps.name == "not" {
					return err
				}
				continue // :is() and :where() are forgiving
			}
			ps.list = append(ps.list, c)
		}
		if ps.name == "matches" || ps.name == "any" || ps.name == "-webkit-any" {
			ps.name = "is"
		}
	case "has":
		for _, part := range splitTopLevel(arg, ',') {
			part = strings.TrimSpace(part)
			comb := byte(' ')
			if part != "" && strings.IndexByte(">+~", part[0]) >= 0 {
				comb = part[0]
				part = strings.TrimSpace(part[1:])
			}
			c, err := parseComplexSel(part)
			if err != nil {
				return err
			}
			ps.list = append(ps.list, c)
			ps.rel = append(ps.rel, comb)
		}
	case "lang", "dir", "host", "host-context", "state":
		ps.name = "never"
	default:
		return errSelector
	}
	return nil
}

func parseNth(s string) (int, int, bool) {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	switch s {
	case "odd":
		return 2, 1, true
	case "even":
		return 2, 0, true
	}
	n := strings.IndexByte(s, 'n')
	if n < 0 {
		b, err := strconv.Atoi(s)
		return 0, b, err == nil
	}
	a := 1
	switch as := s[:n]; as {
	case "", "+":
	case "-":
		a = -1
	default:
		v, err := strconv.Atoi(as)
		if err != nil {
			return 0, 0, false
		}
		a = v
	}
	b := 0
	if rest := s[n+1:]; rest != "" {
		v, err := strconv.Atoi(rest)
		if err != nil {
			return 0, 0, false
		}
		b = v
	}
	return a, b, true
}

func parseAttrSel(s string) (attrSel, error) {
	s = strings.TrimSpace(s)
	var a attrSel
	opIdx := strings.IndexAny(s, "=~|^$*")
	if opIdx < 0 {
		a.name = strings.ToLower(strings.TrimSpace(s))
		if i := strings.IndexByte(a.name, '|'); i >= 0 {
			a.name = a.name[i+1:]
		}
		return a, nil
	}
	a.name = strings.ToLower(strings.TrimSpace(s[:opIdx]))
	rest := s[opIdx:]
	if rest[0] == '=' {
		a.op = "="
		rest = rest[1:]
	} else if len(rest) >= 2 && rest[1] == '=' {
		a.op = rest[:2]
		rest = rest[2:]
	} else {
		return a, errSelector
	}
	rest = strings.TrimSpace(rest)
	if l := len(rest); l >= 2 && (rest[l-1] == 'i' || rest[l-1] == 'I' || rest[l-1] == 's') && isWSByte(rest[l-2]) {
		a.ci = rest[l-1] != 's'
		rest = strings.TrimSpace(rest[:l-1])
	}
	if len(rest) >= 2 && (rest[0] == '"' || rest[0] == '\'') && rest[len(rest)-1] == rest[0] {
		rest = rest[1 : len(rest)-1]
	}
	a.val = rest
	if a.name == "" {
		return a, errSelector
	}
	return a, nil
}

// Specificity packs (ids, classes, types) into one comparable int.
func (c *complexSel) specificity() int {
	sp := 0
	for _, p := range c.parts {
		sp += p.specificity()
	}
	if c.pseudoElem != "" {
		sp++
	}
	return sp
}

func (c compound) specificity() int {
	sp := 0
	if c.id != "" {
		sp += 10000
	}
	sp += 100 * (len(c.classes) + len(c.attrs))
	if c.tag != "" && c.tag != "*" {
		sp++
	}
	for _, p := range c.pseudos {
		switch p.name {
		case "where":
		case "is", "not", "has":
			best := 0
			for _, s := range p.list {
				best = max(best, s.spec)
			}
			sp += best
		default:
			sp += 100
		}
	}
	return sp
}

// ---------------------------------------------------------------------
// Matching

func isElem(n *htmlparse.Node) bool { return n != nil && n.Type == htmlparse.ElementNode }

func parentElem(n *htmlparse.Node) *htmlparse.Node {
	if isElem(n.Parent) {
		return n.Parent
	}
	return nil
}

func siblings(n *htmlparse.Node) []*htmlparse.Node {
	if n.Parent == nil {
		return []*htmlparse.Node{n}
	}
	var out []*htmlparse.Node
	for _, c := range n.Parent.Children {
		if isElem(c) {
			out = append(out, c)
		}
	}
	return out
}

func (c *complexSel) matches(n *htmlparse.Node) bool {
	return c.matchAt(len(c.parts)-1, n)
}

func (c *complexSel) matchAt(i int, n *htmlparse.Node) bool {
	if !isElem(n) || !c.parts[i].matches(n) {
		return false
	}
	if i == 0 {
		return true
	}
	switch c.combs[i-1] {
	case ' ':
		for a := parentElem(n); a != nil; a = parentElem(a) {
			if c.matchAt(i-1, a) {
				return true
			}
		}
	case '>':
		return c.matchAt(i-1, parentElem(n))
	case '+':
		return c.matchAt(i-1, prevSibling(n))
	case '~':
		for s := prevSibling(n); s != nil; s = prevSibling(s) {
			if c.matchAt(i-1, s) {
				return true
			}
		}
	}
	return false
}

func prevSibling(n *htmlparse.Node) *htmlparse.Node {
	var prev *htmlparse.Node
	if n.Parent == nil {
		return nil
	}
	for _, c := range n.Parent.Children {
		if c == n {
			return prev
		}
		if isElem(c) {
			prev = c
		}
	}
	return nil
}

func (c compound) matches(n *htmlparse.Node) bool {
	if c.never {
		return false
	}
	if c.tag != "" && c.tag != "*" && c.tag != n.Tag {
		return false
	}
	if c.id != "" && n.Attr("id") != c.id {
		return false
	}
	if len(c.classes) > 0 {
		have := n.Classes()
		for _, want := range c.classes {
			found := false
			for _, h := range have {
				if h == want {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	for _, a := range c.attrs {
		if !a.matches(n) {
			return false
		}
	}
	for _, p := range c.pseudos {
		if !p.matches(n) {
			return false
		}
	}
	return true
}

func (a attrSel) matches(n *htmlparse.Node) bool {
	v, ok := n.Attrs[a.name]
	if !ok {
		return false
	}
	want := a.val
	if a.ci {
		v, want = strings.ToLower(v), strings.ToLower(want)
	}
	switch a.op {
	case "":
		return true
	case "=":
		return v == want
	case "~=":
		for _, f := range strings.Fields(v) {
			if f == want {
				return true
			}
		}
		return false
	case "|=":
		return v == want || strings.HasPrefix(v, want+"-")
	case "^=":
		return want != "" && strings.HasPrefix(v, want)
	case "$=":
		return want != "" && strings.HasSuffix(v, want)
	case "*=":
		return want != "" && strings.Contains(v, want)
	}
	return false
}

func nthMatch(a, b, k int) bool {
	if a == 0 {
		return k == b
	}
	d := k - b
	return d%a == 0 && d/a >= 0
}

func (p pseudoSel) matches(n *htmlparse.Node) bool {
	switch p.name {
	case "first-child", "last-child", "only-child", "nth-child", "nth-last-child":
		sib := siblings(n)
		idx := indexOf(sib, n)
		switch p.name {
		case "first-child":
			return idx == 0
		case "last-child":
			return idx == len(sib)-1
		case "only-child":
			return len(sib) == 1
		case "nth-child":
			return nthMatch(p.a, p.b, idx+1)
		default:
			return nthMatch(p.a, p.b, len(sib)-idx)
		}
	case "first-of-type", "last-of-type", "only-of-type", "nth-of-type", "nth-last-of-type":
		var same []*htmlparse.Node
		for _, s := range siblings(n) {
			if s.Tag == n.Tag {
				same = append(same, s)
			}
		}
		idx := indexOf(same, n)
		switch p.name {
		case "first-of-type":
			return idx == 0
		case "last-of-type":
			return idx == len(same)-1
		case "only-of-type":
			return len(same) == 1
		case "nth-of-type":
			return nthMatch(p.a, p.b, idx+1)
		default:
			return nthMatch(p.a, p.b, len(same)-idx)
		}
	case "empty", "blank":
		for _, c := range n.Children {
			if isElem(c) || c.Type == htmlparse.TextNode && (p.name == "empty" || strings.TrimSpace(c.Text) != "") {
				return false
			}
		}
		return true
	case "root":
		return n.Parent != nil && n.Parent.Type == htmlparse.DocumentNode
	case "scope":
		return n.Tag == "html"
	case "link", "any-link":
		return (n.Tag == "a" || n.Tag == "area") && n.HasAttr("href")
	case "disabled":
		return n.HasAttr("disabled")
	case "enabled":
		return !n.HasAttr("disabled") && (n.Tag == "input" || n.Tag == "button" || n.Tag == "select" || n.Tag == "textarea")
	case "required":
		return n.HasAttr("required")
	case "optional":
		return !n.HasAttr("required") && (n.Tag == "input" || n.Tag == "select" || n.Tag == "textarea")
	case "read-only":
		return n.HasAttr("readonly") || !(n.Tag == "input" || n.Tag == "textarea")
	// states written in the markup: checked boxes and selected options
	case "checked":
		return (n.Tag == "input" && n.HasAttr("checked")) || (n.Tag == "option" && n.HasAttr("selected"))
	case "default":
		return (n.Tag == "input" && n.HasAttr("checked")) || (n.Tag == "option" && n.HasAttr("selected"))
	case "placeholder-shown":
		return (n.Tag == "input" || n.Tag == "textarea") && n.HasAttr("placeholder") && n.Attr("value") == "" && strings.TrimSpace(n.TextContent()) == ""
	case "read-write":
		return (n.Tag == "input" || n.Tag == "textarea") && !n.HasAttr("readonly") && !n.HasAttr("disabled")
	case "valid", "defined":
		return true
	case "not":
		for _, s := range p.list {
			if s.pseudoElem == "" && s.matches(n) {
				return false
			}
		}
		return true
	case "is", "where":
		for _, s := range p.list {
			if s.pseudoElem == "" && s.matches(n) {
				return true
			}
		}
		return false
	case "has":
		for i, s := range p.list {
			if hasRelative(n, p.rel[i], s) {
				return true
			}
		}
		return false
	}
	return false
}

func indexOf(list []*htmlparse.Node, n *htmlparse.Node) int {
	for i, x := range list {
		if x == n {
			return i
		}
	}
	return -1
}

func hasRelative(anchor *htmlparse.Node, comb byte, s *complexSel) bool {
	switch comb {
	case '>':
		for _, c := range anchor.Children {
			if isElem(c) && s.matches(c) {
				return true
			}
		}
	case '+':
		sib := siblings(anchor)
		if i := indexOf(sib, anchor); i >= 0 && i+1 < len(sib) {
			return s.matches(sib[i+1])
		}
	case '~':
		sib := siblings(anchor)
		for _, x := range sib[indexOf(sib, anchor)+1:] {
			if s.matches(x) {
				return true
			}
		}
	default:
		var walk func(*htmlparse.Node) bool
		walk = func(x *htmlparse.Node) bool {
			for _, c := range x.Children {
				if isElem(c) && (s.matches(c) || walk(c)) {
					return true
				}
			}
			return false
		}
		return walk(anchor)
	}
	return false
}

// splitTopLevel splits s on sep outside of parentheses, brackets and quotes.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
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
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case c == sep && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
