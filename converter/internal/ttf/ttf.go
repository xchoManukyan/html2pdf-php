// Package ttf reads TrueType / OpenType (TrueType outline) fonts and TrueType
// collections, and produces glyph subsets for embedding in PDF files.
// Everything is implemented with the Go standard library.
package ttf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf16"
)

var be = binary.BigEndian

type tableRec struct {
	offset, length uint32
}

// Font is a parsed font face.
type Font struct {
	data   []byte
	tables map[string][]byte

	UnitsPerEm int
	NumGlyphs  int
	// Vertical metrics in font units (Descent is negative).
	Ascent, Descent, LineGap int
	CapHeight                int
	BBox                     [4]int
	ItalicAngle              float64
	FixedPitch               bool
	Weight                   int
	Italic                   bool
	FsType                   uint16

	Family, Subfamily, FullName, PostScriptName string

	numHMetrics int
	locaLong    bool
	cmap        cmap
}

// Info describes a face without loading its glyph data (used for scanning
// font directories quickly).
type Info struct {
	Index          int
	Family         string // typographic family (name ID 16) or legacy family (ID 1)
	LegacyFamily   string // name ID 1
	Subfamily      string
	FullName       string
	PostScriptName string
	Weight         int
	Stretch        int // OS/2 width class, 5 = normal
	Italic         bool
	TrueType       bool // has glyf outlines (CFF fonts cannot be embedded here)
	Embeddable     bool
}

var (
	ErrNotFont   = errors.New("ttf: not a TrueType/OpenType font")
	ErrCFF       = errors.New("ttf: CFF-based OpenType fonts are not supported")
	errTruncated = errors.New("ttf: truncated table")
)

// readDirectory reads the table directory of the face at index (TTC aware).
func readDirectory(r io.ReaderAt, index int) (map[string]tableRec, uint32, error) {
	var hdr [12]byte
	if _, err := r.ReadAt(hdr[:], 0); err != nil {
		return nil, 0, ErrNotFont
	}
	off := int64(0)
	if string(hdr[:4]) == "ttcf" {
		n := int(be.Uint32(hdr[8:]))
		if index < 0 || index >= n {
			return nil, 0, fmt.Errorf("ttf: collection has %d faces, index %d", n, index)
		}
		var o [4]byte
		if _, err := r.ReadAt(o[:], 12+int64(index)*4); err != nil {
			return nil, 0, ErrNotFont
		}
		off = int64(be.Uint32(o[:]))
		if _, err := r.ReadAt(hdr[:], off); err != nil {
			return nil, 0, ErrNotFont
		}
	}
	version := be.Uint32(hdr[:4])
	if version != 0x00010000 && string(hdr[:4]) != "true" && string(hdr[:4]) != "OTTO" {
		return nil, 0, ErrNotFont
	}
	numTables := int(be.Uint16(hdr[4:]))
	if numTables > 200 {
		return nil, 0, ErrNotFont
	}
	dir := make([]byte, numTables*16)
	if _, err := r.ReadAt(dir, off+12); err != nil {
		return nil, 0, ErrNotFont
	}
	tables := map[string]tableRec{}
	for i := 0; i < numTables; i++ {
		rec := dir[i*16:]
		tables[string(rec[:4])] = tableRec{be.Uint32(rec[8:]), be.Uint32(rec[12:])}
	}
	return tables, version, nil
}

// NumFaces returns how many faces a font file holds (1 unless it is a TTC).
func NumFaces(r io.ReaderAt) int {
	var hdr [12]byte
	if _, err := r.ReadAt(hdr[:], 0); err != nil {
		return 0
	}
	if string(hdr[:4]) == "ttcf" {
		return int(be.Uint32(hdr[8:]))
	}
	return 1
}

func readTable(r io.ReaderAt, rec tableRec, limit uint32) []byte {
	n := rec.length
	if limit > 0 && n > limit {
		n = limit
	}
	b := make([]byte, n)
	if _, err := r.ReadAt(b, int64(rec.offset)); err != nil && err != io.EOF {
		return nil
	}
	return b
}

