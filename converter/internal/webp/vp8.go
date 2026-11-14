package webp

import (
	"encoding/binary"
	"errors"
	"image"
)

// VP8 key-frame decoder (RFC 6386): boolean entropy decoding, intra
// prediction, residual decoding with dequantization, inverse WHT/DCT,
// loop filtering, and YUV 4:2:0 to RGB conversion.

var errVP8 = errors.New("webp: invalid lossy bitstream")

// ---------------------------------------------------------------------
// Boolean entropy decoder (RFC 6386 section 7)

type boolDecoder struct {
	data     []byte
	pos      int
	value    uint32
	rng      uint32
	bitCount int
}

func newBoolDecoder(b []byte) *boolDecoder {
	d := &boolDecoder{data: b, rng: 255}
	for i := 0; i < 2; i++ {
		d.value <<= 8
		if d.pos < len(b) {
			d.value |= uint32(b[d.pos])
			d.pos++
		}
	}
	return d
}

func (d *boolDecoder) bit(prob uint8) bool {
	split := 1 + (((d.rng - 1) * uint32(prob)) >> 8)
	big := split << 8
	var r bool
	if d.value >= big {
		r = true
		d.rng -= split
		d.value -= big
	} else {
		d.rng = split
	}
	for d.rng < 128 {
		d.value <<= 1
		d.rng <<= 1
		d.bitCount++
		if d.bitCount == 8 {
			d.bitCount = 0
			if d.pos < len(d.data) {
				d.value |= uint32(d.data[d.pos])
				d.pos++
			}
		}
	}
	return r
}

func (d *boolDecoder) literal(n int) int {
	v := 0
	for i := 0; i < n; i++ {
		v <<= 1
		if d.bit(128) {
			v |= 1
		}
	}
	return v
}

func (d *boolDecoder) signed(n int) int {
	v := d.literal(n)
	if d.bit(128) {
		return -v
	}
	return v
}

func (d *boolDecoder) flag() bool { return d.bit(128) }

// ---------------------------------------------------------------------
// Constants

const (
	dcPred = iota
	vPred
	hPred
	tmPred
	bPred
)

const (
	bDC = iota
	bTM
	bVE
	bHE
	bLD
	bRD
	bVR
	bVL
	bHD
	bHU
)

var (
	zigzag = [16]int{0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15}
	bands  = [17]int{0, 1, 2, 3, 6, 4, 5, 6, 6, 6, 6, 6, 6, 6, 6, 7, 0}

	pcat = [4][]uint8{
		{173, 148, 140},
		{176, 155, 140, 135},
		{180, 157, 141, 134, 130},
		{254, 254, 243, 230, 196, 177, 153, 140, 133, 130, 129},
	}
	pcatBase = [4]int32{11, 19, 35, 67}

	// subblock mode tree (RFC 6386 11.2)
	bmodeTree = [18]int8{-bDC, 2, -bTM, 4, -bVE, 6, 8, 12, -bHE, 10, -bRD, -bVR, -bLD, 14, -bVL, 16, -bHD, -bHU}
)

func readTree(d *boolDecoder, tree []int8, probs []uint8) int {
	i := 0
	for {
		b := 0
		if d.bit(probs[i>>1]) {
			b = 1
		}
		i = int(tree[i+b])
		if i <= 0 {
			return -i
		}
	}
}

// ---------------------------------------------------------------------
// Decoder state

type quant struct {
	y1, y2, uv [2]int32 // dc, ac
}

type filterInfo struct {
	limit, ilevel, hevThresh int
	inner                    bool
}

type vp8Decoder struct {
	w, h, mbw, mbh int

	segEnabled bool
	updateMap  bool
	segAbs     bool
	segQuant   [4]int
	segFilter  [4]int
	segProbs   [3]uint8

	filterType  int // 0 normal, 1 simple
	filterLevel int
	sharpness   int
	lfDelta     bool
	refDelta    [4]int
	modeDelta   [4]int

	q          [4]quant
	coeffProbs [4][8][3][11]uint8
	useSkip    bool
	skipProb   uint8

	y, u, v          []byte
	yStride, cStride int

	// per-MB info kept for the loop filter
	mbFilter []filterInfo
}

func clampQ(q int) int { return max(0, min(127, q)) }

