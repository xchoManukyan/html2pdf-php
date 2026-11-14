package webp

import (
	"errors"
	"image"
)

// VP8L: the WebP lossless bitstream (RFC 9649).

var errVP8L = errors.New("webp: invalid lossless bitstream")

type bitReader struct {
	data  []byte
	pos   int
	val   uint64
	nbits uint
	eof   bool
}

func newBitReader(b []byte) *bitReader { return &bitReader{data: b} }

func (b *bitReader) read(n uint) uint32 {
	if n == 0 {
		return 0
	}
	for b.nbits < n {
		if b.pos < len(b.data) {
			b.val |= uint64(b.data[b.pos]) << b.nbits
			b.pos++
		} else {
			b.eof = true
		}
		b.nbits += 8
	}
	v := uint32(b.val & (1<<n - 1))
	b.val >>= n
	b.nbits -= n
	return v
}

// huffman is a canonical prefix code (deflate-style bit order).
type huffman struct {
	count  [16]uint16
	symbol []uint16
	single int      // >= 0: the only symbol (zero-length code)
	table  []uint32 // fast lookup: symbol<<8 | length, indexed by 8 reversed bits
}

const fastBits = 8

func reverseBits(v uint32, n uint) uint32 {
	r := uint32(0)
	for i := uint(0); i < n; i++ {
		r = r<<1 | v&1
		v >>= 1
	}
	return r
}

func buildHuffman(lengths []uint8) (*huffman, error) {
	h := &huffman{single: -1}
	nonzero, last := 0, 0
	for s, l := range lengths {
		if l > 15 {
			return nil, errVP8L
		}
		h.count[l]++
		if l > 0 {
			nonzero++
			last = s
		}
	}
	if nonzero == 0 {
		h.single = 0
		return h, nil
	}
	if nonzero == 1 {
		h.single = last
		return h, nil
	}
	// check for an over-subscribed code
	left := 1
	for l := 1; l < 16; l++ {
		left <<= 1
		left -= int(h.count[l])
		if left < 0 {
			return nil, errVP8L
		}
	}
	offs := [16]uint16{}
	for l := 1; l < 15; l++ {
		offs[l+1] = offs[l] + h.count[l]
	}
	h.symbol = make([]uint16, nonzero)
	for s, l := range lengths {
		if l > 0 {
			h.symbol[offs[l]] = uint16(s)
			offs[l]++
		}
	}
	// fast table for codes up to 8 bits
	h.table = make([]uint32, 1<<fastBits)
	code, idx := uint32(0), 0
	for l := uint(1); l <= fastBits; l++ {
		for i := 0; i < int(h.count[l]); i++ {
			sym := uint32(h.symbol[idx])
			rev := reverseBits(code, l)
			for fill := rev; fill < 1<<fastBits; fill += 1 << l {
				h.table[fill] = sym<<8 | uint32(l)
			}
			code++
			idx++
		}
		code <<= 1
	}
	return h, nil
}

func (b *bitReader) decode(h *huffman) int {
	if h.single >= 0 {
		return h.single
	}
	// fast path
	for b.nbits < fastBits {
		if b.pos < len(b.data) {
			b.val |= uint64(b.data[b.pos]) << b.nbits
			b.pos++
		} else {
			b.eof = true
		}
		b.nbits += 8
	}
	if e := h.table[b.val&(1<<fastBits-1)]; e != 0 {
		l := uint(e & 0xff)
		b.val >>= l
		b.nbits -= l
		return int(e >> 8)
	}
	code, first, index := 0, 0, 0
	for l := 1; l < 16; l++ {
		code |= int(b.read(1))
		count := int(h.count[l])
		if code-first < count {
			return int(h.symbol[index+code-first])
		}
		index += count
		first += count
		first <<= 1
		code <<= 1
	}
	return 0
}

type vp8lDecoder struct {
	br     *bitReader
	width  int
	height int
}

