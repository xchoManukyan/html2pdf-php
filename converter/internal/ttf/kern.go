package ttf

import (
	"encoding/binary"
	"sync"
)

// Kerning: horizontal pair adjustments from the GPOS "kern" feature
// (PairPos lookups, formats 1 and 2, also behind Extension lookups), or
// from the legacy "kern" table when the font has no GPOS kerning. Browsers
// apply kerning by default, so text widths must include it to wrap lines
// at the same places.

type pairSub struct {
	format         uint16
	data           []byte // the subtable
	coverage       []byte
	vf1, vf2       uint16
	classDef1      []byte
	classDef2      []byte
	class2Count    int
	recordSize     int // bytes per (value1, value2) record
	xAdvanceOffset int // offset of XAdvance in value record 1, -1: none
}

type kerning struct {
	subs   []pairSub
	legacy []byte // format 0 pairs of the "kern" table
}

var kernCache sync.Map // *Font -> *kerning

// Kern returns the adjustment (font units, usually negative) to add to the
// advance of glyph left when it is followed by glyph right.
func (f *Font) Kern(left, right uint16) int {
	k := f.kerning()
	if k == nil {
		return 0
	}
	for i := range k.subs {
		if v, ok := k.subs[i].lookup(left, right); ok {
			return v
		}
	}
	if k.legacy != nil {
		return legacyKern(k.legacy, left, right)
	}
	return 0
}

func (f *Font) kerning() *kerning {
	if v, ok := kernCache.Load(f); ok {
		return v.(*kerning)
	}
	k := &kerning{subs: parseGPOSKern(f.tables["GPOS"])}
	if len(k.subs) == 0 {
		k.legacy = parseLegacyKern(f.tables["kern"])
	}
	if len(k.subs) == 0 && k.legacy == nil {
		k = nil
	}
	v, _ := kernCache.LoadOrStore(f, k)
	return v.(*kerning)
}

func u16(b []byte, off int) uint16 {
	if off < 0 || off+2 > len(b) {
		return 0
	}
	return binary.BigEndian.Uint16(b[off:])
}

func u32(b []byte, off int) uint32 {
	if off < 0 || off+4 > len(b) {
		return 0
	}
	return binary.BigEndian.Uint32(b[off:])
}

func sub(b []byte, off int) []byte {
	if off <= 0 || off >= len(b) {
		return nil
	}
	return b[off:]
}

func parseGPOSKern(gpos []byte) []pairSub {
	if len(gpos) < 10 {
		return nil
	}
	featureList := sub(gpos, int(u16(gpos, 6)))
	lookupList := sub(gpos, int(u16(gpos, 8)))
	if featureList == nil || lookupList == nil {
		return nil
	}
	// lookups referenced by any "kern" feature
	want := map[int]bool{}
	for i, n := 0, int(u16(featureList, 0)); i < n; i++ {
		rec := 2 + 6*i
		if rec+6 > len(featureList) || string(featureList[rec:rec+4]) != "kern" {
			continue
		}
		feat := sub(featureList, int(u16(featureList, rec+4)))
		for j, m := 0, int(u16(feat, 2)); j < m; j++ {
			want[int(u16(feat, 4+2*j))] = true
		}
	}
	var subs []pairSub
	lookupCount := int(u16(lookupList, 0))
	for li := 0; li < lookupCount; li++ {
		if !want[li] {
			continue
		}
		lookup := sub(lookupList, int(u16(lookupList, 2+2*li)))
		if lookup == nil {
			continue
		}
		typ := u16(lookup, 0)
		for si, n := 0, int(u16(lookup, 4)); si < n; si++ {
			st := sub(lookup, int(u16(lookup, 6+2*si)))
			t := typ
			if t == 9 { // Extension: format, extensionLookupType, offset32
				t = u16(st, 2)
				st = sub(st, int(u32(st, 4)))
			}
			if t != 2 || st == nil {
				continue
			}
			if p, ok := parsePairPos(st); ok {
				subs = append(subs, p)
			}
		}
	}
	return subs
}

func valueSize(vf uint16) int {
	n := 0
	for b := vf & 0xff; b != 0; b >>= 1 {
		n += int(b & 1)
	}
	return 2 * n
}

