package css

import (
	"strconv"
	"strings"
)

// Env describes the output medium for @media queries: we render for
// print, on a page whose content box is Width x Height points.
type Env struct {
	Width, Height float64
}

// EvalMedia evaluates a media query list such as
// "print and (min-width: 600px), (orientation: landscape)".
func EvalMedia(list string, env Env) bool {
	list = strings.TrimSpace(strings.ToLower(list))
	if list == "" {
		return true
	}
	for _, q := range splitTopLevel(list, ',') {
		if evalQuery(strings.TrimSpace(q), env) {
			return true
		}
	}
	return false
}

func evalQuery(q string, env Env) bool {
	neg := false
	if strings.HasPrefix(q, "not ") {
		neg, q = true, strings.TrimSpace(q[4:])
	}
	q = strings.TrimPrefix(q, "only ")
	result := true
	for _, part := range splitAnd(q) {
		part = strings.TrimSpace(part)
		switch {
		case part == "":
		case strings.HasPrefix(part, "("):
			if !evalCondition(part, env) {
				result = false
			}
		default: // media type
			switch part {
			case "all", "print":
			default:
				result = false
			}
		}
	}
	return result != neg
}

// splitAnd splits on the keyword "and" outside parentheses.
func splitAnd(q string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(q); i++ {
		switch q[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && strings.HasPrefix(q[i:], " and ") {
			out = append(out, q[start:i])
			start = i + 5
			i += 4
		}
	}
	return append(out, q[start:])
}

// evalCondition evaluates "(feature: value)", "(width >= 600px)",
// "not (...)", "(a) or (b)".
func evalCondition(c string, env Env) bool {
	c = strings.TrimSpace(c)
	if strings.HasPrefix(c, "not ") {
		return !evalCondition(c[4:], env)
	}
	if parts := splitKeyword(c, " or "); len(parts) > 1 {
		for _, p := range parts {
			if evalCondition(p, env) {
				return true
			}
		}
		return false
	}
	if parts := splitKeyword(c, " and "); len(parts) > 1 {
		for _, p := range parts {
			if !evalCondition(p, env) {
				return false
			}
		}
		return true
	}
	if strings.HasPrefix(c, "(") && strings.HasSuffix(c, ")") {
		inner := strings.TrimSpace(c[1 : len(c)-1])
		if strings.HasPrefix(inner, "(") || strings.HasPrefix(inner, "not ") {
			return evalCondition(inner, env)
		}
		return evalFeature(inner, env)
	}
	return false
}

func splitKeyword(c, kw string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(c); i++ {
		switch c[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && strings.HasPrefix(c[i:], kw) {
			out = append(out, c[start:i])
			start = i + len(kw)
			i += len(kw) - 1
		}
	}
	return append(out, c[start:])
}

func mediaLength(v string) (float64, bool) {
	// em/rem in media queries are relative to the initial font size (16px).
	return ParseLength(v, LengthCtx{Em: 12, Rem: 12})
}

func evalFeature(f string, env Env) bool {
	// Range syntax: "width >= 600px", "400px <= width <= 800px".
	if strings.ContainsAny(f, "<>=") && !strings.Contains(f, ":") {
		return evalRange(f, env)
	}
	name, value, hasValue := strings.Cut(f, ":")
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	dims := map[string]float64{"width": env.Width, "height": env.Height,
		"device-width": env.Width, "device-height": env.Height}
	switch {
	case !hasValue:
		switch name {
		case "color", "grid", "monochrome":
			return name == "color"
		case "hover", "pointer", "any-hover", "any-pointer", "prefers-reduced-motion":
			return false
		}
		_, ok := dims[name]
		return ok
	case strings.HasPrefix(name, "min-") || strings.HasPrefix(name, "max-"):
		dim, ok := dims[name[4:]]
		if !ok {
			if strings.HasSuffix(name, "resolution") || strings.HasSuffix(name, "device-pixel-ratio") {
				return strings.HasPrefix(name, "min-") // assume a high resolution printer
			}
			if strings.HasSuffix(name, "aspect-ratio") {
				return compareRatio(name[:3], value, env)
			}
			return false
		}
		v, ok := mediaLength(value)
		if !ok {
			return false
		}
		// a hair of tolerance: computed page widths carry rounding noise
		if name[:3] == "min" {
			return dim >= v-0.001
		}
		return dim <= v+0.001
	}
	switch name {
	case "width", "height", "device-width", "device-height":
		v, ok := mediaLength(value)
		return ok && abs(dims[name]-v) < 0.5
	case "orientation":
		if value == "portrait" {
			return env.Height >= env.Width
		}
		return env.Width > env.Height
	case "prefers-color-scheme":
		return value == "light"
	case "prefers-reduced-motion", "prefers-contrast":
		return value == "no-preference"
	case "hover", "any-hover", "pointer", "any-pointer":
		return value == "none"
	case "scripting":
		return value == "none"
	case "color-gamut":
		return value == "srgb"
	case "display-mode":
		return value == "browser"
	}
	return false
}

func compareRatio(kind, v string, env Env) bool {
	a, b, ok := strings.Cut(v, "/")
	if !ok {
		return false
	}
	x, err1 := strconv.ParseFloat(strings.TrimSpace(a), 64)
	y, err2 := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if err1 != nil || err2 != nil || y == 0 || env.Height == 0 {
		return false
	}
	r := env.Width / env.Height
	if kind == "min" {
		return r >= x/y
	}
	return r <= x/y
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func evalRange(f string, env Env) bool {
	// tokenise into operands and operators
	var toks []string
	cur := ""
	for i := 0; i < len(f); i++ {
		c := f[i]
		if c == '<' || c == '>' || c == '=' {
			if strings.TrimSpace(cur) != "" {
				toks = append(toks, strings.TrimSpace(cur))
			}
			cur = ""
			op := string(c)
			if i+1 < len(f) && f[i+1] == '=' {
				op += "="
				i++
			}
			toks = append(toks, op)
			continue
		}
		cur += string(c)
	}
	if strings.TrimSpace(cur) != "" {
		toks = append(toks, strings.TrimSpace(cur))
	}
	value := func(t string) (float64, bool) {
		switch t {
		case "width", "device-width":
			return env.Width, true
		case "height", "device-height":
			return env.Height, true
		}
		return mediaLength(t)
	}
	if len(toks) != 3 && len(toks) != 5 {
		return false
	}
	for i := 0; i+2 < len(toks); i += 2 {
		a, ok1 := value(toks[i])
		b, ok2 := value(toks[i+2])
		if !ok1 || !ok2 {
			return false
		}
		var r bool
		switch toks[i+1] {
		case "<":
			r = a < b
		case "<=":
			r = a <= b
		case ">":
			r = a > b
		case ">=":
			r = a >= b
		case "=":
			r = abs(a-b) < 0.5
		}
		if !r {
			return false
		}
	}
	return true
}

// supportedProps lists properties the renderer understands, for @supports.
var supportedProps = map[string]bool{}

// RegisterSupported tells @supports which properties are implemented.
func RegisterSupported(props ...string) {
	for _, p := range props {
		supportedProps[p] = true
	}
}

// EvalSupports evaluates an @supports condition.
func EvalSupports(c string) bool {
	c = strings.TrimSpace(strings.ToLower(c))
	if strings.HasPrefix(c, "not ") {
		return !EvalSupports(c[4:])
	}
	if parts := splitKeyword(c, " or "); len(parts) > 1 {
		for _, p := range parts {
			if EvalSupports(p) {
				return true
			}
		}
		return false
	}
	if parts := splitKeyword(c, " and "); len(parts) > 1 {
		for _, p := range parts {
			if !EvalSupports(p) {
				return false
			}
		}
		return true
	}
	if strings.HasPrefix(c, "selector(") {
		_, err := parseSelectorList(strings.TrimSuffix(strings.TrimPrefix(c, "selector("), ")"))
		return err == nil
	}
	if strings.HasPrefix(c, "(") && strings.HasSuffix(c, ")") {
		inner := strings.TrimSpace(c[1 : len(c)-1])
		if strings.HasPrefix(inner, "(") || strings.HasPrefix(inner, "not ") {
			return EvalSupports(inner)
		}
		prop, _, ok := strings.Cut(inner, ":")
		prop = strings.TrimSpace(prop)
		return ok && (supportedProps[prop] || strings.HasPrefix(prop, "--"))
	}
	return false
}