// ReadInfo reads the naming and style information of one face.
func ReadInfo(r io.ReaderAt, index int) (*Info, error) {
	tables, _, err := readDirectory(r, index)
	if err != nil {
		return nil, err
	}
	info := &Info{Index: index, Weight: 400, Stretch: 5}
	_, info.TrueType = tables["glyf"]
	if rec, ok := tables["name"]; ok {
		names := parseNames(readTable(r, rec, 1<<20))
		info.LegacyFamily = names[1]
		info.Family = names[16]
		if info.Family == "" {
			info.Family = names[1]
		}
		info.Subfamily = names[17]
		if info.Subfamily == "" {
			info.Subfamily = names[2]
		}
		info.FullName = names[4]
		info.PostScriptName = names[6]
	}
	info.Embeddable = true
	if rec, ok := tables["OS/2"]; ok {
		b := readTable(r, rec, 100)
		if len(b) >= 64 {
			info.Weight = int(be.Uint16(b[4:]))
			info.Stretch = int(be.Uint16(b[6:]))
			fsType := be.Uint16(b[8:])
			info.Embeddable = fsType&0x000F != 0x0002
			info.Italic = be.Uint16(b[62:])&0x0001 != 0
		}
	} else if rec, ok := tables["head"]; ok {
		b := readTable(r, rec, 54)
		if len(b) >= 46 {
			info.Italic = be.Uint16(b[44:])&0x2 != 0
			if be.Uint16(b[44:])&0x1 != 0 {
				info.Weight = 700
			}
		}
	}
	if info.Weight < 100 || info.Weight > 1000 {
		info.Weight = 400
	}
	return info, nil
}

func parseNames(b []byte) map[int]string {
	out := map[int]string{}
	if len(b) < 6 {
		return out
	}
	count := int(be.Uint16(b[2:]))
	strOff := int(be.Uint16(b[4:]))
	prio := map[int]int{}
	for i := 0; i < count; i++ {
		p := 6 + i*12
		if p+12 > len(b) {
			break
		}
		platform := be.Uint16(b[p:])
		enc := be.Uint16(b[p+2:])
		lang := be.Uint16(b[p+4:])
		id := int(be.Uint16(b[p+6:]))
		ln := int(be.Uint16(b[p+8:]))
		off := strOff + int(be.Uint16(b[p+10:]))
		if off+ln > len(b) {
			continue
		}
		raw := b[off : off+ln]
		var s string
		score := 0
		switch {
		case platform == 3 && (enc == 1 || enc == 10 || enc == 0):
			u := make([]uint16, ln/2)
			for j := range u {
				u[j] = be.Uint16(raw[j*2:])
			}
			s = string(utf16.Decode(u))
			score = 2
			if lang == 0x409 {
				score = 3
			}
		case platform == 0:
			u := make([]uint16, ln/2)
			for j := range u {
				u[j] = be.Uint16(raw[j*2:])
			}
			s = string(utf16.Decode(u))
			score = 2
		case platform == 1 && enc == 0:
			s = string(raw)
			score = 1
		default:
			continue
		}
		if score > prio[id] {
			prio[id] = score
			out[id] = s
		}
	}
	return out
}

