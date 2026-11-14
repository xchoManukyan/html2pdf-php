package tailwind

import (
	"strconv"
	"strings"
)

type util struct {
	decls []decl
	order int
	child string // selector suffix (space-x, divide)
}

// Family order, following Tailwind's core plugin order so that later
// families win ties the same way.
const (
	oContainer = iota
	oSrOnly
	oPointer
	oVisibility
	oPosition
	oInset
	oZ
	oOrder
	oGridCol
	oGridRow
	oFloat
	oClear
	oMargin
	oBoxSizing
	oLineClamp
	oDisplay
	oAspect
	oSize
	oHeight
	oMaxHeight
	oMinHeight
	oWidth
	oMinWidth
	oMaxWidth
	oFlex
	oShrink
	oGrow
	oBasis
	oTableLayout
	oBorderCollapse
	oBorderSpacing
	oOrigin
	oTransform
	oCursor
	oSelect
	oResize
	oListPos
	oListType
	oGridCols
	oGridRows
	oAutoRows
	oGridFlow
	oFlexDir
	oFlexWrap
	oPlace
	oAlignContent
	oAlignItems
	oJustify
	oJustifyItems
	oGap
	oSpace
	oDivide
	oSelf
	oJustifySelf
	oOverflow
	oTruncate
	oWhitespace
	oBreak
	oRounded
	oBorderWidth
	oBorderStyle
	oBorderColor
	oBgColor
	oBgImage
	oGradient
	oBgSize
	oBgPos
	oBgRepeat
	oObject
	oPadding
	oTextAlign
	oIndent
	oVAlign
	oFontFamily
	oFontSize
	oFontWeight
	oTransformCase
	oFontStyle
	oLeading
	oTracking
	oTextColor
	oDecoration
	oOpacity
	oShadow
	oOutline
)

func one(order int, prop, value string) (util, bool) {
	return util{decls: []decl{{prop, value}}, order: order}, true
}

func many(order int, kv ...string) (util, bool) {
	u := util{order: order}
	for i := 0; i+1 < len(kv); i += 2 {
		u.decls = append(u.decls, decl{kv[i], kv[i+1]})
	}
	return u, true
}

var displays = map[string]string{
	"block": "block", "inline-block": "inline-block", "inline": "inline", "flex": "flex",
	"inline-flex": "inline-flex", "table": "table", "inline-table": "inline-table",
	"table-caption": "table-caption", "table-cell": "table-cell", "table-column": "table-column",
	"table-column-group": "table-column-group", "table-footer-group": "table-footer-group",
	"table-header-group": "table-header-group", "table-row-group": "table-row-group",
	"table-row": "table-row", "flow-root": "flow-root", "grid": "grid", "inline-grid": "inline-grid",
	"contents": "contents", "list-item": "list-item", "hidden": "none",
}

