// Package tailwind generates the CSS of Tailwind CSS v3 utility classes,
// like Tailwind's browser script (the "Play CDN") does at runtime. Pages
// that style themselves with that script would otherwise print unstyled,
// because the converter does not run JavaScript.
//
// Generate covers the default theme: layout, flexbox and grid, spacing,
// sizing, typography, colors with opacity, backgrounds and gradients,
// borders and radii, shadows, opacity, transforms, tables, responsive
// (sm: md: lg: xl: 2xl: max-*:) and structural (first: last: odd: even:)
// variants, "!" importance, negative values and arbitrary values
// ("w-[37px]", "grid-cols-[2fr_1fr]", "text-[#0A458F]"). Interaction-only
// variants (hover:, focus:, dark: ...) never match on paper and are skipped.
package tailwind

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Screens are the default breakpoints in CSS pixels.
var screens = []struct {
	name string
	px   int
}{{"sm", 640}, {"md", 768}, {"lg", 1024}, {"xl", 1280}, {"2xl", 1536}}

type decl struct{ prop, value string }

type rule struct {
	group    int // 0: no media query; then breakpoints in order
	order    int // utility family, mirrors Tailwind's plugin order
	selector string
	media    string
	decls    []decl
}

// Generate returns Tailwind's base styles (preflight) followed by the
// rules for the given class names. Unknown classes are ignored.
func Generate(classes []string) string {
	seen := map[string]bool{}
	var rules []rule
	for _, c := range classes {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		if c == "container" {
			rules = append(rules, containerRules(c)...)
			continue
		}
		if r, ok := build(c); ok {
			rules = append(rules, r)
		}
	}
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].group != rules[j].group {
			return rules[i].group < rules[j].group
		}
		return rules[i].order < rules[j].order
	})
	var sb strings.Builder
	sb.WriteString(preflight)
	for _, r := range rules {
		if r.media != "" {
			sb.WriteString("@media " + r.media + " {\n")
		}
		sb.WriteString(r.selector + " {")
		for _, d := range r.decls {
			sb.WriteString(" " + d.prop + ": " + d.value + ";")
		}
		sb.WriteString(" }\n")
		if r.media != "" {
			sb.WriteString("}\n")
		}
	}
	return sb.String()
}

// Classes extracts class names from a class attribute value.
func Classes(attr string) []string { return strings.Fields(attr) }

