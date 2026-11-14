package render

import (
	"math"
	"strconv"
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
)

// CSS Grid Layout (Level 1, without subgrid): explicit and implicit
// tracks, named areas, line-based and auto placement, intrinsic and
// flexible track sizing, alignment, and pagination by rows.

// ---------------------------------------------------------------------
// Property helpers

// parseAreaStrings reads grid-template-areas: "a a b" "c c b".
func parseAreaStrings(raw string) []string {
	var out []string
	for {
		i := strings.IndexAny(raw, `"'`)
		if i < 0 {
			return out
		}
		q := raw[i]
		j := strings.IndexByte(raw[i+1:], q)
		if j < 0 {
			return out
		}
		out = append(out, strings.Join(strings.Fields(raw[i+1:i+1+j]), " "))
		raw = raw[i+j+2:]
	}
}

// parseGridTemplate handles grid-template / grid shorthands:
// "<rows> / <columns>" or "<areas+row sizes> / <columns>".
func parseGridTemplate(s *style, raw string) {
	v := strings.TrimSpace(raw)
	if strings.EqualFold(v, "none") {
		s.gridRows, s.gridCols, s.gridAreas = "", "", nil
		return
	}
	left, right, hasSlash := strings.Cut(v, "/")
	lower := strings.ToLower(left)
	if strings.Contains(lower, "auto-flow") {
		// grid: auto-flow [dense] <auto-rows> / <columns>
		s.gridAutoFlow = "row"
		if strings.Contains(lower, "dense") {
			s.gridAutoFlow = "row dense"
		}
		rest := strings.TrimSpace(strings.NewReplacer("auto-flow", "", "dense", "").Replace(lower))
		if rest != "" {
			s.gridAutoRows = rest
		}
		s.gridCols = strings.ToLower(strings.TrimSpace(right))
		return
	}
	if strings.Contains(strings.ToLower(right), "auto-flow") {
		rr := strings.ToLower(right)
		s.gridAutoFlow = "column"
		if strings.Contains(rr, "dense") {
			s.gridAutoFlow = "column dense"
		}
		if rest := strings.TrimSpace(strings.NewReplacer("auto-flow", "", "dense", "").Replace(rr)); rest != "" {
			s.gridAutoCols = rest
		}
		s.gridRows = strings.ToLower(strings.TrimSpace(left))
		return
	}
	if strings.ContainsAny(left, `"'`) {
		// areas with optional row sizes after each string
		s.gridAreas = parseAreaStrings(left)
		var sizes []string
		rest := left
		for range s.gridAreas {
			i := strings.IndexAny(rest, `"'`)
			q := rest[i]
			j := strings.IndexByte(rest[i+1:], q)
			rest = rest[i+j+2:]
			next := strings.IndexAny(rest, `"'`)
			seg := rest
			if next >= 0 {
				seg = rest[:next]
			}
			if size := strings.TrimSpace(seg); size != "" {
				sizes = append(sizes, strings.ToLower(size))
			} else {
				sizes = append(sizes, "auto")
			}
		}
		s.gridRows = strings.Join(sizes, " ")
	} else {
		s.gridRows = strings.ToLower(strings.TrimSpace(left))
	}
	if hasSlash {
		s.gridCols = strings.ToLower(strings.TrimSpace(right))
	}
}

func splitSlash(v string) (string, string) {
	a, b, _ := strings.Cut(v, "/")
	return strings.TrimSpace(a), strings.TrimSpace(b)
}

func isGridIdent(v string) bool {
	if v == "" || v == "auto" || strings.HasPrefix(v, "span") {
		return false
	}
	if _, err := strconv.Atoi(strings.Fields(v)[0]); err == nil {
		return false
	}
	return true
}

// ---------------------------------------------------------------------
// Track lists

type trackKind int

const (
	tFixed trackKind = iota
	tAuto
	tMinContent
	tMaxContent
	tFr
	tFitContent
)

type trackSize struct {
	minK, maxK trackKind
	minE, maxE string  // length expressions for tFixed / tFitContent
	fr         float64 // flex factor when maxK == tFr
}

// trackList is a parsed track listing; an auto-fill/auto-fit repeat is
// kept apart and expanded once the available space is known.
type trackList struct {
	before, after []trackSize
	autoRepeat    []trackSize
	autoFit       bool
	hasAuto       bool
}