var keywordUtils = map[string][]string{
	"static": {"position", "static"}, "fixed": {"position", "fixed"}, "absolute": {"position", "absolute"},
	"relative": {"position", "relative"}, "sticky": {"position", "sticky"},
	"visible": {"visibility", "visible"}, "invisible": {"visibility", "hidden"}, "collapse": {"visibility", "collapse"},
	"float-left": {"float", "left"}, "float-right": {"float", "right"}, "float-none": {"float", "none"},
	"float-start": {"float", "inline-start"}, "float-end": {"float", "inline-end"},
	"clear-left": {"clear", "left"}, "clear-right": {"clear", "right"}, "clear-both": {"clear", "both"}, "clear-none": {"clear", "none"},
	"box-border": {"box-sizing", "border-box"}, "box-content": {"box-sizing", "content-box"},
	"table-auto": {"table-layout", "auto"}, "table-fixed": {"table-layout", "fixed"},
	"border-collapse": {"border-collapse", "collapse"}, "border-separate": {"border-collapse", "separate"},
	"list-inside": {"list-style-position", "inside"}, "list-outside": {"list-style-position", "outside"},
	"list-none": {"list-style-type", "none"}, "list-disc": {"list-style-type", "disc"}, "list-decimal": {"list-style-type", "decimal"},
	"flex-row": {"flex-direction", "row"}, "flex-row-reverse": {"flex-direction", "row-reverse"},
	"flex-col": {"flex-direction", "column"}, "flex-col-reverse": {"flex-direction", "column-reverse"},
	"flex-wrap": {"flex-wrap", "wrap"}, "flex-wrap-reverse": {"flex-wrap", "wrap-reverse"}, "flex-nowrap": {"flex-wrap", "nowrap"},
	"flex-1": {"flex", "1 1 0%"}, "flex-auto": {"flex", "1 1 auto"}, "flex-initial": {"flex", "0 1 auto"}, "flex-none": {"flex", "none"},
	"grow": {"flex-grow", "1"}, "grow-0": {"flex-grow", "0"}, "flex-grow": {"flex-grow", "1"}, "flex-grow-0": {"flex-grow", "0"},
	"shrink": {"flex-shrink", "1"}, "shrink-0": {"flex-shrink", "0"}, "flex-shrink": {"flex-shrink", "1"}, "flex-shrink-0": {"flex-shrink", "0"},
	"grid-flow-row": {"grid-auto-flow", "row"}, "grid-flow-col": {"grid-auto-flow", "column"}, "grid-flow-dense": {"grid-auto-flow", "dense"},
	"grid-flow-row-dense": {"grid-auto-flow", "row dense"}, "grid-flow-col-dense": {"grid-auto-flow", "column dense"},
	"auto-rows-auto": {"grid-auto-rows", "auto"}, "auto-rows-min": {"grid-auto-rows", "min-content"},
	"auto-rows-max": {"grid-auto-rows", "max-content"}, "auto-rows-fr": {"grid-auto-rows", "minmax(0, 1fr)"},
	"auto-cols-auto": {"grid-auto-columns", "auto"}, "auto-cols-min": {"grid-auto-columns", "min-content"},
	"auto-cols-max": {"grid-auto-columns", "max-content"}, "auto-cols-fr": {"grid-auto-columns", "minmax(0, 1fr)"},
	"overflow-auto": {"overflow", "auto"}, "overflow-hidden": {"overflow", "hidden"}, "overflow-clip": {"overflow", "clip"},
	"overflow-visible": {"overflow", "visible"}, "overflow-scroll": {"overflow", "scroll"},
	"overflow-x-auto": {"overflow-x", "auto"}, "overflow-y-auto": {"overflow-y", "auto"},
	"overflow-x-hidden": {"overflow-x", "hidden"}, "overflow-y-hidden": {"overflow-y", "hidden"},
	"overflow-x-clip": {"overflow-x", "clip"}, "overflow-y-clip": {"overflow-y", "clip"},
	"overflow-x-visible": {"overflow-x", "visible"}, "overflow-y-visible": {"overflow-y", "visible"},
	"overflow-x-scroll": {"overflow-x", "scroll"}, "overflow-y-scroll": {"overflow-y", "scroll"},
	"whitespace-normal": {"white-space", "normal"}, "whitespace-nowrap": {"white-space", "nowrap"},
	"whitespace-pre": {"white-space", "pre"}, "whitespace-pre-line": {"white-space", "pre-line"},
	"whitespace-pre-wrap": {"white-space", "pre-wrap"}, "whitespace-break-spaces": {"white-space", "break-spaces"},
	"break-words": {"overflow-wrap", "break-word"}, "break-all": {"word-break", "break-all"}, "break-keep": {"word-break", "keep-all"},
	"text-left": {"text-align", "left"}, "text-center": {"text-align", "center"}, "text-right": {"text-align", "right"},
	"text-justify": {"text-align", "justify"}, "text-start": {"text-align", "start"}, "text-end": {"text-align", "end"},
	"italic": {"font-style", "italic"}, "not-italic": {"font-style", "normal"},
	"uppercase": {"text-transform", "uppercase"}, "lowercase": {"text-transform", "lowercase"},
	"capitalize": {"text-transform", "capitalize"}, "normal-case": {"text-transform", "none"},
	"underline": {"text-decoration-line", "underline"}, "overline": {"text-decoration-line", "overline"},
	"line-through": {"text-decoration-line", "line-through"}, "no-underline": {"text-decoration-line", "none"},
	"text-ellipsis": {"text-overflow", "ellipsis"}, "text-clip": {"text-overflow", "clip"},
	"align-baseline": {"vertical-align", "baseline"}, "align-top": {"vertical-align", "top"},
	"align-middle": {"vertical-align", "middle"}, "align-bottom": {"vertical-align", "bottom"},
	"align-text-top": {"vertical-align", "text-top"}, "align-text-bottom": {"vertical-align", "text-bottom"},
	"align-sub": {"vertical-align", "sub"}, "align-super": {"vertical-align", "super"},
	"bg-cover": {"background-size", "cover"}, "bg-contain": {"background-size", "contain"}, "bg-auto": {"background-size", "auto"},
	"bg-repeat": {"background-repeat", "repeat"}, "bg-no-repeat": {"background-repeat", "no-repeat"},
	"bg-repeat-x": {"background-repeat", "repeat-x"}, "bg-repeat-y": {"background-repeat", "repeat-y"},
	"bg-none":      {"background-image", "none"},
	"border-solid": {"border-style", "solid"}, "border-dashed": {"border-style", "dashed"}, "border-dotted": {"border-style", "dotted"},
	"border-double": {"border-style", "double"}, "border-hidden": {"border-style", "hidden"}, "border-none": {"border-style", "none"},
	"object-contain": {"object-fit", "contain"}, "object-cover": {"object-fit", "cover"}, "object-fill": {"object-fit", "fill"},
	"object-none": {"object-fit", "none"}, "object-scale-down": {"object-fit", "scale-down"},
	"pointer-events-none": {"pointer-events", "none"}, "pointer-events-auto": {"pointer-events", "auto"},
	"resize-none": {"resize", "none"}, "resize": {"resize", "both"}, "resize-x": {"resize", "horizontal"}, "resize-y": {"resize", "vertical"},
	"select-none": {"user-select", "none"}, "select-text": {"user-select", "text"}, "select-all": {"user-select", "all"}, "select-auto": {"user-select", "auto"},
	"aspect-auto": {"aspect-ratio", "auto"}, "aspect-square": {"aspect-ratio", "1 / 1"}, "aspect-video": {"aspect-ratio", "16 / 9"},
	"font-sans": {"font-family", fontFamily["sans"]}, "font-serif": {"font-family", fontFamily["serif"]}, "font-mono": {"font-family", fontFamily["mono"]},
}