func decodeVP8(data []byte) (image.Image, error) {
	if len(data) < 10 {
		return nil, errVP8
	}
	tag := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16
	if tag&1 != 0 {
		return nil, errUnsupported // not a key frame
	}
	firstSize := int(tag >> 5)
	if data[3] != 0x9d || data[4] != 0x01 || data[5] != 0x2a {
		return nil, errVP8
	}
	d := &vp8Decoder{
		w: int(binary.LittleEndian.Uint16(data[6:]) & 0x3fff),
		h: int(binary.LittleEndian.Uint16(data[8:]) & 0x3fff),
	}
	if d.w == 0 || d.h == 0 || d.w*d.h > maxPixels {
		return nil, errVP8
	}
	data = data[10:]
	if firstSize > len(data) {
		return nil, errVP8
	}
	br := newBoolDecoder(data[:firstSize])
	d.mbw, d.mbh = (d.w+15)/16, (d.h+15)/16

	// frame header (RFC 6386 9.2 - 9.11, 19.2)
	br.literal(1) // color space
	br.literal(1) // clamping type
	d.segEnabled = br.flag()
	if d.segEnabled {
		d.updateMap = br.flag()
		if br.flag() { // update segment feature data
			d.segAbs = br.flag()
			for i := range d.segQuant {
				if br.flag() {
					d.segQuant[i] = br.signed(7)
				}
			}
			for i := range d.segFilter {
				if br.flag() {
					d.segFilter[i] = br.signed(6)
				}
			}
		}
		if d.updateMap {
			for i := range d.segProbs {
				d.segProbs[i] = 255
				if br.flag() {
					d.segProbs[i] = uint8(br.literal(8))
				}
			}
		}
	}
	d.filterType = br.literal(1)
	d.filterLevel = br.literal(6)
	d.sharpness = br.literal(3)
	d.lfDelta = br.flag()
	if d.lfDelta && br.flag() {
		for i := range d.refDelta {
			if br.flag() {
				d.refDelta[i] = br.signed(6)
			}
		}
		for i := range d.modeDelta {
			if br.flag() {
				d.modeDelta[i] = br.signed(6)
			}
		}
	}
	numParts := 1 << br.literal(2)
	partData := data[firstSize:]
	if len(partData) < 3*(numParts-1) {
		return nil, errVP8
	}
	parts := make([]*boolDecoder, numParts)
	sizes := partData[:3*(numParts-1)]
	rest := partData[3*(numParts-1):]
	for i := 0; i < numParts; i++ {
		n := len(rest)
		if i < numParts-1 {
			n = int(sizes[3*i]) | int(sizes[3*i+1])<<8 | int(sizes[3*i+2])<<16
			if n > len(rest) {
				n = len(rest)
			}
		}
		parts[i] = newBoolDecoder(rest[:n])
		rest = rest[n:]
	}

	// quantizer indices
	base := br.literal(7)
	var deltas [5]int
	for i := range deltas {
		if br.flag() {
			deltas[i] = br.signed(4)
		}
	}
	for s := 0; s < 4; s++ {
		q := base
		if d.segEnabled {
			if d.segAbs {
				q = d.segQuant[s]
			} else {
				q += d.segQuant[s]
			}
		}
		qq := &d.q[s]
		qq.y1 = [2]int32{dcQLookup[clampQ(q+deltas[0])], acQLookup[clampQ(q)]}
		qq.y2 = [2]int32{dcQLookup[clampQ(q+deltas[1])] * 2, acQLookup[clampQ(q+deltas[2])] * 155 / 100}
		if qq.y2[1] < 8 {
			qq.y2[1] = 8
		}
		qq.uv = [2]int32{dcQLookup[clampQ(q+deltas[3])], acQLookup[clampQ(q+deltas[4])]}
		if qq.uv[0] > 132 {
			qq.uv[0] = 132
		}
	}
	br.flag() // refresh entropy probs (irrelevant for a single frame)

	// token probabilities
	for i := 0; i < 4; i++ {
		for j := 0; j < 8; j++ {
			for k := 0; k < 3; k++ {
				for l := 0; l < 11; l++ {
					idx := ((i*8+j)*3+k)*11 + l
					d.coeffProbs[i][j][k][l] = defaultCoeffProbsFlat[idx]
					if br.bit(coeffUpdateProbsFlat[idx]) {
						d.coeffProbs[i][j][k][l] = uint8(br.literal(8))
					}
				}
			}
		}
	}
	d.useSkip = br.flag()
	if d.useSkip {
		d.skipProb = uint8(br.literal(8))
	}

	d.yStride = d.mbw * 16
	d.cStride = d.mbw * 8
	d.y = make([]byte, d.yStride*d.mbh*16)
	d.u = make([]byte, d.cStride*d.mbh*8)
	d.v = make([]byte, d.cStride*d.mbh*8)
	d.mbFilter = make([]filterInfo, d.mbw*d.mbh)

	if err := d.decodeMacroblocks(br, parts); err != nil {
		return nil, err
	}
	if d.filterLevel > 0 || d.segEnabled {
		d.loopFilter()
	}
	return d.toRGB(), nil
}

// ---------------------------------------------------------------------
// Macroblocks

type mbContext struct {
	nzY   [4]bool
	nzU   [2]bool
	nzV   [2]bool
	nzY2  bool
	modes [4]uint8 // subblock modes along the edge (for B_PRED contexts)
}

func (d *vp8Decoder) filterFor(seg int, i4x4, skip bool) filterInfo {
	level := d.filterLevel
	if d.segEnabled {
		if d.segAbs {
			level = d.segFilter[seg]
		} else {
			level += d.segFilter[seg]
		}
	}
	if d.lfDelta {
		level += d.refDelta[0]
		if i4x4 {
			level += d.modeDelta[0]
		}
	}
	level = max(0, min(63, level))
	f := filterInfo{inner: i4x4 || !skip}
	if level == 0 {
		return f
	}
	ilevel := level
	if d.sharpness > 0 {
		if d.sharpness > 4 {
			ilevel >>= 2
		} else {
			ilevel >>= 1
		}
		if ilevel > 9-d.sharpness {
			ilevel = 9 - d.sharpness
		}
	}
	if ilevel < 1 {
		ilevel = 1
	}
	f.ilevel = ilevel
	f.limit = 2*level + ilevel
	switch {
	case level >= 40:
		f.hevThresh = 2
	case level >= 15:
		f.hevThresh = 1
	}
	return f
}