func parsePairPos(st []byte) (pairSub, bool) {
	p := pairSub{format: u16(st, 0), data: st}
	p.coverage = sub(st, int(u16(st, 2)))
	p.vf1, p.vf2 = u16(st, 4), u16(st, 6)
	p.recordSize = valueSize(p.vf1) + valueSize(p.vf2)
	p.xAdvanceOffset = -1
	if p.vf1&0x0004 != 0 {
		p.xAdvanceOffset = valueSize(p.vf1 & 0x0003)
	}
	if p.coverage == nil || p.xAdvanceOffset < 0 {
		return p, false // nothing that changes advances
	}
	switch p.format {
	case 1:
		return p, true
	case 2:
		p.classDef1 = sub(st, int(u16(st, 8)))
		p.classDef2 = sub(st, int(u16(st, 10)))
		p.class2Count = int(u16(st, 14))
		return p, true
	}
	return p, false
}

func (p *pairSub) lookup(left, right uint16) (int, bool) {
	ci := coverageIndex(p.coverage, left)
	if ci < 0 {
		return 0, false
	}
	switch p.format {
	case 1:
		set := sub(p.data, int(u16(p.data, 10+2*ci)))
		if set == nil {
			return 0, false
		}
		n := int(u16(set, 0))
		rec := 2 + p.recordSize
		lo, hi := 0, n-1
		for lo <= hi {
			mid := (lo + hi) / 2
			off := 2 + mid*rec
			g := u16(set, off)
			switch {
			case g == right:
				return int(int16(u16(set, off+2+p.xAdvanceOffset))), true
			case g < right:
				lo = mid + 1
			default:
				hi = mid - 1
			}
		}
		return 0, false
	case 2:
		c1 := classOf(p.classDef1, left)
		c2 := classOf(p.classDef2, right)
		if c2 >= p.class2Count {
			return 0, true
		}
		off := 16 + (c1*p.class2Count+c2)*p.recordSize
		return int(int16(u16(p.data, off+p.xAdvanceOffset))), true
	}
	return 0, false
}

func coverageIndex(cov []byte, g uint16) int {
	switch u16(cov, 0) {
	case 1:
		lo, hi := 0, int(u16(cov, 2))-1
		for lo <= hi {
			mid := (lo + hi) / 2
			v := u16(cov, 4+2*mid)
			switch {
			case v == g:
				return mid
			case v < g:
				lo = mid + 1
			default:
				hi = mid - 1
			}
		}
	case 2:
		lo, hi := 0, int(u16(cov, 2))-1
		for lo <= hi {
			mid := (lo + hi) / 2
			off := 4 + 6*mid
			start, end := u16(cov, off), u16(cov, off+2)
			switch {
			case g < start:
				hi = mid - 1
			case g > end:
				lo = mid + 1
			default:
				return int(u16(cov, off+4)) + int(g-start)
			}
		}
	}
	return -1
}

func classOf(cd []byte, g uint16) int {
	switch u16(cd, 0) {
	case 1:
		start, n := u16(cd, 2), int(u16(cd, 4))
		if g >= start && int(g-start) < n {
			return int(u16(cd, 6+2*int(g-start)))
		}
	case 2:
		lo, hi := 0, int(u16(cd, 2))-1
		for lo <= hi {
			mid := (lo + hi) / 2
			off := 4 + 6*mid
			start, end := u16(cd, off), u16(cd, off+2)
			switch {
			case g < start:
				hi = mid - 1
			case g > end:
				lo = mid + 1
			default:
				return int(u16(cd, off+4))
			}
		}
	}
	return 0
}

// parseLegacyKern returns the pair list of the first horizontal format 0
// subtable of a version 0 "kern" table.
func parseLegacyKern(kern []byte) []byte {
	if len(kern) < 4 || u16(kern, 0) != 0 {
		return nil
	}
	off := 4
	for i, n := 0, int(u16(kern, 2)); i < n && off+6 <= len(kern); i++ {
		length := int(u16(kern, off+2))
		coverage := u16(kern, off+4)
		if coverage>>8 == 0 && coverage&1 == 1 && off+14 <= len(kern) {
			pairs := int(u16(kern, off+6))
			start := off + 14
			if start+6*pairs <= len(kern) {
				return kern[start : start+6*pairs]
			}
		}
		if length <= 0 {
			break
		}
		off += length
	}
	return nil
}

func legacyKern(pairs []byte, left, right uint16) int {
	key := uint32(left)<<16 | uint32(right)
	lo, hi := 0, len(pairs)/6-1
	for lo <= hi {
		mid := (lo + hi) / 2
		k := u32(pairs, 6*mid)
		switch {
		case k == key:
			return int(int16(u16(pairs, 6*mid+4)))
		case k < key:
			lo = mid + 1
		default:
			hi = mid - 1
		}
	}
	return 0
}