var keywordOrder = map[string]int{
	"position": oPosition, "visibility": oVisibility, "float": oFloat, "clear": oClear, "box-sizing": oBoxSizing,
	"table-layout": oTableLayout, "border-collapse": oBorderCollapse, "list-style-position": oListPos,
	"list-style-type": oListType, "flex-direction": oFlexDir, "flex-wrap": oFlexWrap, "flex": oFlex,
	"flex-grow": oGrow, "flex-shrink": oShrink, "grid-auto-flow": oGridFlow, "grid-auto-rows": oAutoRows,
	"grid-auto-columns": oAutoRows, "overflow": oOverflow, "overflow-x": oOverflow, "overflow-y": oOverflow,
	"white-space": oWhitespace, "overflow-wrap": oBreak, "word-break": oBreak, "text-align": oTextAlign,
	"font-style": oFontStyle, "text-transform": oTransformCase, "text-decoration-line": oDecoration,
	"text-overflow": oTruncate, "vertical-align": oVAlign, "background-size": oBgSize, "background-repeat": oBgRepeat,
	"background-image": oBgImage, "border-style": oBorderStyle, "object-fit": oObject, "pointer-events": oPointer,
	"resize": oResize, "user-select": oSelect, "aspect-ratio": oAspect, "font-family": oFontFamily,
}

var alignMap = map[string]string{
	"start": "flex-start", "end": "flex-end", "center": "center", "between": "space-between",
	"around": "space-around", "evenly": "space-evenly", "stretch": "stretch", "baseline": "baseline", "normal": "normal",
}

// sides maps padding/margin/inset suffixes to properties.
var sides = map[string][]string{
	"":  {""},
	"x": {"-left", "-right"}, "y": {"-top", "-bottom"},
	"t": {"-top"}, "r": {"-right"}, "b": {"-bottom"}, "l": {"-left"},
	"s": {"-left"}, "e": {"-right"}, // logical sides, left-to-right
}

func sideDecls(order int, prop, side, value string) (util, bool) {
	props, ok := sides[side]
	if !ok {
		return util{}, false
	}
	u := util{order: order}
	for _, p := range props {
		u.decls = append(u.decls, decl{prop + p, value})
	}
	return u, true
}

// cut splits "px-4" into ("px", "4") for the given prefixes, longest first.
func cut(s string, prefixes ...string) (string, string, bool) {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p+"-") {
			return p, s[len(p)+1:], true
		}
	}
	return "", "", false
}