func parseTrackSize(tok string) trackSize {
	tok = strings.TrimSpace(tok)
	switch {
	case tok == "auto":
		return trackSize{minK: tAuto, maxK: tAuto}
	case tok == "min-content":
		return trackSize{minK: tMinContent, maxK: tMinContent}
	case tok == "max-content":
		return trackSize{minK: tMaxContent, maxK: tMaxContent}
	case strings.HasSuffix(tok, "fr") && !strings.Contains(tok, "("):
		f, err := strconv.ParseFloat(strings.TrimSuffix(tok, "fr"), 64)
		if err != nil || f < 0 {
			return trackSize{minK: tAuto, maxK: tAuto}
		}
		return trackSize{minK: tAuto, maxK: tFr, fr: f}
	case strings.HasPrefix(tok, "minmax(") && strings.HasSuffix(tok, ")"):
		args := splitTopLevelComma(tok[7 : len(tok)-1])
		if len(args) != 2 {
			return trackSize{minK: tAuto, maxK: tAuto}
		}
		lo, hi := parseTrackSize(args[0]), parseTrackSize(args[1])
		t := trackSize{minK: lo.minK, minE: lo.minE, maxK: hi.maxK, maxE: hi.maxE, fr: hi.fr}
		if lo.maxK == tFr { // a flexible minimum is invalid: treat as auto
			t.minK = tAuto
		}
		if lo.minK == tFixed {
			t.minE = lo.minE
		}
		if hi.maxK == tFixed {
			t.maxE = hi.minE
		}
		return t
	case strings.HasPrefix(tok, "fit-content(") && strings.HasSuffix(tok, ")"):
		return trackSize{minK: tAuto, maxK: tFitContent, maxE: tok[12 : len(tok)-1]}
	}
	return trackSize{minK: tFixed, maxK: tFixed, minE: tok, maxE: tok}
}

func parseTrackList(v string) trackList {
	var tl trackList
	if v == "" || v == "none" {
		return tl
	}
	var expand func(tokens []string) []trackSize
	expand = func(tokens []string) []trackSize {
		var out []trackSize
		for _, tok := range tokens {
			if strings.HasPrefix(tok, "[") { // line names are not used
				continue
			}
			if strings.HasPrefix(tok, "repeat(") && strings.HasSuffix(tok, ")") {
				count, list, ok := strings.Cut(tok[7:len(tok)-1], ",")
				if !ok {
					continue
				}
				inner := expand(splitValue(stripBrackets(list)))
				switch c := strings.TrimSpace(count); c {
				case "auto-fill", "auto-fit":
					tl.hasAuto = true
					tl.autoFit = c == "auto-fit"
					tl.autoRepeat = inner
					return out // tracks after an auto repeat go to "after"
				default:
					n, err := strconv.Atoi(c)
					if err != nil || n < 1 {
						continue
					}
					for i := 0; i < min(n, 1000); i++ {
						out = append(out, inner...)
					}
				}
				continue
			}
			out = append(out, parseTrackSize(tok))
		}
		return out
	}
	tokens := splitValue(stripBrackets(v))
	tl.before = expand(tokens)
	if tl.hasAuto {
		// collect what follows the auto repeat
		after := false
		var rest []string
		for _, tok := range tokens {
			if after {
				rest = append(rest, tok)
			} else if strings.HasPrefix(tok, "repeat(") && (strings.Contains(tok, "auto-fill") || strings.Contains(tok, "auto-fit")) {
				after = true
			}
		}
		saved := tl
		tl.after = expand(rest)
		tl.before, tl.autoRepeat, tl.autoFit, tl.hasAuto = saved.before, saved.autoRepeat, saved.autoFit, true
	}
	return tl
}

