// Package webp decodes WebP images with the Go standard library only:
// lossy (VP8), lossless (VP8L), the extended format with an alpha channel
// (VP8X + ALPH), and the first frame of animations. Importing the package
// registers the format with image.Decode.
package webp

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"io"
)

func init() {
	image.RegisterFormat("webp", "RIFF????WEBP", Decode, DecodeConfig)
}

// maxPixels bounds the memory a forged header can make the decoder allocate.
const maxPixels = 40_000_000

var (
	errFormat      = errors.New("webp: invalid format")
	errUnsupported = errors.New("webp: unsupported feature")
)

type chunk struct {
	id   string
	data []byte
}

func readChunks(data []byte) ([]chunk, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return nil, errFormat
	}
	size := int(binary.LittleEndian.Uint32(data[4:]))
	if size+8 < len(data) {
		data = data[:size+8]
	}
	var out []chunk
	for p := 12; p+8 <= len(data); {
		id := string(data[p : p+4])
		n := int(binary.LittleEndian.Uint32(data[p+4:]))
		if n < 0 || p+8+n > len(data) {
			return nil, errFormat
		}
		out = append(out, chunk{id, data[p+8 : p+8+n]})
		p += 8 + n + n&1
	}
	return out, nil
}

// frame locates the image data: the VP8/VP8L chunk and an optional ALPH.
func frame(chunks []chunk) (vp8, vp8l, alph []byte, err error) {
	for _, c := range chunks {
		switch c.id {
		case "VP8 ":
			return c.data, nil, alph, nil
		case "VP8L":
			return nil, c.data, nil, nil
		case "ALPH":
			alph = c.data
		case "ANMF":
			// first animation frame: 16-byte frame header, then chunks
			if len(c.data) < 16 {
				return nil, nil, nil, errFormat
			}
			sub, err := readChunks(append([]byte("RIFF\x00\x00\x00\x00WEBP"), c.data[16:]...))
			if err != nil {
				return nil, nil, nil, err
			}
			return frame(sub)
		}
	}
	return nil, nil, nil, errFormat
}

// Decode reads a WebP image.
func Decode(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(io.LimitReader(r, 256<<20))
	if err != nil {
		return nil, err
	}
	chunks, err := readChunks(data)
	if err != nil {
		return nil, err
	}
	vp8, vp8l, alph, err := frame(chunks)
	if err != nil {
		return nil, err
	}
	if vp8l != nil {
		return decodeVP8L(vp8l)
	}
	img, err := decodeVP8(vp8)
	if err != nil {
		return nil, err
	}
	if alph == nil {
		return img, nil
	}
	return applyAlpha(img, alph)
}

// DecodeConfig returns the dimensions without decoding pixels.
func DecodeConfig(r io.Reader) (image.Config, error) {
	data, err := io.ReadAll(io.LimitReader(r, 256<<20))
	if err != nil {
		return image.Config{}, err
	}
	chunks, err := readChunks(data)
	if err != nil {
		return image.Config{}, err
	}
	for _, c := range chunks {
		if c.id == "VP8X" && len(c.data) >= 10 {
			w := int(c.data[4]) | int(c.data[5])<<8 | int(c.data[6])<<16
			h := int(c.data[7]) | int(c.data[8])<<8 | int(c.data[9])<<16
			return image.Config{ColorModel: color.NRGBAModel, Width: w + 1, Height: h + 1}, nil
		}
	}
	vp8, vp8l, _, err := frame(chunks)
	if err != nil {
		return image.Config{}, err
	}
	if vp8l != nil {
		if len(vp8l) < 5 || vp8l[0] != 0x2f {
			return image.Config{}, errFormat
		}
		bits := binary.LittleEndian.Uint32(vp8l[1:])
		return image.Config{ColorModel: color.NRGBAModel, Width: int(bits&0x3fff) + 1, Height: int(bits>>14&0x3fff) + 1}, nil
	}
	if len(vp8) < 10 {
		return image.Config{}, errFormat
	}
	w := int(binary.LittleEndian.Uint16(vp8[6:]) & 0x3fff)
	h := int(binary.LittleEndian.Uint16(vp8[8:]) & 0x3fff)
	return image.Config{ColorModel: color.YCbCrModel, Width: w, Height: h}, nil
}

// applyAlpha decodes an ALPH chunk and combines it with the color image.
func applyAlpha(img image.Image, alph []byte) (image.Image, error) {
	if len(alph) < 1 {
		return img, nil
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	header := alph[0]
	compression := header & 3
	filter := header >> 2 & 3
	var a []byte
	switch compression {
	case 0:
		a = alph[1:]
		if len(a) < w*h {
			return nil, errFormat
		}
	case 1:
		d := &vp8lDecoder{br: newBitReader(alph[1:])}
		px, err := d.decodeImageStream(w, h, true)
		if err != nil {
			return nil, err
		}
		a = make([]byte, w*h)
		for i, p := range px {
			a[i] = byte(p >> 8) // green channel
		}
	default:
		return nil, errUnsupported
	}
	unfilterAlpha(a, w, h, filter)
	out := image.NewNRGBA(b)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := out.PixOffset(b.Min.X+x, b.Min.Y+y)
			out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = byte(r>>8), byte(g>>8), byte(bl>>8), a[y*w+x]
		}
	}
	return out, nil
}

// unfilterAlpha reverses the ALPH prediction filters.
func unfilterAlpha(a []byte, w, h int, method byte) {
	if method == 0 {
		return
	}
	at := func(x, y int) int { return int(a[y*w+x]) }
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var pred int
			switch {
			case x == 0 && y == 0:
				pred = 0
			case y == 0:
				pred = at(x-1, 0)
			case x == 0:
				pred = at(0, y-1)
			default:
				switch method {
				case 1:
					pred = at(x-1, y)
				case 2:
					pred = at(x, y-1)
				case 3:
					pred = at(x-1, y) + at(x, y-1) - at(x-1, y-1)
					pred = max(0, min(255, pred))
				}
			}
			a[y*w+x] = byte(int(a[y*w+x]) + pred)
		}
	}
}