func utility(s string, neg bool) (util, bool) {
	// arbitrary property: [mask-type:luminance]
	if isArbitrary(s) && !neg {
		prop, val, ok := strings.Cut(arbitrary(s), ":")
		if !ok || prop == "" || strings.HasPrefix(val, "--spacing") {
			return util{}, false
		}
		return one(oOutline+1, strings.TrimSpace(prop), strings.TrimSpace(val))
	}
	if v, ok := displays[s]; ok && !neg {
		return one(oDisplay, "display", v)
	}
	if kv, ok := keywordUtils[s]; ok && !neg {
		return one(keywordOrder[kv[0]], kv[0], kv[1])
	}
	switch s {
	case "container":
		return util{}, false // handled by containerRules
	case "sr-only":
		return many(oSrOnly, "position", "absolute", "width", "1px", "height", "1px", "padding", "0",
			"margin", "-1px", "overflow", "hidden", "clip", "rect(0, 0, 0, 0)", "white-space", "nowrap", "border-width", "0")
	case "not-sr-only":
		return many(oSrOnly, "position", "static", "width", "auto", "height", "auto", "padding", "0",
			"margin", "0", "overflow", "visible", "clip", "auto", "white-space", "normal")
	case "truncate":
		return many(oTruncate, "overflow", "hidden", "text-overflow", "ellipsis", "white-space", "nowrap")
	case "border":
		return one(oBorderWidth, "border-width", "1px")
	case "rounded":
		return one(oRounded, "border-radius", radius[""])
	case "shadow":
		return one(oShadow, "box-shadow", shadows[""])
	case "transform", "transform-gpu", "transform-cpu":
		return one(oTransform, "transform", transformValue)
	case "transform-none":
		return one(oTransform, "transform", "none")
	case "antialiased", "subpixel-antialiased", "transition", "transition-all", "transition-colors",
		"transition-opacity", "transition-shadow", "transition-transform", "transition-none", "outline-none":
		return util{}, false
	case "grow", "shrink":
		return util{}, false
	}

	// spacing: padding and margin
	if p, v, ok := cut(s, "px", "py", "pt", "pr", "pb", "pl", "ps", "pe", "p"); ok && !neg {
		val, ok := spacingOr(v)
		if !ok {
			return util{}, false
		}
		return sideDecls(oPadding, "padding", p[1:], val)
	}
	if p, v, ok := cut(s, "mx", "my", "mt", "mr", "mb", "ml", "ms", "me", "m"); ok {
		val, ok := spacingOr(v)
		if v == "auto" {
			val, ok = "auto", !neg
		}
		if !ok {
			return util{}, false
		}
		return sideDecls(oMargin, "margin", p[1:], negate(val, neg))
	}
	if p, v, ok := cut(s, "space-x", "space-y"); ok {
		val, ok := spacingOr(v)
		if !ok {
			return util{}, false
		}
		val = negate(val, neg)
		u := util{order: oSpace, child: " > :not([hidden]) ~ :not([hidden])"}
		if p == "space-x" {
			u.decls = []decl{{"margin-right", "0"}, {"margin-left", val}}
		} else {
			u.decls = []decl{{"margin-top", val}, {"margin-bottom", "0"}}
		}
		return u, true
	}

	// inset
	if p, v, ok := cut(s, "inset-x", "inset-y", "inset", "top", "right", "bottom", "left", "start", "end"); ok {
		val, ok := length(v, insetExtra)
		if !ok {
			return util{}, false
		}
		val = negate(val, neg)
		switch p {
		case "inset":
			return many(oInset, "top", val, "right", val, "bottom", val, "left", val)
		case "inset-x":
			return many(oInset, "left", val, "right", val)
		case "inset-y":
			return many(oInset, "top", val, "bottom", val)
		case "start":
			return one(oInset, "left", val)
		case "end":
			return one(oInset, "right", val)
		}
		return one(oInset, p, val)
	}
	if _, v, ok := cut(s, "z"); ok {
		if v == "auto" {
			return one(oZ, "z-index", "auto")
		}
		if isArbitrary(v) {
			return one(oZ, "z-index", arbitrary(v))
		}
		if n, err := strconv.Atoi(v); err == nil && n%10 == 0 && n <= 50 {
			return one(oZ, "z-index", negate(v, neg))
		}
		return util{}, false
	}
	if _, v, ok := cut(s, "order"); ok {
		switch v {
		case "first":
			return one(oOrder, "order", "-9999")
		case "last":
			return one(oOrder, "order", "9999")
		case "none":
			return one(oOrder, "order", "0")
		}
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 12 {
			return one(oOrder, "order", negate(v, neg))
		}
		if isArbitrary(v) {
			return one(oOrder, "order", arbitrary(v))
		}
		return util{}, false
	}

	// sizing
	if _, v, ok := cut(s, "size"); ok && !neg {
		val, ok := length(v, widthExtra)
		if !ok {
			return util{}, false
		}
		return many(oSize, "width", val, "height", val)
	}
	if p, v, ok := cut(s, "min-w", "max-w", "w"); ok && !neg {
		var val string
		var ok bool
		switch p {
		case "w":
			val, ok = length(v, widthExtra)
			return orFail(oWidth, "width", val, ok)
		case "min-w":
			val, ok = length(v, widthExtra)
			return orFail(oMinWidth, "min-width", val, ok)
		default:
			if val, ok = maxWidth[v]; !ok && isArbitrary(v) {
				val, ok = arbitrary(v), true
			}
			return orFail(oMaxWidth, "max-width", val, ok)
		}
	}
	if p, v, ok := cut(s, "min-h", "max-h", "h"); ok && !neg {
		val, ok := length(v, heightExtra)
		switch p {
		case "h":
			return orFail(oHeight, "height", val, ok)
		case "min-h":
			return orFail(oMinHeight, "min-height", val, ok)
		default:
			if v == "none" {
				val, ok = "none", true
			}
			return orFail(oMaxHeight, "max-height", val, ok)
		}
	}
	if _, v, ok := cut(s, "basis"); ok && !neg {
		val, ok := length(v, widthExtra)
		return orFail(oBasis, "flex-basis", val, ok)
	}
	if _, v, ok := cut(s, "aspect"); ok && isArbitrary(v) {
		return one(oAspect, "aspect-ratio", arbitrary(v))
	}
	if _, v, ok := cut(s, "flex"); ok && isArbitrary(v) {
		return one(oFlex, "flex", arbitrary(v))
	}

	// grid
	if _, v, ok := cut(s, "grid-cols"); ok {
		return gridTemplate(oGridCols, "grid-template-columns", v)
	}
	if _, v, ok := cut(s, "grid-rows"); ok {
		return gridTemplate(oGridRows, "grid-template-rows", v)
	}
	if p, v, ok := cut(s, "col-span", "col-start", "col-end", "row-span", "row-start", "row-end", "col", "row"); ok {
		return gridPlacement(p, v)
	}
	if p, v, ok := cut(s, "gap-x", "gap-y", "gap"); ok && !neg {
		val, ok := spacingOr(v)
		if !ok {
			return util{}, false
		}
		switch p {
		case "gap-x":
			return one(oGap, "column-gap", val)
		case "gap-y":
			return one(oGap, "row-gap", val)
		}
		return one(oGap, "gap", val)
	}

	// alignment
	if p, v, ok := cut(s, "justify-items", "justify-self", "justify", "content", "items", "self", "place-content", "place-items", "place-self"); ok {
		switch p {
		case "justify":
			if a, ok := alignMap[v]; ok {
				return one(oJustify, "justify-content", a)
			}
		case "content":
			if a, ok := alignMap[v]; ok {
				return one(oAlignContent, "align-content", a)
			}
		case "items":
			if a, ok := alignMap[v]; ok && v != "between" && v != "around" && v != "evenly" {
				return one(oAlignItems, "align-items", a)
			}
		case "self":
			if v == "auto" {
				return one(oSelf, "align-self", "auto")
			}
			if a, ok := alignMap[v]; ok {
				return one(oSelf, "align-self", a)
			}
		case "justify-items", "justify-self", "place-content", "place-items", "place-self":
			val := map[string]string{"start": "start", "end": "end", "center": "center", "stretch": "stretch",
				"auto": "auto", "between": "space-between", "around": "space-around", "evenly": "space-evenly", "baseline": "baseline"}[v]
			if val == "" {
				return util{}, false
			}
			order := map[string]int{"justify-items": oJustifyItems, "justify-self": oJustifySelf,
				"place-content": oPlace, "place-items": oPlace, "place-self": oPlace}[p]
			return one(order, p, val)
		}
		return util{}, false
	}

	// typography
	if _, v, ok := cut(s, "font"); ok && !neg {
		if w, ok := fontWeight[v]; ok {
			return one(oFontWeight, "font-weight", w)
		}
		if isArbitrary(v) {
			a := arbitrary(v)
			if _, err := strconv.Atoi(a); err == nil {
				return one(oFontWeight, "font-weight", a)
			}
			return one(oFontFamily, "font-family", a)
		}
		return util{}, false
	}
	if _, v, ok := cut(s, "text"); ok && !neg {
		base, lh, hasLH := strings.Cut(v, "/")
		if sz, ok := fontSize[base]; ok {
			line := sz[1]
			if hasLH {
				if l, ok := leading[lh]; ok {
					line = l
				} else if isArbitrary(lh) {
					line = arbitrary(lh)
				} else {
					return util{}, false
				}
			}
			return many(oFontSize, "font-size", sz[0], "line-height", line)
		}
		if isArbitrary(v) && isLength(arbitrary(v)) {
			return one(oFontSize, "font-size", strings.TrimPrefix(arbitrary(v), "length:"))
		}
		if c, ok := color(v); ok {
			return one(oTextColor, "color", c)
		}
		return util{}, false
	}
	if _, v, ok := cut(s, "leading"); ok && !neg {
		if l, ok := leading[v]; ok {
			return one(oLeading, "line-height", l)
		}
		if isArbitrary(v) {
			return one(oLeading, "line-height", arbitrary(v))
		}
		return util{}, false
	}
	if _, v, ok := cut(s, "tracking"); ok {
		if t, ok := tracking[v]; ok {
			return one(oTracking, "letter-spacing", negate(t, neg))
		}
		if isArbitrary(v) {
			return one(oTracking, "letter-spacing", negate(arbitrary(v), neg))
		}
		return util{}, false
	}
	if _, v, ok := cut(s, "indent"); ok {
		val, ok := length(v, nil)
		return orFail(oIndent, "text-indent", negate(val, neg), ok)
	}
	if _, v, ok := cut(s, "list"); ok && isArbitrary(v) {
		return one(oListType, "list-style-type", arbitrary(v))
	}
	if _, v, ok := cut(s, "line-clamp"); ok {
		if v == "none" {
			return many(oLineClamp, "overflow", "visible", "display", "block")
		}
		return many(oLineClamp, "overflow", "hidden", "display", "-webkit-box", "-webkit-line-clamp", v)
	}
	if _, v, ok := cut(s, "decoration"); ok {
		if c, ok := color(v); ok {
			return one(oDecoration, "text-decoration-color", c)
		}
		return util{}, false
	}

	// backgrounds
	if _, v, ok := cut(s, "bg-gradient-to"); ok {
		if d, ok := gradientDir[v]; ok {
			return one(oBgImage, "background-image", "linear-gradient("+d+", var(--tw-gradient-stops))")
		}
		return util{}, false
	}
	if p, v, ok := cut(s, "from", "via", "to"); ok {
		c, ok := color(v)
		if !ok {
			return util{}, false
		}
		switch p {
		case "from":
			return many(oGradient, "--tw-gradient-from", c, "--tw-gradient-to", transparentOf(c),
				"--tw-gradient-stops", "var(--tw-gradient-from), var(--tw-gradient-to)")
		case "via":
			return many(oGradient+1, "--tw-gradient-to", transparentOf(c),
				"--tw-gradient-stops", "var(--tw-gradient-from), "+c+", var(--tw-gradient-to)")
		}
		return many(oGradient+2, "--tw-gradient-to", c)
	}
	if _, v, ok := cut(s, "bg"); ok && !neg {
		switch v {
		case "center", "top", "bottom", "left", "right", "left-top", "left-bottom", "right-top", "right-bottom":
			return one(oBgPos, "background-position", strings.ReplaceAll(v, "-", " "))
		case "fixed", "local", "scroll":
			return one(oBgPos, "background-attachment", v)
		}
		if isArbitrary(v) {
			a := arbitrary(v)
			if strings.HasPrefix(a, "url(") || strings.Contains(a, "gradient(") {
				return one(oBgImage, "background-image", a)
			}
		}
		if c, ok := color(v); ok {
			return one(oBgColor, "background-color", c)
		}
		return util{}, false
	}

	// borders
	if strings.HasPrefix(s, "rounded") && !neg {
		return rounded(s)
	}
	if strings.HasPrefix(s, "border") && !neg {
		return border(s)
	}
	if p, v, ok := cut(s, "divide-x", "divide-y", "divide"); ok && !neg {
		u := util{order: oDivide, child: " > :not([hidden]) ~ :not([hidden])"}
		if p == "divide" {
			c, ok := color(v)
			if !ok {
				return util{}, false
			}
			u.decls = []decl{{"border-color", c}}
			return u, true
		}
		w := map[string]string{"0": "0px", "2": "2px", "4": "4px", "8": "8px"}[v]
		if v == "" {
			w = "1px"
		}
		if w == "" {
			return util{}, false
		}
		if p == "divide-x" {
			u.decls = []decl{{"border-right-width", "0"}, {"border-left-width", w}}
		} else {
			u.decls = []decl{{"border-top-width", w}, {"border-bottom-width", "0"}}
		}
		return u, true
	}
	if s == "divide-x" || s == "divide-y" {
		return utility(s+"-", false)
	}

	// effects
	if _, v, ok := cut(s, "shadow"); ok && !neg {
		if sh, ok := shadows[v]; ok {
			return one(oShadow, "box-shadow", sh)
		}
		if isArbitrary(v) {
			return one(oShadow, "box-shadow", arbitrary(v))
		}
		return util{}, false // shadow colors are not supported
	}
	if _, v, ok := cut(s, "opacity"); ok && !neg {
		if a, ok := opacityValue(v); ok && (isArbitrary(v) || isOpacityStep(v)) {
			return one(oOpacity, "opacity", strconv.FormatFloat(a, 'f', -1, 64))
		}
		return util{}, false
	}

	// transforms
	if p, v, ok := cut(s, "translate-x", "translate-y"); ok {
		val, ok := length(v, map[string]string{"full": "100%"})
		if !ok {
			return util{}, false
		}
		return many(oTransform, "--tw-"+p, negate(val, neg), "transform", transformValue)
	}
	if p, v, ok := cut(s, "scale-x", "scale-y", "scale"); ok {
		var val string
		if isArbitrary(v) {
			val = arbitrary(v)
		} else if n, err := strconv.Atoi(v); err == nil && strings.Contains(" 0 50 75 90 95 100 105 110 125 150 ", " "+v+" ") {
			val = strconv.FormatFloat(float64(n)/100, 'f', -1, 64)
		} else {
			return util{}, false
		}
		val = negate(val, neg)
		switch p {
		case "scale-x":
			return many(oTransform, "--tw-scale-x", val, "transform", transformValue)
		case "scale-y":
			return many(oTransform, "--tw-scale-y", val, "transform", transformValue)
		}
		return many(oTransform, "--tw-scale-x", val, "--tw-scale-y", val, "transform", transformValue)
	}
	if p, v, ok := cut(s, "rotate", "skew-x", "skew-y"); ok {
		var val string
		if isArbitrary(v) {
			val = arbitrary(v)
		} else if _, err := strconv.Atoi(v); err == nil {
			val = v + "deg"
		} else {
			return util{}, false
		}
		prop := map[string]string{"rotate": "--tw-rotate", "skew-x": "--tw-skew-x", "skew-y": "--tw-skew-y"}[p]
		return many(oTransform, prop, negate(val, neg), "transform", transformValue)
	}
	if _, v, ok := cut(s, "origin"); ok {
		o := map[string]string{"center": "center", "top": "top", "top-right": "top right", "right": "right",
			"bottom-right": "bottom right", "bottom": "bottom", "bottom-left": "bottom left", "left": "left", "top-left": "top left"}[v]
		if o == "" && isArbitrary(v) {
			o = arbitrary(v)
		}
		return orFail(oOrigin, "transform-origin", o, o != "")
	}
	if _, v, ok := cut(s, "object"); ok {
		pos := strings.ReplaceAll(v, "-", " ")
		return one(oObject, "object-position", pos)
	}
	if _, v, ok := cut(s, "border-spacing"); ok {
		val, ok := spacingOr(v)
		return orFail(oBorderSpacing, "border-spacing", val, ok)
	}
	if _, v, ok := cut(s, "cursor"); ok {
		return one(oCursor, "cursor", v)
	}
	return util{}, false
}