// stripBrackets removes [line names] so they do not disturb splitting.
func stripBrackets(v string) string {
	var sb strings.Builder
	depth := 0
	for _, r := range v {
		switch {
		case r == '[':
			depth++
		case r == ']':
			depth--
			sb.WriteByte(' ')
		case depth == 0:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// expandTracks resolves an auto-fill/auto-fit repetition for the
// available size.
func (e *engine) expandTracks(tl trackList, st *style, avail, gap float64, definite bool) ([]trackSize, int, int) {
	tracks := append([]trackSize(nil), tl.before...)
	repStart, repCount := len(tracks), 0
	if tl.hasAuto && len(tl.autoRepeat) > 0 {
		reps := 1
		if definite {
			fixed := 0.0
			for _, t := range append(append([]trackSize(nil), tl.before...), tl.after...) {
				if v, ok := e.fixedSize(t, st, avail); ok {
					fixed += v
				}
				fixed += gap
			}
			one := 0.0
			for _, t := range tl.autoRepeat {
				v, ok := e.fixedSize(t, st, avail)
				if !ok {
					v = 0
				}
				one += v + gap
			}
			if one > 0 {
				reps = max(int(math.Floor((avail-fixed+gap)/one)), 1)
			}
		}
		reps = min(reps, 500)
		for i := 0; i < reps; i++ {
			tracks = append(tracks, tl.autoRepeat...)
		}
		repCount = reps * len(tl.autoRepeat)
	}
	tracks = append(tracks, tl.after...)
	return tracks, repStart, repCount
}

// fixedSize returns a definite size of a track (its fixed max, else its
// fixed min) for auto-repeat counting.
func (e *engine) fixedSize(t trackSize, st *style, ref float64) (float64, bool) {
	if t.maxK == tFixed {
		return e.resolveLength(t.maxE, st, ref)
	}
	if t.minK == tFixed {
		return e.resolveLength(t.minE, st, ref)
	}
	return 0, false
}

// ---------------------------------------------------------------------
// Placement

type gridItem struct {
	*flexItem
	row, col         int // 0-based start
	rowSpan, colSpan int
}

type gridLine struct {
	auto bool
	line int // 1-based (negative counts from the end)
	span int
	name string
}

func parseGridLine(v string) gridLine {
	v = strings.TrimSpace(v)
	if v == "" || v == "auto" {
		return gridLine{auto: true}
	}
	f := strings.Fields(v)
	gl := gridLine{}
	for _, tok := range f {
		if tok == "span" {
			gl.span = max(gl.span, 1)
			continue
		}
		if n, err := strconv.Atoi(tok); err == nil {
			if gl.span > 0 || strings.Contains(v, "span") {
				gl.span = max(n, 1)
			} else {
				gl.line = n
			}
			continue
		}
		gl.name = tok
	}
	if gl.span == 0 && gl.line == 0 && gl.name == "" {
		gl.auto = true
	}
	return gl
}

type area struct{ r0, r1, c0, c1 int }

func parseAreas(rows []string) (map[string]area, int, int) {
	areas := map[string]area{}
	cols := 0
	for r, row := range rows {
		names := strings.Fields(row)
		cols = max(cols, len(names))
		for c, name := range names {
			if strings.Trim(name, ".") == "" {
				continue
			}
			a, ok := areas[name]
			if !ok {
				a = area{r, r + 1, c, c + 1}
			}
			a.r0, a.r1 = min(a.r0, r), max(a.r1, r+1)
			a.c0, a.c1 = min(a.c0, c), max(a.c1, c+1)
			areas[name] = a
		}
	}
	return areas, len(rows), cols
}

// resolveAxis turns start/end values into a 0-based start (-1 = auto) and
// a span, for an axis with `explicit` tracks.
func resolveAxis(startV, endV string, explicit int, areas map[string]area, rowAxis bool) (int, int) {
	s, e := parseGridLine(startV), parseGridLine(endV)
	lineIdx := func(l gridLine, isEnd bool) (int, bool) {
		if l.name != "" {
			name := strings.TrimSuffix(strings.TrimSuffix(l.name, "-start"), "-end")
			if a, ok := areas[name]; ok {
				switch {
				case rowAxis && (isEnd || strings.HasSuffix(l.name, "-end")):
					return a.r1, true
				case rowAxis:
					return a.r0, true
				case isEnd || strings.HasSuffix(l.name, "-end"):
					return a.c1, true
				default:
					return a.c0, true
				}
			}
			return 0, false
		}
		if l.line != 0 && l.span == 0 {
			if l.line > 0 {
				return l.line - 1, true
			}
			return max(explicit+1+l.line, 0), true
		}
		return 0, false
	}
	si, sOK := lineIdx(s, false)
	ei, eOK := lineIdx(e, true)
	if s.name != "" && e.auto {
		// a named start with an auto end spans the whole area
		if ei2, ok := lineIdx(gridLine{name: s.name}, true); ok {
			ei, eOK = ei2, true
		}
	}
	switch {
	case sOK && eOK:
		if ei < si {
			si, ei = ei, si
		}
		return si, max(ei-si, 1)
	case sOK:
		return si, max(e.span, 1)
	case eOK:
		span := max(s.span, 1)
		return max(ei-span, 0), span
	}
	return -1, max(max(s.span, e.span), 1)
}

// placeItems runs the grid placement algorithm. It returns the items with
// positions and the final row and column counts.
func (e *engine) placeItems(items []*flexItem, st *style, explicitRows, explicitCols int, areas map[string]area) ([]*gridItem, int, int) {
	flow := strings.Fields(st.gridAutoFlow)
	columnFlow, dense := false, false
	for _, f := range flow {
		if f == "column" {
			columnFlow = true
		}
		if f == "dense" {
			dense = true
		}
	}
	var gis []*gridItem
	rows, cols := explicitRows, explicitCols
	for _, it := range items {
		r, rs := resolveAxis(it.st.gridRowStart, it.st.gridRowEnd, explicitRows, areas, true)
		c, cs := resolveAxis(it.st.gridColStart, it.st.gridColEnd, explicitCols, areas, false)
		gi := &gridItem{flexItem: it, row: r, col: c, rowSpan: min(rs, 1000), colSpan: min(cs, 1000)}
		gis = append(gis, gi)
		if r >= 0 {
			rows = max(rows, r+gi.rowSpan)
		}
		if c >= 0 {
			cols = max(cols, c+gi.colSpan)
		}
		if columnFlow {
			rows = max(rows, gi.rowSpan)
		} else {
			cols = max(cols, gi.colSpan)
		}
	}
	cols, rows = max(cols, 1), max(rows, 1)

	occ := map[[2]int]bool{}
	fits := func(r, c, rs, cs int) bool {
		for i := r; i < r+rs; i++ {
			for j := c; j < c+cs; j++ {
				if occ[[2]int{i, j}] {
					return false
				}
			}
		}
		return true
	}
	mark := func(gi *gridItem) {
		for i := gi.row; i < gi.row+gi.rowSpan; i++ {
			for j := gi.col; j < gi.col+gi.colSpan; j++ {
				occ[[2]int{i, j}] = true
			}
		}
		rows = max(rows, gi.row+gi.rowSpan)
		cols = max(cols, gi.col+gi.colSpan)
	}
	// 1. fully definite items
	for _, gi := range gis {
		if gi.row >= 0 && gi.col >= 0 {
			mark(gi)
		}
	}
	// In column flow the roles of rows and columns swap.
	major := func(gi *gridItem) *int {
		if columnFlow {
			return &gi.col
		}
		return &gi.row
	}
	minor := func(gi *gridItem) *int {
		if columnFlow {
			return &gi.row
		}
		return &gi.col
	}
	spans := func(gi *gridItem) (int, int) { // major span, minor span
		if columnFlow {
			return gi.colSpan, gi.rowSpan
		}
		return gi.rowSpan, gi.colSpan
	}
	minorCount := func() int {
		if columnFlow {
			return rows
		}
		return cols
	}
	at := func(maj, min, majSpan, minSpan int) bool {
		if columnFlow {
			return fits(min, maj, minSpan, majSpan)
		}
		return fits(maj, min, majSpan, minSpan)
	}
	// 2. items locked to a major track (row in row flow)
	for _, gi := range gis {
		if *major(gi) >= 0 && *minor(gi) < 0 {
			ms, ns := spans(gi)
			m := 0
			for !at(*major(gi), m, ms, ns) && m < 10000 {
				m++
			}
			*minor(gi) = m
			mark(gi)
		}
	}
	// 3. everything else, with the auto-placement cursor
	curMaj, curMin := 0, 0
	for _, gi := range gis {
		if *major(gi) >= 0 && *minor(gi) >= 0 {
			continue
		}
		if dense {
			curMaj, curMin = 0, 0
		}
		ms, ns := spans(gi)
		if *minor(gi) >= 0 {
			// minor position fixed: find the first major track from the cursor
			if *minor(gi) < curMin {
				curMaj++
			}
			m := curMaj
			for !at(m, *minor(gi), ms, ns) && m < 100000 {
				m++
			}
			*major(gi) = m
			curMaj, curMin = m, *minor(gi)
			mark(gi)
			continue
		}
		for placed := false; !placed && curMaj < 100000; {
			for n := curMin; n+ns <= max(minorCount(), ns); n++ {
				if at(curMaj, n, ms, ns) {
					*major(gi), *minor(gi) = curMaj, n
					placed = true
					curMin = n + ns
					break
				}
			}
			if !placed {
				curMaj++
				curMin = 0
			}
		}
		mark(gi)
	}
	return gis, rows, cols
}

// ---------------------------------------------------------------------
// Track sizing

// sizeTracks implements a simplified CSS Grid track sizing algorithm for
// one axis. contrib returns an item's min-content and max-content
// contribution in this axis. It returns the base sizes after intrinsic
// sizing (min-content of the grid) and the final sizes.
func (e *engine) sizeTracks(tracks []trackSize, items []*gridItem, st *style, avail float64, definite bool, gap float64,
	span func(*gridItem) (int, int), contrib func(*gridItem) (float64, float64), stretch bool) (bases, sizes []float64) {
	n := len(tracks)
	base := make([]float64, n)
	limit := make([]float64, n)
	inf := math.Inf(1)
	ref := avail
	if !definite {
		ref = 0
	}
	for i, t := range tracks {
		if t.minK == tFixed {
			if v, ok := e.resolveLength(t.minE, st, ref); ok && (definite || !strings.Contains(t.minE, "%")) {
				base[i] = max(v, 0)
			}
		}
		switch t.maxK {
		case tFixed:
			if v, ok := e.resolveLength(t.maxE, st, ref); ok && (definite || !strings.Contains(t.maxE, "%")) {
				limit[i] = max(v, base[i])
			} else {
				limit[i] = inf
			}
		default:
			limit[i] = inf
		}
	}
	isFlex := func(i int) bool { return tracks[i].maxK == tFr }
	contribs := map[*gridItem][2]float64{}
	get := func(gi *gridItem) (float64, float64) {
		if c, ok := contribs[gi]; ok {
			return c[0], c[1]
		}
		mn, mx := contrib(gi)
		contribs[gi] = [2]float64{mn, mx}
		return mn, mx
	}
	// single-track items
	limSet := make([]bool, n)
	for _, gi := range items {
		s, sp := span(gi)
		if sp != 1 || s >= n {
			continue
		}
		mn, mx := get(gi)
		t := tracks[s]
		switch t.minK {
		case tAuto, tMinContent:
			base[s] = max(base[s], mn)
		case tMaxContent:
			base[s] = max(base[s], mx)
		}
		switch t.maxK {
		case tAuto, tMaxContent:
			limit[s] = maxSet(limit[s], mx, &limSet[s])
		case tMinContent:
			limit[s] = maxSet(limit[s], mn, &limSet[s])
		case tFitContent:
			fc, _ := e.resolveLength(t.maxE, st, ref)
			limit[s] = maxSet(limit[s], max(mn, min(mx, fc)), &limSet[s])
		}
	}
	// spanning items: distribute what does not fit yet
	for _, gi := range items {
		s, sp := span(gi)
		if sp < 2 {
			continue
		}
		end := min(s+sp, n)
		mn, mx := get(gi)
		have, haveLim := gap*float64(end-s-1), gap*float64(end-s-1)
		var targets, flexT []int
		for i := s; i < end; i++ {
			have += base[i]
			if !math.IsInf(limit[i], 1) {
				haveLim += limit[i]
			} else {
				haveLim += base[i]
			}
			if isFlex(i) {
				flexT = append(flexT, i)
			} else if tracks[i].minK != tFixed {
				targets = append(targets, i)
			}
		}
		if len(flexT) > 0 {
			targets = flexT
		}
		if len(targets) == 0 {
			continue
		}
		if extra := mn - have; extra > 0 {
			for _, i := range targets {
				base[i] += extra / float64(len(targets))
			}
		}
		if extra := mx - haveLim; extra > 0 && len(flexT) == 0 {
			for _, i := range targets {
				if math.IsInf(limit[i], 1) {
					limit[i] = base[i]
				}
				limit[i] += extra / float64(len(targets))
				limSet[i] = true
			}
		}
	}
	for i := range limit {
		if math.IsInf(limit[i], 1) && !isFlex(i) {
			limit[i] = base[i]
		}
		if limit[i] < base[i] {
			limit[i] = base[i]
		}
	}
	bases = append([]float64(nil), base...)

	used := func() float64 {
		u := gap * float64(max(n-1, 0))
		for _, b := range base {
			u += b
		}
		return u
	}
	// maximize: grow towards growth limits
	if !definite {
		for i := range base {
			if !isFlex(i) {
				base[i] = limit[i]
			}
		}
	} else if free := avail - used(); free > 0 {
		for iter := 0; iter < n+1 && free > 0.01; iter++ {
			var growable []int
			for i := range base {
				if !isFlex(i) && limit[i] > base[i]+0.001 {
					growable = append(growable, i)
				}
			}
			if len(growable) == 0 {
				break
			}
			share := free / float64(len(growable))
			for _, i := range growable {
				d := min(share, limit[i]-base[i])
				base[i] += d
				free -= d
			}
		}
	}
	// flexible tracks
	sumFr := 0.0
	for i, t := range tracks {
		if isFlex(i) {
			sumFr += t.fr
		}
	}
	if sumFr > 0 {
		if definite {
			inflexible := make([]bool, n)
			var frSize float64
			for iter := 0; iter <= n; iter++ {
				left := avail - gap*float64(max(n-1, 0))
				flex := 0.0
				for i, t := range tracks {
					if isFlex(i) && !inflexible[i] {
						flex += t.fr
					} else {
						left -= base[i]
					}
				}
				frSize = max(left, 0) / max(flex, 1)
				changed := false
				for i, t := range tracks {
					if isFlex(i) && !inflexible[i] && t.fr*frSize < base[i] {
						inflexible[i] = true
						changed = true
					}
				}
				if !changed {
					break
				}
			}
			for i, t := range tracks {
				if isFlex(i) && !inflexible[i] {
					base[i] = max(base[i], t.fr*frSize)
				}
			}
		} else {
			frSize := 0.0
			for i, t := range tracks {
				if isFlex(i) && t.fr > 0 {
					frSize = max(frSize, base[i]/max(t.fr, 1))
				}
			}
			for i, t := range tracks {
				if isFlex(i) {
					base[i] = max(base[i], t.fr*frSize)
				}
			}
		}
	}
	// stretch auto tracks
	if stretch && definite {
		if free := avail - used(); free > 0.01 {
			var autos []int
			for i, t := range tracks {
				if t.maxK == tAuto {
					autos = append(autos, i)
				}
			}
			for _, i := range autos {
				base[i] += free / float64(len(autos))
			}
		}
	}
	return bases, base
}

func maxSet(cur, v float64, set *bool) float64 {
	if !*set || math.IsInf(cur, 1) {
		*set = true
		return v
	}
	return max(cur, v)
}

// ---------------------------------------------------------------------
// Layout

type gridSetup struct {
	items      []*gridItem
	cols, rows []trackSize
	colGap     float64
	rowGap     float64
	colRep     [2]int // auto-fit repetition (start, count) for columns
	autoFitCol bool
}

func (e *engine) setupGrid(n *htmlparse.Node, st *style, w float64, definiteW bool) *gridSetup {
	items := e.flexChildren(n, st)
	areas, areaRows, areaCols := parseAreas(st.gridAreas)
	colGap := e.gap(st.colGapExpr, st, w)
	rowGap := e.gap(st.rowGapExpr, st, w)
	ctl := parseTrackList(st.gridCols)
	rtl := parseTrackList(st.gridRows)
	cols, colRepStart, colRepCount := e.expandTracks(ctl, st, w, colGap, definiteW)
	rows, _, _ := e.expandTracks(rtl, st, e.contentHeight(st), rowGap, e.contentHeight(st) > 0)
	explicitCols, explicitRows := max(len(cols), areaCols), max(len(rows), areaRows)
	gis, nRows, nCols := e.placeItems(items, st, explicitRows, explicitCols, areas)

	autoCol := parseTrackList(st.gridAutoCols).before
	autoRow := parseTrackList(st.gridAutoRows).before
	if len(autoCol) == 0 {
		autoCol = []trackSize{{minK: tAuto, maxK: tAuto}}
	}
	if len(autoRow) == 0 {
		autoRow = []trackSize{{minK: tAuto, maxK: tAuto}}
	}
	for i := len(cols); i < nCols; i++ {
		cols = append(cols, autoCol[(i-len(cols))%len(autoCol)])
	}
	for i := len(rows); i < nRows; i++ {
		rows = append(rows, autoRow[(i-len(rows))%len(autoRow)])
	}
	return &gridSetup{items: gis, cols: cols, rows: rows, colGap: colGap, rowGap: rowGap,
		colRep: [2]int{colRepStart, colRepCount}, autoFitCol: ctl.autoFit}
}

func trackSum(sizes []float64, from, n int, gap float64) float64 {
	s := gap * float64(max(n-1, 0))
	for i := from; i < from+n && i < len(sizes); i++ {
		s += sizes[i]
	}
	return s
}

// itemWidth returns the border-box width of an item in an area.
func (e *engine) gridItemWidth(gi *gridItem, st *style, areaW float64) float64 {
	s := gi.st
	if bw, ok := e.borderBoxWidth(s, areaW); ok {
		return bw
	}
	js := s.justifySelf
	if js == "auto" || js == "" {
		js = st.justifyItems
	}
	avail := areaW - s.margin[1] - s.margin[3]
	if (js == "stretch" || js == "normal") && !s.autoMargin[1] && !s.autoMargin[3] && !isReplaced(gi.node) {
		return max(avail, s.extrasH())
	}
	return e.shrinkToFit(gi.node, s, max(avail, 0))
}

func (e *engine) layoutGrid(n *htmlparse.Node, st *style, x, w float64) {
	for _, c := range n.Children {
		if c.Type == htmlparse.ElementNode {
			if cst := e.computeStyle(c, st); cst.display != "none" && (cst.position == "absolute" || cst.position == "fixed") {
				e.addAbsolute(c, cst, x, e.y+e.pendingMargin)
			}
		}
	}
	g := e.setupGrid(n, st, w, true)
	if len(g.items) == 0 {
		return
	}
	e.applyMargin()

	// columns
	_, colW := e.sizeTracks(g.cols, g.items, st, w, true, g.colGap,
		func(gi *gridItem) (int, int) { return gi.col, gi.colSpan },
		func(gi *gridItem) (float64, float64) { return e.measureBlock(gi.node, gi.st) },
		st.justify == "normal" || st.justify == "stretch")
	colGap := g.colGap
	// auto-fit: empty repeated columns collapse
	if g.autoFitCol && g.colRep[1] > 0 {
		used := map[int]bool{}
		for _, gi := range g.items {
			for c := gi.col; c < gi.col+gi.colSpan; c++ {
				used[c] = true
			}
		}
		for c := g.colRep[0]; c < g.colRep[0]+g.colRep[1] && c < len(colW); c++ {
			if !used[c] {
				colW[c] = 0
			}
		}
		_, colW = e.resizeAfterCollapse(g, st, w, colW, used)
	}
	nc := len(colW)
	// column positions with justify-content
	totalW := trackSum(colW, 0, nc, colGap)
	offX, betweenX := contentDistribution(st.justify, w-totalW, nc)
	colX := make([]float64, nc+1)
	cx := x + offX
	for i := 0; i < nc; i++ {
		colX[i] = cx
		gapHere := colGap + betweenX
		if colW[i] == 0 && g.autoFitCol {
			gapHere = 0
		}
		cx += colW[i] + gapHere
	}
	colX[nc] = cx
	areaW := func(gi *gridItem) float64 {
		end := min(gi.col+gi.colSpan, nc)
		return colX[end-1] + colW[end-1] - colX[gi.col]
	}

	// rows: contributions are item heights at their resolved widths
	heights := map[*gridItem]float64{}
	itemW := map[*gridItem]float64{}
	definiteH := e.contentHeight(st)
	_, rowH := e.sizeTracks(g.rows, g.items, st, definiteH, definiteH > 0, g.rowGap,
		func(gi *gridItem) (int, int) { return gi.row, gi.rowSpan },
		func(gi *gridItem) (float64, float64) {
			bw := e.gridItemWidth(gi, st, areaW(gi))
			itemW[gi] = bw
			h, _ := e.measureBox(gi.node, gi.st, bw)
			heights[gi] = h
			h += gi.st.margin[0] + gi.st.margin[2]
			return h, h
		}, st.alignContent == "stretch" || st.alignContent == "normal")
	nr := len(rowH)
	offY, betweenY := 0.0, 0.0
	if definiteH > 0 {
		offY, betweenY = contentDistribution(st.alignContent, definiteH-trackSum(rowH, 0, nr, g.rowGap), nr)
	}
	rowGap := g.rowGap + betweenY

	// pagination by groups of rows tied together by spanning items
	start := e.y
	e.y += offY
	pageH := e.bottom - e.top
	for r := 0; r < nr; {
		end := r + 1
		for changed := true; changed; {
			changed = false
			for _, gi := range g.items {
				if gi.row >= r && gi.row < end && gi.row+gi.rowSpan > end {
					end = min(gi.row+gi.rowSpan, nr)
					changed = true
				}
			}
		}
		groupH := trackSum(rowH, r, end-r, rowGap)
		if r > 0 {
			e.y += rowGap
		}
		if !e.noBreak && !e.dry && e.y+groupH > e.bottom+0.01 && e.y > e.top+0.5 && groupH <= pageH {
			e.newPage()
			e.atTop = false
		}
		flow := !e.noBreak && !e.dry && e.y+groupH > e.bottom+0.01
		groupTop := e.y
		rowTop := make([]float64, end-r+1)
		y := groupTop
		for k := r; k < end; k++ {
			rowTop[k-r] = y
			y += rowH[k] + rowGap
		}
		startPage := e.pageIdx
		endPage, endY := e.pageIdx, groupTop+groupH
		for _, gi := range g.items {
			if gi.row < r || gi.row >= end {
				continue
			}
			s := gi.st
			aw := areaW(gi)
			ah := trackSum(rowH, gi.row, gi.rowSpan, rowGap)
			bw := itemW[gi]
			if bw == 0 {
				bw = e.gridItemWidth(gi, st, aw)
			}
			// horizontal alignment
			js := s.justifySelf
			if js == "auto" || js == "" {
				js = st.justifyItems
			}
			ix := colX[gi.col] + s.margin[3]
			freeX := aw - bw - s.margin[1] - s.margin[3]
			switch {
			case s.autoMargin[3] && s.autoMargin[1]:
				ix += freeX / 2
			case s.autoMargin[3]:
				ix += freeX
			case js == "center":
				ix += freeX / 2
			case js == "end":
				ix += freeX
			}
			// vertical alignment
			al := s.alignSelf
			if al == "auto" || al == "" {
				al = st.alignItems
			}
			h := heights[gi]
			top := rowTop[gi.row-r] + s.margin[0]
			freeY := ah - h - s.margin[0] - s.margin[2]
			minH := 0.0
			switch {
			case s.autoMargin[0] && s.autoMargin[2]:
				top += freeY / 2
			case s.autoMargin[0]:
				top += freeY
			case al == "stretch" && e.boxHeight(s) == 0 && !isReplaced(gi.node):
				minH = ah - s.margin[0] - s.margin[2]
			case al == "center":
				top += freeY / 2
			case al == "end":
				top += freeY
			}
			if flow {
				e.pageIdx, e.y = startPage, top
				e.atTop = false
				e.resetMargin()
				e.renderBox(gi.node, s, ix, bw, 0)
				yy := e.y + s.margin[2]
				if e.pageIdx > endPage || e.pageIdx == endPage && yy > endY {
					endPage, endY = e.pageIdx, yy
				}
				continue
			}
			sv := e.saveState()
			e.noBreak = true
			e.y = top
			e.renderBox(gi.node, s, ix, bw, minH)
			e.restoreState(sv)
		}
		if flow {
			e.pageIdx, e.y = endPage, endY
		} else {
			e.y = groupTop + groupH
		}
		r = end
	}
	if definiteH > 0 && e.pageIdx == e.pageIdx && e.y-start < definiteH {
		e.y = start + definiteH
	}
	e.atTop = false
}

// resizeAfterCollapse redistributes the space freed by collapsed auto-fit
// tracks among the remaining flexible tracks.
func (e *engine) resizeAfterCollapse(g *gridSetup, st *style, w float64, colW []float64, used map[int]bool) ([]float64, []float64) {
	tracks := make([]trackSize, 0, len(g.cols))
	var keep []int
	for i, t := range g.cols {
		if i >= g.colRep[0] && i < g.colRep[0]+g.colRep[1] && !used[i] {
			continue
		}
		tracks = append(tracks, t)
		keep = append(keep, i)
	}
	if len(keep) == len(g.cols) || len(keep) == 0 {
		return nil, colW
	}
	// remap items to the kept columns
	index := map[int]int{}
	for k, i := range keep {
		index[i] = k
	}
	var items []*gridItem
	orig := map[*gridItem][2]int{}
	for _, gi := range g.items {
		orig[gi] = [2]int{gi.col, gi.colSpan}
		gi.col = index[gi.col]
		items = append(items, gi)
	}
	_, sizes := e.sizeTracks(tracks, items, st, w, true, g.colGap,
		func(gi *gridItem) (int, int) { return gi.col, gi.colSpan },
		func(gi *gridItem) (float64, float64) { return e.measureBlock(gi.node, gi.st) },
		st.justify == "normal" || st.justify == "stretch")
	for _, gi := range g.items {
		o := orig[gi]
		gi.col, gi.colSpan = o[0], o[1]
	}
	out := make([]float64, len(colW))
	for k, i := range keep {
		out[i] = sizes[k]
	}
	return nil, out
}

// contentDistribution returns the start offset and extra gap for
// justify-content / align-content with free space over n tracks.
func contentDistribution(mode string, free float64, n int) (float64, float64) {
	if free <= 0 || n == 0 {
		return 0, 0
	}
	switch mode {
	case "end":
		return free, 0
	case "center":
		return free / 2, 0
	case "space-between":
		if n > 1 {
			return 0, free / float64(n-1)
		}
	case "space-around":
		return free / float64(n) / 2, free / float64(n)
	case "space-evenly":
		return free / float64(n+1), free / float64(n+1)
	}
	return 0, 0
}

// measureGrid returns the min-content and max-content widths of a grid
// container's content box.
func (e *engine) measureGrid(n *htmlparse.Node, st *style) (float64, float64) {
	g := e.setupGrid(n, st, 0, false)
	if len(g.items) == 0 {
		return 0, 0
	}
	bases, sizes := e.sizeTracks(g.cols, g.items, st, 0, false, g.colGap,
		func(gi *gridItem) (int, int) { return gi.col, gi.colSpan },
		func(gi *gridItem) (float64, float64) { return e.measureBlock(gi.node, gi.st) }, false)
	return trackSum(bases, 0, len(bases), g.colGap), trackSum(sizes, 0, len(sizes), g.colGap)
}