func (d *vp8Decoder) decodeMacroblocks(br *boolDecoder, parts []*boolDecoder) error {
	above := make([]mbContext, d.mbw)
	for i := range above {
		above[i].modes = [4]uint8{bDC, bDC, bDC, bDC}
	}
	var coeffs [25][16]int32
	var nonzero [25]bool
	for mby := 0; mby < d.mbh; mby++ {
		tp := parts[mby%len(parts)]
		left := mbContext{modes: [4]uint8{bDC, bDC, bDC, bDC}}
		for mbx := 0; mbx < d.mbw; mbx++ {
			// --- modes (first partition)
			seg := 0
			if d.updateMap {
				if br.bit(d.segProbs[0]) {
					seg = 2
					if br.bit(d.segProbs[2]) {
						seg = 3
					}
				} else if br.bit(d.segProbs[1]) {
					seg = 1
				}
			}
			skip := false
			if d.useSkip {
				skip = br.bit(d.skipProb)
			}
			var ymode int
			if !br.bit(145) {
				ymode = bPred
			} else if !br.bit(156) {
				if br.bit(163) {
					ymode = vPred
				} else {
					ymode = dcPred
				}
			} else if br.bit(128) {
				ymode = tmPred
			} else {
				ymode = hPred
			}
			var bmodes [16]uint8
			a := &above[mbx]
			if ymode == bPred {
				for i := 0; i < 16; i++ {
					r, c := i>>2, i&3
					top := a.modes[c]
					if r > 0 {
						top = bmodes[i-4]
					}
					lft := left.modes[r]
					if c > 0 {
						lft = bmodes[i-1]
					}
					off := (int(top)*10 + int(lft)) * 9
					bmodes[i] = uint8(readTree(br, bmodeTree[:], kfBmodeProbsFlat[off:off+9]))
				}
				for c := 0; c < 4; c++ {
					a.modes[c] = bmodes[12+c]
				}
				for r := 0; r < 4; r++ {
					left.modes[r] = bmodes[r*4+3]
				}
			} else {
				implied := map[int]uint8{dcPred: bDC, vPred: bVE, hPred: bHE, tmPred: bTM}[ymode]
				a.modes = [4]uint8{implied, implied, implied, implied}
				left.modes = a.modes
			}
			uvmode := dcPred
			if br.bit(142) {
				if !br.bit(114) {
					uvmode = vPred
				} else if br.bit(183) {
					uvmode = tmPred
				} else {
					uvmode = hPred
				}
			}

			// --- residuals (token partition)
			for i := range coeffs {
				coeffs[i] = [16]int32{}
				nonzero[i] = false
			}
			q := &d.q[seg]
			if !skip {
				first := 0
				ytype := 3
				if ymode != bPred {
					ctx := btoi(a.nzY2) + btoi(left.nzY2)
					nz := d.decodeCoeffs(tp, 1, ctx, 0, q.y2, &coeffs[24])
					a.nzY2, left.nzY2 = nz, nz
					if nz {
						var dc [16]int32
						inverseWHT(&coeffs[24], &dc)
						for i := 0; i < 16; i++ {
							coeffs[i][0] = dc[i]
							if dc[i] != 0 {
								nonzero[i] = true
							}
						}
					}
					first, ytype = 1, 0
				}
				for i := 0; i < 16; i++ {
					r, c := i>>2, i&3
					ctx := btoi(a.nzY[c]) + btoi(left.nzY[r])
					nz := d.decodeCoeffs(tp, ytype, ctx, first, q.y1, &coeffs[i])
					a.nzY[c], left.nzY[r] = nz, nz
					nonzero[i] = nonzero[i] || nz
				}
				for i := 0; i < 4; i++ {
					r, c := i>>1, i&1
					ctx := btoi(a.nzU[c]) + btoi(left.nzU[r])
					nz := d.decodeCoeffs(tp, 2, ctx, 0, q.uv, &coeffs[16+i])
					a.nzU[c], left.nzU[r] = nz, nz
					nonzero[16+i] = nz
				}
				for i := 0; i < 4; i++ {
					r, c := i>>1, i&1
					ctx := btoi(a.nzV[c]) + btoi(left.nzV[r])
					nz := d.decodeCoeffs(tp, 2, ctx, 0, q.uv, &coeffs[20+i])
					a.nzV[c], left.nzV[r] = nz, nz
					nonzero[20+i] = nz
				}
			} else {
				a.nzY, left.nzY = [4]bool{}, [4]bool{}
				a.nzU, left.nzU = [2]bool{}, [2]bool{}
				a.nzV, left.nzV = [2]bool{}, [2]bool{}
				if ymode != bPred {
					a.nzY2, left.nzY2 = false, false
				}
			}

			d.reconstruct(mbx, mby, ymode, bmodes, uvmode, &coeffs, &nonzero)
			d.mbFilter[mby*d.mbw+mbx] = d.filterFor(seg, ymode == bPred, skip)
		}
	}
	return nil
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// decodeCoeffs reads the tokens of one 4x4 block, dequantizes them into
// out (natural order) and reports whether any coefficient was coded.
func (d *vp8Decoder) decodeCoeffs(br *boolDecoder, typ, ctx, first int, dq [2]int32, out *[16]int32) bool {
	probs := &d.coeffProbs[typ]
	n := first
	p := &probs[bands[n]][ctx]
	if !br.bit(p[0]) {
		return false // end of block right away
	}
	for n < 16 {
		if !br.bit(p[1]) { // zero token
			n++
			if n == 16 {
				return true
			}
			p = &probs[bands[n]][0]
			continue
		}
		var v int32
		next := 2
		if !br.bit(p[2]) {
			v, next = 1, 1
		} else {
			v = readLargeToken(br, p)
		}
		if br.bit(128) {
			v = -v
		}
		qf := dq[1]
		if n == 0 {
			qf = dq[0]
		}
		out[zigzag[n]] = v * qf
		n++
		if n == 16 {
			return true
		}
		p = &probs[bands[n]][next]
		if !br.bit(p[0]) {
			return true // end of block
		}
	}
	return true
}

func readLargeToken(br *boolDecoder, p *[11]uint8) int32 {
	if !br.bit(p[3]) {
		if !br.bit(p[4]) {
			return 2
		}
		return 3 + int32(btoi(br.bit(p[5])))
	}
	if !br.bit(p[6]) {
		if !br.bit(p[7]) {
			return 5 + int32(btoi(br.bit(159)))
		}
		return 7 + 2*int32(btoi(br.bit(165))) + int32(btoi(br.bit(145)))
	}
	b1 := btoi(br.bit(p[8]))
	b0 := btoi(br.bit(p[9+b1]))
	cat := 2*b1 + b0
	v := int32(0)
	for _, pr := range pcat[cat] {
		v = v*2 + int32(btoi(br.bit(pr)))
	}
	return v + pcatBase[cat]
}

// ---------------------------------------------------------------------
// Transforms

func inverseWHT(in, out *[16]int32) {
	var t [16]int32
	for i := 0; i < 4; i++ {
		a1 := in[i] + in[12+i]
		b1 := in[4+i] + in[8+i]
		c1 := in[4+i] - in[8+i]
		d1 := in[i] - in[12+i]
		t[i] = a1 + b1
		t[4+i] = c1 + d1
		t[8+i] = a1 - b1
		t[12+i] = d1 - c1
	}
	for i := 0; i < 4; i++ {
		a1 := t[4*i] + t[4*i+3]
		b1 := t[4*i+1] + t[4*i+2]
		c1 := t[4*i+1] - t[4*i+2]
		d1 := t[4*i] - t[4*i+3]
		out[4*i] = (a1 + b1 + 3) >> 3
		out[4*i+1] = (c1 + d1 + 3) >> 3
		out[4*i+2] = (a1 - b1 + 3) >> 3
		out[4*i+3] = (d1 - c1 + 3) >> 3
	}
}

const (
	cospi8sqrt2minus1 = 20091
	sinpi8sqrt2       = 35468
)

// addIDCT adds the inverse DCT of in to the 4x4 block at dst.
func addIDCT(in *[16]int32, dst []byte, stride int) {
	var t [16]int32
	for i := 0; i < 4; i++ {
		a1 := in[i] + in[8+i]
		b1 := in[i] - in[8+i]
		c1 := (in[4+i]*sinpi8sqrt2)>>16 - (in[12+i] + (in[12+i]*cospi8sqrt2minus1)>>16)
		d1 := (in[4+i] + (in[4+i]*cospi8sqrt2minus1)>>16) + (in[12+i]*sinpi8sqrt2)>>16
		t[i] = a1 + d1
		t[12+i] = a1 - d1
		t[4+i] = b1 + c1
		t[8+i] = b1 - c1
	}
	for i := 0; i < 4; i++ {
		r := t[4*i : 4*i+4]
		a1 := r[0] + r[2]
		b1 := r[0] - r[2]
		c1 := (r[1]*sinpi8sqrt2)>>16 - (r[3] + (r[3]*cospi8sqrt2minus1)>>16)
		d1 := (r[1] + (r[1]*cospi8sqrt2minus1)>>16) + (r[3]*sinpi8sqrt2)>>16
		row := dst[i*stride:]
		row[0] = clip8(int32(row[0]) + (a1+d1+4)>>3)
		row[3] = clip8(int32(row[3]) + (a1-d1+4)>>3)
		row[1] = clip8(int32(row[1]) + (b1+c1+4)>>3)
		row[2] = clip8(int32(row[2]) + (b1-c1+4)>>3)
	}
}

func clip8(v int32) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

// ---------------------------------------------------------------------
// Prediction and reconstruction

// edges collects the prediction context of a block: the row above
// (including 4 above-right pixels for luma), the left column and the
// top-left pixel, following the VP8 rules for the frame border (127 above,
// 129 on the left).
type edges struct {
	top     [20]byte
	left    [16]byte
	topLeft byte
}

func (d *vp8Decoder) mbEdges(plane []byte, stride, size, mbx, mby int, luma bool) edges {
	var e edges
	x0, y0 := mbx*size, mby*size
	n := size
	if luma {
		n = 20
	}
	if mby == 0 {
		for i := range e.top {
			e.top[i] = 127
		}
		e.topLeft = 127
	} else {
		row := plane[(y0-1)*stride:]
		for i := 0; i < size; i++ {
			e.top[i] = row[x0+i]
		}
		if luma {
			for i := 16; i < 20; i++ {
				if mbx < d.mbw-1 {
					e.top[i] = row[x0+i]
				} else {
					e.top[i] = row[x0+15]
				}
			}
		}
		if mbx == 0 {
			e.topLeft = 129
		} else {
			e.topLeft = row[x0-1]
		}
	}
	_ = n
	for i := 0; i < size; i++ {
		if mbx == 0 {
			e.left[i] = 129
		} else {
			e.left[i] = plane[(y0+i)*stride+x0-1]
		}
	}
	return e
}

// predictBlock fills a size x size block with a whole-block mode.
func predictBlock(dst []byte, stride, size, mode, mbx, mby int, e *edges) {
	switch mode {
	case dcPred:
		sum, shift := 0, 0
		if mby > 0 {
			for i := 0; i < size; i++ {
				sum += int(e.top[i])
			}
			shift++
		}
		if mbx > 0 {
			for i := 0; i < size; i++ {
				sum += int(e.left[i])
			}
			shift++
		}
		dc := byte(128)
		if shift > 0 {
			bits := 3
			if size == 16 {
				bits = 4
			}
			total := bits + shift - 1 // log2(size * count)
			dc = byte((sum + 1<<(total-1)) >> total)
		}
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				dst[y*stride+x] = dc
			}
		}
	case vPred:
		for y := 0; y < size; y++ {
			copy(dst[y*stride:y*stride+size], e.top[:size])
		}
	case hPred:
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				dst[y*stride+x] = e.left[y]
			}
		}
	case tmPred:
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				dst[y*stride+x] = clip8(int32(e.left[y]) + int32(e.top[x]) - int32(e.topLeft))
			}
		}
	}
}