const transformValue = "translate(var(--tw-translate-x), var(--tw-translate-y)) rotate(var(--tw-rotate)) skewX(var(--tw-skew-x)) skewY(var(--tw-skew-y)) scaleX(var(--tw-scale-x)) scaleY(var(--tw-scale-y))"

func isOpacityStep(v string) bool {
	return strings.Contains(" 0 5 10 15 20 25 30 35 40 45 50 55 60 65 70 75 80 85 90 95 100 ", " "+v+" ")
}

func orFail(order int, prop, val string, ok bool) (util, bool) {
	if !ok {
		return util{}, false
	}
	return one(order, prop, val)
}

func spacingOr(v string) (string, bool) {
	if isArbitrary(v) {
		return arbitrary(v), true
	}
	return spacing(v)
}

func gridTemplate(order int, prop, v string) (util, bool) {
	switch {
	case v == "none":
		return one(order, prop, "none")
	case v == "subgrid":
		return one(order, prop, "subgrid")
	case isArbitrary(v):
		return one(order, prop, arbitrary(v))
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 12 {
		return util{}, false
	}
	return one(order, prop, "repeat("+v+", minmax(0, 1fr))")
}

func gridPlacement(p, v string) (util, bool) {
	axis := "column"
	order := oGridCol
	if strings.HasPrefix(p, "row") {
		axis, order = "row", oGridRow
	}
	switch {
	case p == "col" || p == "row":
		if v == "auto" {
			return one(order, "grid-"+axis, "auto")
		}
		if isArbitrary(v) {
			return one(order, "grid-"+axis, arbitrary(v))
		}
	case strings.HasSuffix(p, "-span"):
		if v == "full" {
			return one(order, "grid-"+axis, "1 / -1")
		}
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 12 {
			return one(order, "grid-"+axis, "span "+v+" / span "+v)
		}
		if isArbitrary(v) {
			return one(order, "grid-"+axis, arbitrary(v))
		}
	case strings.HasSuffix(p, "-start"), strings.HasSuffix(p, "-end"):
		edge := p[strings.IndexByte(p, '-')+1:]
		if v == "auto" {
			return one(order, "grid-"+axis+"-"+edge, "auto")
		}
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 13 {
			return one(order, "grid-"+axis+"-"+edge, v)
		}
		if isArbitrary(v) {
			return one(order, "grid-"+axis+"-"+edge, arbitrary(v))
		}
	}
	return util{}, false
}

var radiusCorners = map[string][]string{
	"":   {"border-radius"},
	"t":  {"border-top-left-radius", "border-top-right-radius"},
	"r":  {"border-top-right-radius", "border-bottom-right-radius"},
	"b":  {"border-bottom-right-radius", "border-bottom-left-radius"},
	"l":  {"border-top-left-radius", "border-bottom-left-radius"},
	"s":  {"border-start-start-radius", "border-end-start-radius"},
	"e":  {"border-start-end-radius", "border-end-end-radius"},
	"tl": {"border-top-left-radius"}, "tr": {"border-top-right-radius"},
	"br": {"border-bottom-right-radius"}, "bl": {"border-bottom-left-radius"},
	"ss": {"border-start-start-radius"}, "se": {"border-start-end-radius"},
	"es": {"border-end-start-radius"}, "ee": {"border-end-end-radius"},
}

// rounded handles rounded, rounded-lg, rounded-t-xl, rounded-[20px] ...
func rounded(s string) (util, bool) {
	rest := strings.TrimPrefix(s, "rounded")
	rest = strings.TrimPrefix(rest, "-")
	corner, size := "", rest
	if i := strings.IndexByte(rest, '-'); i >= 0 && !isArbitrary(rest) {
		corner, size = rest[:i], rest[i+1:]
	} else if _, ok := radiusCorners[rest]; ok && rest != "" {
		corner, size = rest, ""
	}
	props, ok := radiusCorners[corner]
	if !ok {
		return util{}, false
	}
	val, ok := radius[size]
	if !ok && isArbitrary(size) {
		val, ok = arbitrary(size), true
	}
	if !ok {
		return util{}, false
	}
	u := util{order: oRounded}
	for _, p := range props {
		u.decls = append(u.decls, decl{p, val})
	}
	// physical corners over logical ones for the engine
	for i, d := range u.decls {
		u.decls[i].prop = map[string]string{
			"border-start-start-radius": "border-top-left-radius", "border-end-start-radius": "border-bottom-left-radius",
			"border-start-end-radius": "border-top-right-radius", "border-end-end-radius": "border-bottom-right-radius",
		}[d.prop]
		if u.decls[i].prop == "" {
			u.decls[i].prop = d.prop
		}
	}
	return u, true
}

var borderSides = map[string][]string{
	"": {""}, "x": {"-left", "-right"}, "y": {"-top", "-bottom"},
	"t": {"-top"}, "r": {"-right"}, "b": {"-bottom"}, "l": {"-left"}, "s": {"-left"}, "e": {"-right"},
}

var borderWidths = map[string]string{"0": "0px", "2": "2px", "4": "4px", "8": "8px"}

// border handles border-2, border-b, border-t-4, border-gray-200,
// border-x-[3px], border-[rgba(...)] ...
func border(s string) (util, bool) {
	rest := strings.TrimPrefix(s, "border")
	rest = strings.TrimPrefix(rest, "-")
	side := ""
	if i := strings.IndexByte(rest, '-'); i > 0 && len(rest[:i]) == 1 && strings.Contains("xytrbles", rest[:i]) {
		side, rest = rest[:i], rest[i+1:]
	} else if len(rest) == 1 && strings.Contains("xytrbles", rest) {
		side, rest = rest, ""
	}
	props := borderSides[side]
	set := func(order int, suffix, val string) (util, bool) {
		u := util{order: order}
		for _, p := range props {
			u.decls = append(u.decls, decl{"border" + p + suffix, val})
		}
		return u, true
	}
	if rest == "" {
		return set(oBorderWidth, "-width", "1px")
	}
	if w, ok := borderWidths[rest]; ok {
		return set(oBorderWidth, "-width", w)
	}
	if isArbitrary(rest) && isLength(arbitrary(rest)) {
		return set(oBorderWidth, "-width", strings.TrimPrefix(arbitrary(rest), "length:"))
	}
	if c, ok := color(rest); ok {
		return set(oBorderColor, "-color", c)
	}
	return util{}, false
}

// containerRules returns the rules of the "container" class: full width,
// capped at each breakpoint.
func containerRules(class string) []rule {
	sel := "." + escape(class)
	out := []rule{{order: oContainer, selector: sel, decls: []decl{{"width", "100%"}}}}
	for i, sc := range screens {
		out = append(out, rule{group: i + 1, order: oContainer, selector: sel,
			media: "(min-width: " + strconv.Itoa(sc.px) + "px)",
			decls: []decl{{"max-width", strconv.Itoa(sc.px) + "px"}}})
	}
	return out
}