// splitVariants splits "md:hover:px-4" at colons outside brackets.
func splitVariants(c string) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(c); i++ {
		switch c[i] {
		case '[', '(':
			depth++
		case ']', ')':
			depth--
		case ':':
			if depth == 0 {
				parts = append(parts, c[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, c[start:])
}

func build(class string) (rule, bool) {
	parts := splitVariants(class)
	util := parts[len(parts)-1]
	r := rule{}
	pseudo := ""
	var media []string
	for _, v := range parts[:len(parts)-1] {
		switch v {
		case "first":
			pseudo += ":first-child"
		case "last":
			pseudo += ":last-child"
		case "only":
			pseudo += ":only-child"
		case "odd":
			pseudo += ":nth-child(odd)"
		case "even":
			pseudo += ":nth-child(even)"
		case "empty":
			pseudo += ":empty"
		case "print":
			// always true on paper
		default:
			matched := false
			for i, s := range screens {
				if v == s.name {
					media = append(media, fmt.Sprintf("(min-width: %dpx)", s.px))
					r.group = max(r.group, i+1)
					matched = true
				} else if v == "max-"+s.name {
					media = append(media, fmt.Sprintf("(max-width: %.2fpx)", float64(s.px)-0.02))
					r.group = max(r.group, len(screens)+len(screens)-i)
					matched = true
				}
			}
			if v == "min-[" || strings.HasPrefix(v, "min-[") && strings.HasSuffix(v, "]") {
				media = append(media, "(min-width: "+arbitrary(v[4:])+")")
				r.group = max(r.group, 2*len(screens)+1)
				matched = true
			} else if strings.HasPrefix(v, "max-[") && strings.HasSuffix(v, "]") {
				media = append(media, "(max-width: "+arbitrary(v[4:])+")")
				r.group = max(r.group, 2*len(screens)+1)
				matched = true
			}
			if !matched {
				return r, false // hover:, focus:, dark:, group-*, [&...]: never apply on paper
			}
		}
	}
	important := false
	if strings.HasPrefix(util, "!") {
		important, util = true, util[1:]
	} else if strings.HasSuffix(util, "!") {
		important, util = true, util[:len(util)-1]
	}
	neg := false
	if strings.HasPrefix(util, "-") {
		neg, util = true, util[1:]
	}
	u, ok := utility(util, neg)
	if !ok || len(u.decls) == 0 {
		return r, false
	}
	if important {
		for i := range u.decls {
			u.decls[i].value += " !important"
		}
	}
	r.order = u.order
	r.decls = u.decls
	r.selector = "." + escape(class) + pseudo + u.child
	r.media = strings.Join(media, " and ")
	return r, true
}

// escape writes a class name as a CSS identifier: everything but letters,
// digits, "-" and "_" becomes a hex escape.
func escape(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		ident := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-' || c == '_' || c >= 0x80 || c >= '0' && c <= '9'
		if c >= '0' && c <= '9' && i == 0 {
			ident = false
		}
		if ident {
			sb.WriteByte(c)
		} else {
			fmt.Fprintf(&sb, "\\%x ", c)
		}
	}
	return sb.String()
}

// arbitrary decodes "[value]": underscores become spaces ("\_" stays "_").
func arbitrary(s string) string {
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == '_':
			sb.WriteByte('_')
			i++
		case s[i] == '_':
			sb.WriteByte(' ')
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String()
}

func isArbitrary(s string) bool { return strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") }

// ---------------------------------------------------------------------
// Theme values

var spacingKeys = map[string]bool{}

func init() {
	for _, k := range strings.Fields("0 px 0.5 1 1.5 2 2.5 3 3.5 4 5 6 7 8 9 10 11 12 14 16 20 24 28 32 36 40 44 48 52 56 60 64 72 80 96") {
		spacingKeys[k] = true
	}
}

func rem(v float64) string {
	if v == 0 {
		return "0px"
	}
	return strconv.FormatFloat(v, 'f', -1, 64) + "rem"
}

// spacing resolves the spacing scale ("4" -> 1rem, "px" -> 1px).
func spacing(k string) (string, bool) {
	if k == "px" {
		return "1px", true
	}
	if !spacingKeys[k] {
		return "", false
	}
	f, _ := strconv.ParseFloat(k, 64)
	return rem(f / 4), true
}

// fraction resolves "1/2" -> 50%.
func fraction(k string) (string, bool) {
	a, b, ok := strings.Cut(k, "/")
	if !ok {
		return "", false
	}
	n, err1 := strconv.Atoi(a)
	d, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || d == 0 || n <= 0 || n > d || !strings.Contains(" 2 3 4 5 6 12 ", " "+b+" ") {
		return "", false
	}
	p := float64(n) / float64(d) * 100
	return strconv.FormatFloat(math.Round(p*1e6)/1e6, 'f', -1, 64) + "%", true
}

func negate(v string, neg bool) string {
	if !neg {
		return v
	}
	if v == "0px" || v == "0" {
		return v
	}
	if strings.HasPrefix(v, "calc(") || strings.HasPrefix(v, "var(") {
		return "calc(" + v + " * -1)"
	}
	if strings.HasPrefix(v, "-") {
		return v[1:]
	}
	return "-" + v
}

// length resolves a spacing key, fraction, keyword or arbitrary value.
func length(k string, extra map[string]string) (string, bool) {
	if v, ok := extra[k]; ok {
		return v, true
	}
	if isArbitrary(k) {
		return arbitrary(k), true
	}
	if v, ok := spacing(k); ok {
		return v, true
	}
	return fraction(k)
}

var (
	insetExtra  = map[string]string{"auto": "auto", "full": "100%"}
	widthExtra  = map[string]string{"auto": "auto", "full": "100%", "screen": "100vw", "svw": "100svw", "lvw": "100lvw", "dvw": "100dvw", "min": "min-content", "max": "max-content", "fit": "fit-content"}
	heightExtra = map[string]string{"auto": "auto", "full": "100%", "screen": "100vh", "svh": "100svh", "lvh": "100lvh", "dvh": "100dvh", "min": "min-content", "max": "max-content", "fit": "fit-content"}
	maxWidth    = map[string]string{
		"none": "none", "0": "0rem", "xs": "20rem", "sm": "24rem", "md": "28rem", "lg": "32rem", "xl": "36rem",
		"2xl": "42rem", "3xl": "48rem", "4xl": "56rem", "5xl": "64rem", "6xl": "72rem", "7xl": "80rem",
		"full": "100%", "min": "min-content", "max": "max-content", "fit": "fit-content", "prose": "65ch",
		"screen-sm": "640px", "screen-md": "768px", "screen-lg": "1024px", "screen-xl": "1280px", "screen-2xl": "1536px",
	}
	fontSize = map[string][2]string{
		"xs": {"0.75rem", "1rem"}, "sm": {"0.875rem", "1.25rem"}, "base": {"1rem", "1.5rem"},
		"lg": {"1.125rem", "1.75rem"}, "xl": {"1.25rem", "1.75rem"}, "2xl": {"1.5rem", "2rem"},
		"3xl": {"1.875rem", "2.25rem"}, "4xl": {"2.25rem", "2.5rem"}, "5xl": {"3rem", "1"},
		"6xl": {"3.75rem", "1"}, "7xl": {"4.5rem", "1"}, "8xl": {"6rem", "1"}, "9xl": {"8rem", "1"},
	}
	fontWeight = map[string]string{
		"thin": "100", "extralight": "200", "light": "300", "normal": "400", "medium": "500",
		"semibold": "600", "bold": "700", "extrabold": "800", "black": "900",
	}
	leading = map[string]string{
		"none": "1", "tight": "1.25", "snug": "1.375", "normal": "1.5", "relaxed": "1.625", "loose": "2",
		"3": ".75rem", "4": "1rem", "5": "1.25rem", "6": "1.5rem", "7": "1.75rem", "8": "2rem", "9": "2.25rem", "10": "2.5rem",
	}
	tracking = map[string]string{
		"tighter": "-0.05em", "tight": "-0.025em", "normal": "0em", "wide": "0.025em", "wider": "0.05em", "widest": "0.1em",
	}
	radius = map[string]string{
		"none": "0px", "sm": "0.125rem", "": "0.25rem", "md": "0.375rem", "lg": "0.5rem", "xl": "0.75rem",
		"2xl": "1rem", "3xl": "1.5rem", "full": "9999px",
	}
	shadows = map[string]string{
		"sm":    "0 1px 2px 0 rgba(0, 0, 0, 0.05)",
		"":      "0 1px 3px 0 rgba(0, 0, 0, 0.1), 0 1px 2px -1px rgba(0, 0, 0, 0.1)",
		"md":    "0 4px 6px -1px rgba(0, 0, 0, 0.1), 0 2px 4px -2px rgba(0, 0, 0, 0.1)",
		"lg":    "0 10px 15px -3px rgba(0, 0, 0, 0.1), 0 4px 6px -4px rgba(0, 0, 0, 0.1)",
		"xl":    "0 20px 25px -5px rgba(0, 0, 0, 0.1), 0 8px 10px -6px rgba(0, 0, 0, 0.1)",
		"2xl":   "0 25px 50px -12px rgba(0, 0, 0, 0.25)",
		"inner": "inset 0 2px 4px 0 rgba(0, 0, 0, 0.05)",
		"none":  "0 0 #0000",
	}
	fontFamily = map[string]string{
		"sans":  `ui-sans-serif, system-ui, sans-serif, "Apple Color Emoji", "Segoe UI Emoji", "Segoe UI Symbol", "Noto Color Emoji"`,
		"serif": `ui-serif, Georgia, Cambria, "Times New Roman", Times, serif`,
		"mono":  `ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace`,
	}
	gradientDir = map[string]string{
		"t": "to top", "tr": "to top right", "r": "to right", "br": "to bottom right",
		"b": "to bottom", "bl": "to bottom left", "l": "to left", "tl": "to top left",
	}
)

// color resolves "blue-500", "white", "black/50", "[#0A458F]" or
// "[rgba(...)]" (with an optional "/opacity" modifier).
func color(k string) (string, bool) {
	name, alpha := k, ""
	if i := topLevelSlash(k); i > 0 {
		name, alpha = k[:i], k[i+1:]
	}
	var base string
	switch {
	case isArbitrary(name):
		v := arbitrary(name)
		if strings.HasPrefix(v, "color:") {
			v = v[6:]
		}
		if !looksLikeColor(v) {
			return "", false
		}
		base = v
	case name == "inherit", name == "transparent":
		base = name
	case name == "current":
		base = "currentColor"
	case name == "black":
		base = "#000"
	case name == "white":
		base = "#fff"
	default:
		i := strings.LastIndexByte(name, '-')
		if i < 0 {
			return "", false
		}
		shades, ok := palette[name[:i]]
		if !ok {
			return "", false
		}
		if base, ok = shades[name[i+1:]]; !ok {
			return "", false
		}
	}
	if alpha == "" {
		return base, true
	}
	a, ok := opacityValue(alpha)
	if !ok {
		return "", false
	}
	return withAlpha(base, a), true
}

// topLevelSlash finds the last "/" outside brackets and parentheses.
func topLevelSlash(s string) int {
	depth, at := 0, -1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[', '(':
			depth++
		case ']', ')':
			depth--
		case '/':
			if depth == 0 {
				at = i
			}
		}
	}
	return at
}

func opacityValue(s string) (float64, bool) {
	if isArbitrary(s) {
		v := arbitrary(s)
		if strings.HasSuffix(v, "%") {
			f, err := strconv.ParseFloat(v[:len(v)-1], 64)
			return f / 100, err == nil
		}
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 100 {
		return 0, false
	}
	return float64(n) / 100, true
}

func looksLikeColor(v string) bool {
	l := strings.ToLower(strings.TrimSpace(v))
	if strings.HasPrefix(l, "#") {
		return true
	}
	for _, f := range []string{"rgb(", "rgba(", "hsl(", "hsla(", "hwb(", "color(", "oklch(", "lab(", "lch(", "var("} {
		if strings.HasPrefix(l, f) {
			return true
		}
	}
	return namedColors[l]
}

var namedColors = map[string]bool{
	"black": true, "white": true, "red": true, "green": true, "blue": true, "gray": true, "grey": true,
	"yellow": true, "orange": true, "purple": true, "pink": true, "brown": true, "navy": true, "teal": true,
	"silver": true, "maroon": true, "olive": true, "lime": true, "aqua": true, "fuchsia": true,
	"transparent": true, "currentcolor": true, "inherit": true,
}

// withAlpha applies an opacity to a hex color; other colors are mixed with
// color-mix-free fallbacks where possible.
func withAlpha(c string, a float64) string {
	if r, g, b, ok := hexRGB(c); ok {
		return fmt.Sprintf("rgba(%d, %d, %d, %s)", r, g, b, strconv.FormatFloat(a, 'f', -1, 64))
	}
	return c
}

func hexRGB(c string) (r, g, b int, ok bool) {
	if !strings.HasPrefix(c, "#") {
		return 0, 0, 0, false
	}
	h := c[1:]
	if len(h) == 3 || len(h) == 4 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 && len(h) != 8 {
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(h[:6], 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(v >> 16 & 255), int(v >> 8 & 255), int(v & 255), true
}

// transparentOf returns the color with zero alpha, used as the implicit
// end of a gradient that only has a "from" color.
func transparentOf(c string) string {
	if r, g, b, ok := hexRGB(c); ok {
		return fmt.Sprintf("rgba(%d, %d, %d, 0)", r, g, b)
	}
	return "transparent"
}

// isLength tells whether an arbitrary value is a size (for "text-[11px]"
// versus "text-[#333]").
func isLength(v string) bool {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "length:") {
		return true
	}
	if strings.HasPrefix(v, "calc(") || strings.HasPrefix(v, "clamp(") || strings.HasPrefix(v, "min(") || strings.HasPrefix(v, "max(") {
		return true
	}
	if v == "" || !(v[0] >= '0' && v[0] <= '9' || v[0] == '.' || v[0] == '-') {
		return false
	}
	return true
}