func avg2(a, b byte) byte    { return byte((int(a) + int(b) + 1) >> 1) }
func avg3(a, b, c byte) byte { return byte((int(a) + 2*int(b) + int(c) + 2) >> 2) }

// predict4 fills a 4x4 subblock. t holds the 8 pixels above (A..H), l the 4
// on the left (I..L) and x the top-left pixel.
func predict4(dst []byte, stride int, mode uint8, t [8]byte, l [4]byte, x byte) {
	set := func(c, r int, v byte) { dst[r*stride+c] = v }
	A, B, C, D, E, F, G, H := t[0], t[1], t[2], t[3], t[4], t[5], t[6], t[7]
	I, J, K, L := l[0], l[1], l[2], l[3]
	switch mode {
	case bDC:
		s := 4
		for i := 0; i < 4; i++ {
			s += int(t[i]) + int(l[i])
		}
		v := byte(s >> 3)
		for r := 0; r < 4; r++ {
			for c := 0; c < 4; c++ {
				set(c, r, v)
			}
		}
	case bTM:
		for r := 0; r < 4; r++ {
			for c := 0; c < 4; c++ {
				set(c, r, clip8(int32(l[r])+int32(t[c])-int32(x)))
			}
		}
	case bVE:
		vals := [4]byte{avg3(x, A, B), avg3(A, B, C), avg3(B, C, D), avg3(C, D, E)}
		for r := 0; r < 4; r++ {
			for c := 0; c < 4; c++ {
				set(c, r, vals[c])
			}
		}
	case bHE:
		vals := [4]byte{avg3(x, I, J), avg3(I, J, K), avg3(J, K, L), avg3(K, L, L)}
		for r := 0; r < 4; r++ {
			for c := 0; c < 4; c++ {
				set(c, r, vals[r])
			}
		}
	case bLD:
		set(0, 0, avg3(A, B, C))
		v := avg3(B, C, D)
		set(1, 0, v)
		set(0, 1, v)
		v = avg3(C, D, E)
		set(2, 0, v)
		set(1, 1, v)
		set(0, 2, v)
		v = avg3(D, E, F)
		set(3, 0, v)
		set(2, 1, v)
		set(1, 2, v)
		set(0, 3, v)
		v = avg3(E, F, G)
		set(3, 1, v)
		set(2, 2, v)
		set(1, 3, v)
		v = avg3(F, G, H)
		set(3, 2, v)
		set(2, 3, v)
		set(3, 3, avg3(G, H, H))
	case bRD:
		set(0, 3, avg3(J, K, L))
		v := avg3(I, J, K)
		set(1, 3, v)
		set(0, 2, v)
		v = avg3(x, I, J)
		set(2, 3, v)
		set(1, 2, v)
		set(0, 1, v)
		v = avg3(A, x, I)
		set(3, 3, v)
		set(2, 2, v)
		set(1, 1, v)
		set(0, 0, v)
		v = avg3(B, A, x)
		set(3, 2, v)
		set(2, 1, v)
		set(1, 0, v)
		v = avg3(C, B, A)
		set(3, 1, v)
		set(2, 0, v)
		set(3, 0, avg3(D, C, B))
	case bVR:
		v := avg2(x, A)
		set(0, 0, v)
		set(1, 2, v)
		v = avg2(A, B)
		set(1, 0, v)
		set(2, 2, v)
		v = avg2(B, C)
		set(2, 0, v)
		set(3, 2, v)
		set(3, 0, avg2(C, D))
		set(0, 3, avg3(K, J, I))
		set(0, 2, avg3(J, I, x))
		v = avg3(I, x, A)
		set(0, 1, v)
		set(1, 3, v)
		v = avg3(x, A, B)
		set(1, 1, v)
		set(2, 3, v)
		v = avg3(A, B, C)
		set(2, 1, v)
		set(3, 3, v)
		set(3, 1, avg3(B, C, D))
	case bVL:
		set(0, 0, avg2(A, B))
		v := avg2(B, C)
		set(1, 0, v)
		set(0, 2, v)
		v = avg2(C, D)
		set(2, 0, v)
		set(1, 2, v)
		v = avg2(D, E)
		set(3, 0, v)
		set(2, 2, v)
		set(0, 1, avg3(A, B, C))
		v = avg3(B, C, D)
		set(1, 1, v)
		set(0, 3, v)
		v = avg3(C, D, E)
		set(2, 1, v)
		set(1, 3, v)
		v = avg3(D, E, F)
		set(3, 1, v)
		set(2, 3, v)
		set(3, 2, avg3(E, F, G))
		set(3, 3, avg3(F, G, H))
	case bHD:
		v := avg2(I, x)
		set(0, 0, v)
		set(2, 1, v)
		v = avg2(J, I)
		set(0, 1, v)
		set(2, 2, v)
		v = avg2(K, J)
		set(0, 2, v)
		set(2, 3, v)
		set(0, 3, avg2(L, K))
		set(3, 0, avg3(A, B, C))
		set(2, 0, avg3(x, A, B))
		v = avg3(I, x, A)
		set(1, 0, v)
		set(3, 1, v)
		v = avg3(J, I, x)
		set(1, 1, v)
		set(3, 2, v)
		v = avg3(K, J, I)
		set(1, 2, v)
		set(3, 3, v)
		set(1, 3, avg3(L, K, J))
	case bHU:
		set(0, 0, avg2(I, J))
		v := avg2(J, K)
		set(2, 0, v)
		set(0, 1, v)
		v = avg2(K, L)
		set(2, 1, v)
		set(0, 2, v)
		set(1, 0, avg3(I, J, K))
		v = avg3(J, K, L)
		set(3, 0, v)
		set(1, 1, v)
		v = avg3(K, L, L)
		set(3, 1, v)
		set(1, 2, v)
		for _, p := range [][2]int{{3, 2}, {2, 2}, {0, 3}, {1, 3}, {2, 3}, {3, 3}} {
			set(p[0], p[1], L)
		}
	}
}