var codeLengthOrder = [19]int{17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

func (d *vp8lDecoder) readCode(alphabet int) (*huffman, error) {
	br := d.br
	lengths := make([]uint8, alphabet)
	if br.read(1) == 1 { // simple code
		n := br.read(1) + 1
		first := br.read(1)
		s0 := int(br.read(1 + 7*uint(first)))
		if s0 >= alphabet {
			return nil, errVP8L
		}
		if n == 1 {
			return &huffman{single: s0}, nil
		}
		s1 := int(br.read(8))
		if s1 >= alphabet {
			return nil, errVP8L
		}
		lengths[s0], lengths[s1] = 1, 1
		return buildHuffman(lengths)
	}
	var clLengths [19]uint8
	n := int(br.read(4)) + 4
	for i := 0; i < n; i++ {
		clLengths[codeLengthOrder[i]] = uint8(br.read(3))
	}
	clCode, err := buildHuffman(clLengths[:])
	if err != nil {
		return nil, err
	}
	maxSymbol := alphabet
	if br.read(1) == 1 {
		nbits := 2 + 2*uint(br.read(3))
		maxSymbol = 2 + int(br.read(nbits))
		if maxSymbol > alphabet {
			return nil, errVP8L
		}
	}
	prev := uint8(8)
	for s := 0; s < alphabet; {
		if maxSymbol == 0 {
			break
		}
		maxSymbol--
		cl := br.decode(clCode)
		if cl < 16 {
			lengths[s] = uint8(cl)
			s++
			if cl != 0 {
				prev = uint8(cl)
			}
			continue
		}
		var repeat int
		var val uint8
		switch cl {
		case 16:
			repeat, val = 3+int(br.read(2)), prev
		case 17:
			repeat = 3 + int(br.read(3))
		default:
			repeat = 11 + int(br.read(7))
		}
		if s+repeat > alphabet {
			return nil, errVP8L
		}
		for i := 0; i < repeat; i++ {
			lengths[s] = val
			s++
		}
	}
	return buildHuffman(lengths)
}

type group [5]*huffman // green+length+cache, red, blue, alpha, distance

func (d *vp8lDecoder) readGroup(cacheSize int) (group, error) {
	var g group
	sizes := [5]int{256 + 24 + cacheSize, 256, 256, 256, 40}
	for i, n := range sizes {
		h, err := d.readCode(n)
		if err != nil {
			return g, err
		}
		g[i] = h
	}
	return g, nil
}

func divRound(n, bits int) int { return (n + 1<<bits - 1) >> bits }

type transform struct {
	kind  int
	bits  int
	xsize int // width before this transform (for color indexing)
	data  []uint32
}

func decodeVP8L(data []byte) (image.Image, error) {
	if len(data) < 5 || data[0] != 0x2f {
		return nil, errVP8L
	}
	d := &vp8lDecoder{br: newBitReader(data[1:])}
	w := int(d.br.read(14)) + 1
	h := int(d.br.read(14)) + 1
	d.br.read(1) // alpha hint
	if d.br.read(3) != 0 {
		return nil, errVP8L
	}
	if w*h > maxPixels {
		return nil, errVP8L
	}
	px, err := d.decodeImageStream(w, h, true)
	if err != nil {
		return nil, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i, p := range px {
		img.Pix[i*4] = byte(p >> 16)
		img.Pix[i*4+1] = byte(p >> 8)
		img.Pix[i*4+2] = byte(p)
		img.Pix[i*4+3] = byte(p >> 24)
	}
	return img, nil
}

// decodeImageStream decodes an entropy-coded image. The main image
// (level0) carries transforms and meta prefix codes; sub-images do not.
func (d *vp8lDecoder) decodeImageStream(xsize, ysize int, level0 bool) ([]uint32, error) {
	br := d.br
	var transforms []transform
	if level0 {
		seen := 0
		for br.read(1) == 1 {
			t := transform{kind: int(br.read(2)), xsize: xsize}
			if seen&(1<<t.kind) != 0 {
				return nil, errVP8L
			}
			seen |= 1 << t.kind
			switch t.kind {
			case 0, 1: // predictor, cross-color
				t.bits = int(br.read(3)) + 2
				sub, err := d.decodeImageStream(divRound(xsize, t.bits), divRound(ysize, t.bits), false)
				if err != nil {
					return nil, err
				}
				t.data = sub
			case 2: // subtract green
			case 3: // color indexing
				n := int(br.read(8)) + 1
				table, err := d.decodeImageStream(n, 1, false)
				if err != nil {
					return nil, err
				}
				for i := 1; i < n; i++ { // delta coded
					table[i] = addPixels(table[i], table[i-1])
				}
				t.data = table
				switch {
				case n <= 2:
					t.bits = 3
				case n <= 4:
					t.bits = 2
				case n <= 16:
					t.bits = 1
				}
				xsize = divRound(xsize, t.bits)
			}
			transforms = append(transforms, t)
		}
	}
	cacheBits := 0
	if br.read(1) == 1 {
		cacheBits = int(br.read(4))
		if cacheBits < 1 || cacheBits > 11 {
			return nil, errVP8L
		}
	}
	var meta []uint32
	metaBits := 0
	numGroups := 1
	if level0 && br.read(1) == 1 {
		metaBits = int(br.read(3)) + 2
		var err error
		meta, err = d.decodeImageStream(divRound(xsize, metaBits), divRound(ysize, metaBits), false)
		if err != nil {
			return nil, err
		}
		for i, m := range meta {
			code := int(m >> 8 & 0xffff)
			meta[i] = uint32(code)
			numGroups = max(numGroups, code+1)
		}
	}
	cacheSize := 0
	if cacheBits > 0 {
		cacheSize = 1 << cacheBits
	}
	groups := make([]group, numGroups)
	for i := range groups {
		g, err := d.readGroup(cacheSize)
		if err != nil {
			return nil, err
		}
		groups[i] = g
	}
	px, err := d.decodePixels(xsize, ysize, groups, meta, metaBits, cacheBits)
	if err != nil {
		return nil, err
	}
	for i := len(transforms) - 1; i >= 0; i-- {
		px = inverseTransform(transforms[i], px, ysize)
	}
	return px, nil
}

func prefixValue(br *bitReader, code int) int {
	if code < 4 {
		return code + 1
	}
	extra := uint(code-2) >> 1
	offset := (2 + code&1) << extra
	return offset + int(br.read(extra)) + 1
}

var distanceMap = [120][2]int8{
	{0, 1}, {1, 0}, {1, 1}, {-1, 1}, {0, 2}, {2, 0}, {1, 2}, {-1, 2},
	{2, 1}, {-2, 1}, {2, 2}, {-2, 2}, {0, 3}, {3, 0}, {1, 3}, {-1, 3},
	{3, 1}, {-3, 1}, {2, 3}, {-2, 3}, {3, 2}, {-3, 2}, {0, 4}, {4, 0},
	{1, 4}, {-1, 4}, {4, 1}, {-4, 1}, {3, 3}, {-3, 3}, {2, 4}, {-2, 4},
	{4, 2}, {-4, 2}, {0, 5}, {3, 4}, {-3, 4}, {4, 3}, {-4, 3}, {5, 0},
	{1, 5}, {-1, 5}, {5, 1}, {-5, 1}, {2, 5}, {-2, 5}, {5, 2}, {-5, 2},
	{4, 4}, {-4, 4}, {3, 5}, {-3, 5}, {5, 3}, {-5, 3}, {0, 6}, {6, 0},
	{1, 6}, {-1, 6}, {6, 1}, {-6, 1}, {2, 6}, {-2, 6}, {6, 2}, {-6, 2},
	{4, 5}, {-4, 5}, {5, 4}, {-5, 4}, {3, 6}, {-3, 6}, {6, 3}, {-6, 3},
	{0, 7}, {7, 0}, {1, 7}, {-1, 7}, {5, 5}, {-5, 5}, {7, 1}, {-7, 1},
	{4, 6}, {-4, 6}, {6, 4}, {-6, 4}, {2, 7}, {-2, 7}, {7, 2}, {-7, 2},
	{3, 7}, {-3, 7}, {7, 3}, {-7, 3}, {5, 6}, {-5, 6}, {6, 5}, {-6, 5},
	{8, 0}, {4, 7}, {-4, 7}, {7, 4}, {-7, 4}, {8, 1}, {8, 2}, {6, 6},
	{-6, 6}, {8, 3}, {5, 7}, {-5, 7}, {7, 5}, {-7, 5}, {8, 4}, {6, 7},
	{-6, 7}, {7, 6}, {-7, 6}, {8, 5}, {7, 7}, {-7, 7}, {8, 6}, {8, 7},
}

func (d *vp8lDecoder) decodePixels(w, h int, groups []group, meta []uint32, metaBits, cacheBits int) ([]uint32, error) {
	br := d.br
	total := w * h
	px := make([]uint32, total)
	var cache []uint32
	if cacheBits > 0 {
		cache = make([]uint32, 1<<cacheBits)
	}
	shift := uint(32 - cacheBits)
	add := func(p uint32) {
		if cache != nil {
			cache[(0x1e35a7bd*p)>>shift] = p
		}
	}
	metaW := 0
	if meta != nil {
		metaW = divRound(w, metaBits)
	}
	g := &groups[0]
	for pos := 0; pos < total; {
		if br.eof && br.nbits == 0 {
			return nil, errVP8L
		}
		x, y := pos%w, pos/w
		if meta != nil {
			g = &groups[meta[(y>>metaBits)*metaW+(x>>metaBits)]]
		}
		s := br.decode(g[0])
		switch {
		case s < 256:
			r := br.decode(g[1])
			b := br.decode(g[2])
			a := br.decode(g[3])
			p := uint32(a)<<24 | uint32(r)<<16 | uint32(s)<<8 | uint32(b)
			px[pos] = p
			add(p)
			pos++
		case s < 256+24:
			length := prefixValue(br, s-256)
			dcode := prefixValue(br, br.decode(g[4]))
			var dist int
			if dcode > 120 {
				dist = dcode - 120
			} else {
				m := distanceMap[dcode-1]
				dist = int(m[0]) + int(m[1])*w
				if dist < 1 {
					dist = 1
				}
			}
			if dist > pos || pos+length > total {
				return nil, errVP8L
			}
			for i := 0; i < length; i++ {
				p := px[pos-dist]
				px[pos] = p
				add(p)
				pos++
			}
		default:
			idx := s - 280
			if cache == nil || idx >= len(cache) {
				return nil, errVP8L
			}
			p := cache[idx]
			px[pos] = p
			add(p)
			pos++
		}
	}
	return px, nil
}

func addPixels(a, b uint32) uint32 {
	ag := (a & 0xff00ff00) + (b & 0xff00ff00)
	rb := (a & 0x00ff00ff) + (b & 0x00ff00ff)
	return ag&0xff00ff00 | rb&0x00ff00ff
}

func average2(a, b uint32) uint32 {
	return (((a ^ b) & 0xfefefefe) >> 1) + (a & b)
}

func channel(p uint32, shift uint) int { return int(p >> shift & 0xff) }

func clamp255(v int) uint32 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint32(v)
}

func selectPred(l, t, tl uint32) uint32 {
	pl, pt := 0, 0
	for _, s := range []uint{24, 16, 8, 0} {
		p := channel(l, s) + channel(t, s) - channel(tl, s)
		pl += absInt(p - channel(l, s))
		pt += absInt(p - channel(t, s))
	}
	if pl < pt {
		return l
	}
	return t
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func clampAddSubtractFull(a, b, c uint32) uint32 {
	var out uint32
	for _, s := range []uint{24, 16, 8, 0} {
		out |= clamp255(channel(a, s)+channel(b, s)-channel(c, s)) << s
	}
	return out
}

func clampAddSubtractHalf(a, b uint32) uint32 {
	var out uint32
	for _, s := range []uint{24, 16, 8, 0} {
		ca, cb := channel(a, s), channel(b, s)
		out |= clamp255(ca+(ca-cb)/2) << s
	}
	return out
}

func predict(mode uint32, px []uint32, pos, w int) uint32 {
	l, t := px[pos-1], px[pos-w]
	tl, tr := px[pos-w-1], px[pos-w+1]
	switch mode {
	case 0:
		return 0xff000000
	case 1:
		return l
	case 2:
		return t
	case 3:
		return tr
	case 4:
		return tl
	case 5:
		return average2(average2(l, tr), t)
	case 6:
		return average2(l, tl)
	case 7:
		return average2(l, t)
	case 8:
		return average2(tl, t)
	case 9:
		return average2(t, tr)
	case 10:
		return average2(average2(l, tl), average2(t, tr))
	case 11:
		return selectPred(l, t, tl)
	case 12:
		return clampAddSubtractFull(l, t, tl)
	case 13:
		return clampAddSubtractHalf(average2(l, t), tl)
	}
	return 0
}

func inverseTransform(t transform, px []uint32, h int) []uint32 {
	w := t.xsize
	switch t.kind {
	case 0: // predictor
		tw := divRound(w, t.bits)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				pos := y*w + x
				var pred uint32
				switch {
				case x == 0 && y == 0:
					pred = 0xff000000
				case y == 0:
					pred = px[pos-1]
				case x == 0:
					pred = px[pos-w]
				default:
					mode := t.data[(y>>t.bits)*tw+(x>>t.bits)] >> 8 & 0xf
					if x == w-1 && pos-w+1 >= len(px) {
						mode = 1
					}
					pred = predict(mode, px, pos, w)
				}
				px[pos] = addPixels(px[pos], pred)
			}
		}
	case 1: // cross-color
		tw := divRound(w, t.bits)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				m := t.data[(y>>t.bits)*tw+(x>>t.bits)]
				g2r, g2b, r2b := int8(m), int8(m>>8), int8(m>>16)
				p := px[y*w+x]
				green := int8(p >> 8)
				red := int(p>>16&0xff) + int(g2r)*int(green)>>5
				red &= 0xff
				blue := int(p&0xff) + int(g2b)*int(green)>>5
				blue += int(r2b) * int(int8(red)) >> 5
				blue &= 0xff
				px[y*w+x] = p&0xff00ff00 | uint32(red)<<16 | uint32(blue)
			}
		}
	case 2: // subtract green
		for i, p := range px {
			g := p >> 8 & 0xff
			r := (p>>16 + g) & 0xff
			b := (p + g) & 0xff
			px[i] = p&0xff00ff00 | r<<16 | b
		}
	case 3: // color indexing
		packedW := divRound(w, t.bits)
		out := make([]uint32, w*h)
		ppb := 1 << t.bits
		bitsPer := uint(8 >> t.bits)
		mask := uint32(1)<<bitsPer - 1
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				packed := px[y*packedW+x/ppb] >> 8 & 0xff
				idx := packed >> (uint(x%ppb) * bitsPer) & mask
				if int(idx) < len(t.data) {
					out[y*w+x] = t.data[idx]
				}
			}
		}
		return out
	}
	return px
}
