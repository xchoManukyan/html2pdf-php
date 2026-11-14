package ttf

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
)

// cmap maps characters to glyphs by reading the subtable bytes directly,
// so even very large (CJK) fonts need no per-character map in memory.
type cmap struct {
	format int
	data   []byte
	symbol bool
}

func parseCmap(b []byte) (cmap, error) {
	if len(b) < 4 {
		return cmap{}, errors.New("ttf: bad cmap")
	}
	n := int(be.Uint16(b[2:]))
	best, bestScore := cmap{}, 0
	for i := 0; i < n; i++ {
		p := 4 + i*8
		if p+8 > len(b) {
			break
		}
		platform, enc := be.Uint16(b[p:]), be.Uint16(b[p+2:])
		off := int(be.Uint32(b[p+4:]))
		if off+4 > len(b) {
			continue
		}
		sub := b[off:]
		format := int(be.Uint16(sub))
		score := 0
		switch {
		case format == 12 && (platform == 3 && enc == 10 || platform == 0):
			score = 5
		case format == 4 && (platform == 3 && enc == 1 || platform == 0):
			score = 4
		case format == 4 && platform == 3 && enc == 0:
			score = 2
		}
		if score > bestScore {
			bestScore = score
			best = cmap{format: format, data: sub, symbol: platform == 3 && enc == 0}
		}
	}
	if bestScore == 0 {
		return cmap{}, errors.New("ttf: no Unicode cmap")
	}
	return best, nil
}

func (c cmap) lookup(r rune) uint16 {
	g := c.lookup1(r)
	if g == 0 && c.symbol && r < 0x100 {
		g = c.lookup1(0xF000 + r)
	}
	return g
}

func (c cmap) lookup1(r rune) uint16 {
	d := c.data
	switch c.format {
	case 4:
		if r > 0xFFFF || len(d) < 14 {
			return 0
		}
		segX2 := int(be.Uint16(d[6:]))
		segs := segX2 / 2
		if 16+4*segX2 > len(d) {
			return 0
		}
		lo, hi := 0, segs-1
		for lo < hi { // first segment with endCode >= r
			mid := (lo + hi) / 2
			if rune(be.Uint16(d[14+mid*2:])) < r {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		i := lo
		end := rune(be.Uint16(d[14+i*2:]))
		start := rune(be.Uint16(d[16+segX2+i*2:]))
		if r < start || r > end {
			return 0
		}
		delta := be.Uint16(d[16+2*segX2+i*2:])
		roPos := 16 + 3*segX2 + i*2
		ro := int(be.Uint16(d[roPos:]))
		if ro == 0 {
			return uint16(r) + delta
		}
		addr := roPos + ro + 2*int(r-start)
		if addr+2 > len(d) {
			return 0
		}
		g := be.Uint16(d[addr:])
		if g == 0 {
			return 0
		}
		return g + delta
	case 12:
		if len(d) < 16 {
			return 0
		}
		n := int(be.Uint32(d[12:]))
		lo, hi := 0, n-1
		for lo <= hi {
			mid := (lo + hi) / 2
			p := 16 + mid*12
			if p+12 > len(d) {
				return 0
			}
			s, e := rune(be.Uint32(d[p:])), rune(be.Uint32(d[p+4:]))
			switch {
			case r < s:
				hi = mid - 1
			case r > e:
				lo = mid + 1
			default:
				return uint16(be.Uint32(d[p+8:]) + uint32(r-s))
			}
		}
	}
	return 0
}

// Coverage loads only the cmap of a face, for cheap "does this font have
// character X" checks while choosing fallback fonts.
type Coverage struct{ c cmap }

// ReadCoverage reads the cmap of one face.
func ReadCoverage(r io.ReaderAt, index int) (*Coverage, error) {
	tables, _, err := readDirectory(r, index)
	if err != nil {
		return nil, err
	}
	rec, ok := tables["cmap"]
	if !ok {
		return nil, errors.New("ttf: no cmap")
	}
	c, err := parseCmap(readTable(r, rec, 0))
	if err != nil {
		return nil, err
	}
	return &Coverage{c}, nil
}

// Has reports whether the face maps r to a glyph.
func (c *Coverage) Has(r rune) bool { return c.c.lookup(r) != 0 }

// DecodeWOFF converts a WOFF 1.0 file to a plain sfnt font. (WOFF2 needs
// Brotli, which the standard library does not provide.)
func DecodeWOFF(data []byte) ([]byte, error) {
	if len(data) < 44 || string(data[:4]) != "wOFF" {
		return nil, errors.New("ttf: not a WOFF file")
	}
	numTables := int(be.Uint16(data[12:]))
	tables := map[string][]byte{}
	for i := 0; i < numTables; i++ {
		p := 44 + i*20
		if p+20 > len(data) {
			return nil, errTruncated
		}
		tag := string(data[p : p+4])
		off, compLen, origLen := be.Uint32(data[p+4:]), be.Uint32(data[p+8:]), be.Uint32(data[p+12:])
		if uint64(off)+uint64(compLen) > uint64(len(data)) {
			return nil, errTruncated
		}
		raw := data[off : off+compLen]
		if compLen < origLen {
			zr, err := zlib.NewReader(bytes.NewReader(raw))
			if err != nil {
				return nil, err
			}
			out, err := io.ReadAll(io.LimitReader(zr, int64(origLen)+1))
			if err != nil || len(out) != int(origLen) {
				return nil, errors.New("ttf: bad WOFF table")
			}
			raw = out
		}
		tables[tag] = raw
	}
	return writeSfnt(tables), nil
}