func (d *vp8Decoder) reconstruct(mbx, mby, ymode int, bmodes [16]uint8, uvmode int, coeffs *[25][16]int32, nonzero *[25]bool) {
	ys := d.yStride
	x0, y0 := mbx*16, mby*16
	e := d.mbEdges(d.y, ys, 16, mbx, mby, true)
	if ymode == bPred {
		for i := 0; i < 16; i++ {
			r, c := i>>2, i&3
			px, py := x0+4*c, y0+4*r
			var t [8]byte
			var l [4]byte
			var tl byte
			if r == 0 {
				copy(t[:], e.top[4*c:4*c+8])
				if c == 0 {
					tl = e.topLeft
				} else {
					tl = e.top[4*c-1]
				}
			} else {
				row := d.y[(py-1)*ys:]
				copy(t[:4], row[px:px+4])
				if c < 3 {
					copy(t[4:], row[px+4:px+8])
				} else {
					copy(t[4:], e.top[16:20])
				}
				if c == 0 {
					tl = e.left[4*r-1]
				} else {
					tl = row[px-1]
				}
			}
			for k := 0; k < 4; k++ {
				if c == 0 {
					l[k] = e.left[4*r+k]
				} else {
					l[k] = d.y[(py+k)*ys+px-1]
				}
			}
			dst := d.y[py*ys+px:]
			predict4(dst, ys, bmodes[i], t, l, tl)
			if nonzero[i] {
				addIDCT(&coeffs[i], dst, ys)
			}
		}
	} else {
		predictBlock(d.y[y0*ys+x0:], ys, 16, ymode, mbx, mby, &e)
		for i := 0; i < 16; i++ {
			if nonzero[i] {
				r, c := i>>2, i&3
				addIDCT(&coeffs[i], d.y[(y0+4*r)*ys+x0+4*c:], ys)
			}
		}
	}
	cs := d.cStride
	cx, cy := mbx*8, mby*8
	for p, plane := range [][]byte{d.u, d.v} {
		ce := d.mbEdges(plane, cs, 8, mbx, mby, false)
		predictBlock(plane[cy*cs+cx:], cs, 8, uvmode, mbx, mby, &ce)
		for i := 0; i < 4; i++ {
			k := 16 + 4*p + i
			if nonzero[k] {
				r, c := i>>1, i&1
				addIDCT(&coeffs[k], plane[(cy+4*r)*cs+cx+4*c:], cs)
			}
		}
	}
}