// Parse loads a font face from memory. index selects the face of a TTC.
func Parse(data []byte, index int) (*Font, error) {
	recs, version, err := readDirectory(bytes.NewReader(data), index)
	if err != nil {
		return nil, err
	}
	if version == 0x4F54544F { // OTTO
		return nil, ErrCFF
	}
	f := &Font{data: data, tables: map[string][]byte{}}
	for tag, rec := range recs {
		end := uint64(rec.offset) + uint64(rec.length)
		if end > uint64(len(data)) {
			return nil, errTruncated
		}
		f.tables[tag] = data[rec.offset:end]
	}
	for _, t := range []string{"head", "hhea", "maxp", "hmtx", "cmap", "loca", "glyf"} {
		if f.tables[t] == nil {
			if t == "glyf" || t == "loca" {
				return nil, ErrCFF
			}
			return nil, fmt.Errorf("ttf: missing %s table", t)
		}
	}
	head := f.tables["head"]
	if len(head) < 54 {
		return nil, errTruncated
	}
	f.UnitsPerEm = int(be.Uint16(head[18:]))
	if f.UnitsPerEm == 0 {
		f.UnitsPerEm = 1000
	}
	f.BBox = [4]int{int(int16(be.Uint16(head[36:]))), int(int16(be.Uint16(head[38:]))),
		int(int16(be.Uint16(head[40:]))), int(int16(be.Uint16(head[42:])))}
	f.locaLong = be.Uint16(head[50:]) == 1
	macStyle := be.Uint16(head[44:])

	hhea := f.tables["hhea"]
	if len(hhea) < 36 {
		return nil, errTruncated
	}
	f.Ascent = int(int16(be.Uint16(hhea[4:])))
	f.Descent = int(int16(be.Uint16(hhea[6:])))
	f.LineGap = int(int16(be.Uint16(hhea[8:])))
	f.numHMetrics = int(be.Uint16(hhea[34:]))

	maxp := f.tables["maxp"]
	if len(maxp) < 6 {
		return nil, errTruncated
	}
	f.NumGlyphs = int(be.Uint16(maxp[4:]))
	if f.numHMetrics == 0 || f.numHMetrics > f.NumGlyphs || len(f.tables["hmtx"]) < f.numHMetrics*4 {
		return nil, errTruncated
	}

	f.Weight = 400
	if macStyle&1 != 0 {
		f.Weight = 700
	}
	f.Italic = macStyle&2 != 0
	f.CapHeight = f.Ascent * 7 / 10
	if os2 := f.tables["OS/2"]; len(os2) >= 78 {
		f.Weight = int(be.Uint16(os2[4:]))
		f.FsType = be.Uint16(os2[8:])
		fsSel := be.Uint16(os2[62:])
		f.Italic = fsSel&1 != 0
		typoAsc := int(int16(be.Uint16(os2[68:])))
		typoDesc := int(int16(be.Uint16(os2[70:])))
		typoGap := int(int16(be.Uint16(os2[72:])))
		winAsc := int(be.Uint16(os2[74:]))
		winDesc := int(be.Uint16(os2[76:]))
		switch {
		case fsSel&0x80 != 0 && typoAsc != 0: // USE_TYPO_METRICS
			f.Ascent, f.Descent, f.LineGap = typoAsc, typoDesc, typoGap
		case f.Ascent == 0 && f.Descent == 0:
			f.Ascent, f.Descent, f.LineGap = winAsc, -winDesc, 0
		}
		if len(os2) >= 90 && be.Uint16(os2[0:]) >= 2 {
			if ch := int(int16(be.Uint16(os2[88:]))); ch > 0 {
				f.CapHeight = ch
			}
		}
	}
	if post := f.tables["post"]; len(post) >= 16 {
		f.ItalicAngle = float64(int32(be.Uint32(post[4:]))) / 65536
		f.FixedPitch = be.Uint32(post[12:]) != 0
	}
	names := parseNames(f.tables["name"])
	f.Family = names[16]
	if f.Family == "" {
		f.Family = names[1]
	}
	f.Subfamily = names[2]
	f.FullName = names[4]
	f.PostScriptName = names[6]

	f.cmap, err = parseCmap(f.tables["cmap"])
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Embeddable reports whether the license bits allow embedding.
func (f *Font) Embeddable() bool { return f.FsType&0x000F != 0x0002 }

// GlyphIndex maps a character to a glyph (0 = missing).
func (f *Font) GlyphIndex(r rune) uint16 {
	g := f.cmap.lookup(r)
	if int(g) >= f.NumGlyphs {
		return 0
	}
	return g
}

// Advance returns the advance width of a glyph in font units.
func (f *Font) Advance(g uint16) int {
	hmtx := f.tables["hmtx"]
	i := int(g)
	if i >= f.numHMetrics {
		i = f.numHMetrics - 1
	}
	return int(be.Uint16(hmtx[i*4:]))
}

func (f *Font) glyphData(g uint16) []byte {
	loca, glyf := f.tables["loca"], f.tables["glyf"]
	i := int(g)
	var start, end int
	if f.locaLong {
		if (i+2)*4 > len(loca) {
			return nil
		}
		start, end = int(be.Uint32(loca[i*4:])), int(be.Uint32(loca[i*4+4:]))
	} else {
		if (i+2)*2 > len(loca) {
			return nil
		}
		start, end = int(be.Uint16(loca[i*2:]))*2, int(be.Uint16(loca[i*2+2:]))*2
	}
	if start >= end || end > len(glyf) {
		return nil
	}
	return glyf[start:end]
}

// componentGlyphs lists the glyphs a composite glyph is built from.
func componentGlyphs(data []byte) []uint16 {
	if len(data) < 10 || int16(be.Uint16(data)) >= 0 {
		return nil
	}
	var out []uint16
	p := 10
	for p+4 <= len(data) {
		flags := be.Uint16(data[p:])
		out = append(out, be.Uint16(data[p+2:]))
		p += 4
		if flags&0x0001 != 0 {
			p += 4
		} else {
			p += 2
		}
		switch {
		case flags&0x0008 != 0:
			p += 2
		case flags&0x0040 != 0:
			p += 4
		case flags&0x0080 != 0:
			p += 8
		}
		if flags&0x0020 == 0 {
			break
		}
	}
	return out
}

// Subset builds a font file containing only the given glyphs. Glyph IDs are
// kept unchanged (unused glyphs become empty), so a PDF can address glyphs
// with an identity CID mapping.
func (f *Font) Subset(used map[uint16]bool) []byte {
	keep := map[uint16]bool{0: true}
	var stack []uint16
	for g := range used {
		stack = append(stack, g)
	}
	for len(stack) > 0 {
		g := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if keep[g] || int(g) >= f.NumGlyphs {
			continue
		}
		keep[g] = true
		stack = append(stack, componentGlyphs(f.glyphData(g))...)
	}
	for _, c := range componentGlyphs(f.glyphData(0)) {
		keep[c] = true
	}

	var glyf bytes.Buffer
	loca := make([]byte, (f.NumGlyphs+1)*4)
	for g := 0; g < f.NumGlyphs; g++ {
		be.PutUint32(loca[g*4:], uint32(glyf.Len()))
		if keep[uint16(g)] {
			d := f.glyphData(uint16(g))
			glyf.Write(d)
			for glyf.Len()%4 != 0 {
				glyf.WriteByte(0)
			}
		}
	}
	be.PutUint32(loca[f.NumGlyphs*4:], uint32(glyf.Len()))

	head := append([]byte(nil), f.tables["head"]...)
	be.PutUint32(head[8:], 0)  // checkSumAdjustment, fixed below
	be.PutUint16(head[50:], 1) // long loca offsets

	tables := map[string][]byte{
		"head": head, "hhea": f.tables["hhea"], "maxp": f.tables["maxp"],
		"hmtx": f.tables["hmtx"], "loca": loca, "glyf": glyf.Bytes(),
	}
	for _, t := range []string{"cvt ", "fpgm", "prep", "OS/2"} {
		if b := f.tables[t]; b != nil {
			tables[t] = b
		}
	}
	out := writeSfnt(tables)
	// checkSumAdjustment = 0xB1B0AFBA - checksum(entire font)
	adj := 0xB1B0AFBA - checksum(out)
	for i := 12; i < 12+16*len(tables); i += 16 {
		if string(out[i:i+4]) == "head" {
			off := be.Uint32(out[i+8:])
			be.PutUint32(out[off+8:], adj)
		}
	}
	return out
}

func checksum(b []byte) uint32 {
	var sum uint32
	for i := 0; i < len(b); i += 4 {
		var w [4]byte
		copy(w[:], b[i:])
		sum += be.Uint32(w[:])
	}
	return sum
}

func writeSfnt(tables map[string][]byte) []byte {
	tags := make([]string, 0, len(tables))
	for t := range tables {
		tags = append(tags, t)
	}
	// sort tags (stdlib sort avoided for tiny n)
	for i := 1; i < len(tags); i++ {
		for j := i; j > 0 && tags[j] < tags[j-1]; j-- {
			tags[j], tags[j-1] = tags[j-1], tags[j]
		}
	}
	n := len(tags)
	entrySel := 0
	for (1 << (entrySel + 1)) <= n {
		entrySel++
	}
	searchRange := (1 << entrySel) * 16
	var buf bytes.Buffer
	hdr := make([]byte, 12)
	be.PutUint32(hdr, 0x00010000)
	be.PutUint16(hdr[4:], uint16(n))
	be.PutUint16(hdr[6:], uint16(searchRange))
	be.PutUint16(hdr[8:], uint16(entrySel))
	be.PutUint16(hdr[10:], uint16(n*16-searchRange))
	buf.Write(hdr)
	dir := make([]byte, n*16)
	offset := 12 + n*16
	var body bytes.Buffer
	for i, t := range tags {
		data := tables[t]
		copy(dir[i*16:], t)
		be.PutUint32(dir[i*16+4:], checksum(data))
		be.PutUint32(dir[i*16+8:], uint32(offset+body.Len()))
		be.PutUint32(dir[i*16+12:], uint32(len(data)))
		body.Write(data)
		for body.Len()%4 != 0 {
			body.WriteByte(0)
		}
	}
	buf.Write(dir)
	buf.Write(body.Bytes())
	return buf.Bytes()
}