// ---------------------------------------------------------------------
// Loop filter (RFC 6386 section 15)

func abs32(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sclamp(v, lo, hi int) int { return max(lo, min(hi, v)) }

func u8(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

func needsFilter(p []byte, i, step, t int) bool {
	p1, p0, q0, q1 := int(p[i-2*step]), int(p[i-step]), int(p[i]), int(p[i+step])
	return 4*abs32(p0-q0)+abs32(p1-q1) <= t
}

func needsFilter2(p []byte, i, step, t, it int) bool {
	p3, p2, p1, p0 := int(p[i-4*step]), int(p[i-3*step]), int(p[i-2*step]), int(p[i-step])
	q0, q1, q2, q3 := int(p[i]), int(p[i+step]), int(p[i+2*step]), int(p[i+3*step])
	if 4*abs32(p0-q0)+abs32(p1-q1) > t {
		return false
	}
	return abs32(p3-p2) <= it && abs32(p2-p1) <= it && abs32(p1-p0) <= it &&
		abs32(q3-q2) <= it && abs32(q2-q1) <= it && abs32(q1-q0) <= it
}

func hev(p []byte, i, step, thresh int) bool {
	p1, p0, q0, q1 := int(p[i-2*step]), int(p[i-step]), int(p[i]), int(p[i+step])
	return abs32(p1-p0) > thresh || abs32(q1-q0) > thresh
}

func doFilter2(p []byte, i, step int) {
	p1, p0, q0, q1 := int(p[i-2*step]), int(p[i-step]), int(p[i]), int(p[i+step])
	a := 3*(q0-p0) + sclamp(p1-q1, -128, 127)
	a1 := sclamp((a+4)>>3, -16, 15)
	a2 := sclamp((a+3)>>3, -16, 15)
	p[i-step] = u8(p0 + a2)
	p[i] = u8(q0 - a1)
}

func doFilter4(p []byte, i, step int) {
	p1, p0, q0, q1 := int(p[i-2*step]), int(p[i-step]), int(p[i]), int(p[i+step])
	a := 3 * (q0 - p0)
	a1 := sclamp((a+4)>>3, -16, 15)
	a2 := sclamp((a+3)>>3, -16, 15)
	a3 := (a1 + 1) >> 1
	p[i-2*step] = u8(p1 + a3)
	p[i-step] = u8(p0 + a2)
	p[i] = u8(q0 - a1)
	p[i+step] = u8(q1 - a3)
}

func doFilter6(p []byte, i, step int) {
	p2, p1, p0 := int(p[i-3*step]), int(p[i-2*step]), int(p[i-step])
	q0, q1, q2 := int(p[i]), int(p[i+step]), int(p[i+2*step])
	a := sclamp(3*(q0-p0)+sclamp(p1-q1, -128, 127), -128, 127)
	a1 := (27*a + 63) >> 7
	a2 := (18*a + 63) >> 7
	a3 := (9*a + 63) >> 7
	p[i-3*step] = u8(p2 + a3)
	p[i-2*step] = u8(p1 + a2)
	p[i-step] = u8(p0 + a1)
	p[i] = u8(q0 - a1)
	p[i+step] = u8(q1 - a2)
	p[i+2*step] = u8(q2 - a3)
}

// filterLoop filters `size` positions along an edge. hstep crosses the
// edge, vstep moves along it.
func filterLoop(p []byte, i, hstep, vstep, size, thresh, ithresh, hevT int, mbEdge bool) {
	t2 := 2*thresh + 1
	for k := 0; k < size; k++ {
		if needsFilter2(p, i, hstep, t2, ithresh) {
			switch {
			case hev(p, i, hstep, hevT):
				doFilter2(p, i, hstep)
			case mbEdge:
				doFilter6(p, i, hstep)
			default:
				doFilter4(p, i, hstep)
			}
		}
		i += vstep
	}
}

func simpleLoop(p []byte, i, hstep, vstep, size, thresh int) {
	t2 := 2*thresh + 1
	for k := 0; k < size; k++ {
		if needsFilter(p, i, hstep, t2) {
			doFilter2(p, i, hstep)
		}
		i += vstep
	}
}

func (d *vp8Decoder) loopFilter() {
	ys, cs := d.yStride, d.cStride
	for mby := 0; mby < d.mbh; mby++ {
		for mbx := 0; mbx < d.mbw; mbx++ {
			f := d.mbFilter[mby*d.mbw+mbx]
			if f.limit == 0 {
				continue
			}
			yi := mby*16*ys + mbx*16
			if d.filterType == 1 { // simple filter, luma only
				if mbx > 0 {
					simpleLoop(d.y, yi, 1, ys, 16, f.limit+4)
				}
				if f.inner {
					for k := 4; k < 16; k += 4 {
						simpleLoop(d.y, yi+k, 1, ys, 16, f.limit)
					}
				}
				if mby > 0 {
					simpleLoop(d.y, yi, ys, 1, 16, f.limit+4)
				}
				if f.inner {
					for k := 4; k < 16; k += 4 {
						simpleLoop(d.y, yi+k*ys, ys, 1, 16, f.limit)
					}
				}
				continue
			}
			ci := mby*8*cs + mbx*8
			il, ht := f.ilevel, f.hevThresh
			if mbx > 0 {
				filterLoop(d.y, yi, 1, ys, 16, f.limit+4, il, ht, true)
				filterLoop(d.u, ci, 1, cs, 8, f.limit+4, il, ht, true)
				filterLoop(d.v, ci, 1, cs, 8, f.limit+4, il, ht, true)
			}
			if f.inner {
				for k := 4; k < 16; k += 4 {
					filterLoop(d.y, yi+k, 1, ys, 16, f.limit, il, ht, false)
				}
				filterLoop(d.u, ci+4, 1, cs, 8, f.limit, il, ht, false)
				filterLoop(d.v, ci+4, 1, cs, 8, f.limit, il, ht, false)
			}
			if mby > 0 {
				filterLoop(d.y, yi, ys, 1, 16, f.limit+4, il, ht, true)
				filterLoop(d.u, ci, cs, 1, 8, f.limit+4, il, ht, true)
				filterLoop(d.v, ci, cs, 1, 8, f.limit+4, il, ht, true)
			}
			if f.inner {
				for k := 4; k < 16; k += 4 {
					filterLoop(d.y, yi+k*ys, ys, 1, 16, f.limit, il, ht, false)
				}
				filterLoop(d.u, ci+4*cs, cs, 1, 8, f.limit, il, ht, false)
				filterLoop(d.v, ci+4*cs, cs, 1, 8, f.limit, il, ht, false)
			}
		}
	}
}

// ---------------------------------------------------------------------
// Color conversion (libwebp's fixed-point BT.601) with "fancy" bilinear
// chroma upsampling.

func yuvToRGB(y, u, v int) (byte, byte, byte) {
	mul := func(a, c int) int { return (a * c) >> 8 }
	clip := func(x int) byte {
		x >>= 6
		if x < 0 {
			return 0
		}
		if x > 255 {
			return 255
		}
		return byte(x)
	}
	r := clip(mul(y, 19077) + mul(v, 26149) - 14234)
	g := clip(mul(y, 19077) - mul(u, 6419) - mul(v, 13320) + 8708)
	b := clip(mul(y, 19077) + mul(u, 33050) - 17685)
	return r, g, b
}

func (d *vp8Decoder) toRGB() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, d.w, d.h))
	ch := (d.h + 1) / 2
	cs := d.cStride
	var nu, fu, nv, fv []byte
	for y := 0; y < d.h; y++ {
		// libwebp pairs each output row with the nearest chroma row (3/4
		// weight) and the next nearest one (1/4 weight).
		var near, far int
		switch {
		case y == 0:
		case y&1 == 1:
			near, far = y/2, min(y/2+1, ch-1)
		default:
			near, far = y/2, y/2-1
		}
		nu, fu = d.u[near*cs:], d.u[far*cs:]
		nv, fv = d.v[near*cs:], d.v[far*cs:]
		row := img.Pix[y*img.Stride:]
		yrow := d.y[y*d.yStride:]
		put := func(x, u, v int) {
			r, g, b := yuvToRGB(int(yrow[x]), u, v)
			row[4*x], row[4*x+1], row[4*x+2], row[4*x+3] = r, g, b, 255
		}
		edge := func(n, f []byte, i int) int { return (3*int(n[i]) + int(f[i]) + 2) >> 2 }
		put(0, edge(nu, fu, 0), edge(nv, fv, 0))
		up := func(n, f []byte, x int) (int, int) {
			avg := int(n[x-1]) + int(n[x]) + int(f[x-1]) + int(f[x]) + 8
			d12 := (avg + 2*(int(n[x])+int(f[x-1]))) >> 3
			d03 := (avg + 2*(int(n[x-1])+int(f[x]))) >> 3
			return (d12 + int(n[x-1])) >> 1, (d03 + int(n[x])) >> 1
		}
		last := (d.w - 1) >> 1
		for x := 1; x <= last; x++ {
			u0, u1 := up(nu, fu, x)
			v0, v1 := up(nv, fv, x)
			put(2*x-1, u0, v0)
			put(2*x, u1, v1)
		}
		if d.w&1 == 0 {
			put(d.w-1, edge(nu, fu, last), edge(nv, fv, last))
		}
	}
	return img
}
