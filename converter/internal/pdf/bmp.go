package pdf

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"io"
)

// A small BMP decoder (the standard library has none): 1/4/8-bit
// palettes, 16-bit (5-5-5 / 5-6-5), 24-bit and 32-bit, bottom-up or
// top-down, uncompressed or BI_BITFIELDS.

func init() {
	image.RegisterFormat("bmp", "BM", decodeBMP, decodeBMPConfig)
}

type bmpHeader struct {
	offset        uint32
	width, height int
	topDown       bool
	bpp           int
	compression   uint32
	palette       []color.NRGBA
	rMask, gMask  uint32
	bMask, aMask  uint32
}

var errBMP = errors.New("bmp: unsupported or invalid file")

func readBMPHeader(r io.Reader) (*bmpHeader, []byte, error) {
	head := make([]byte, 14+124)
	if _, err := io.ReadFull(r, head[:18]); err != nil {
		return nil, nil, errBMP
	}
	if string(head[:2]) != "BM" {
		return nil, nil, errBMP
	}
	infoSize := int(binary.LittleEndian.Uint32(head[14:]))
	if infoSize < 12 || infoSize > 124 {
		return nil, nil, errBMP
	}
	if _, err := io.ReadFull(r, head[18:14+infoSize]); err != nil {
		return nil, nil, errBMP
	}
	h := &bmpHeader{offset: binary.LittleEndian.Uint32(head[10:])}
	info := head[14 : 14+infoSize]
	if infoSize == 12 { // OS/2 BITMAPCOREHEADER
		h.width = int(binary.LittleEndian.Uint16(info[4:]))
		h.height = int(int16(binary.LittleEndian.Uint16(info[6:])))
		h.bpp = int(binary.LittleEndian.Uint16(info[10:]))
	} else {
		h.width = int(int32(binary.LittleEndian.Uint32(info[4:])))
		h.height = int(int32(binary.LittleEndian.Uint32(info[8:])))
		h.bpp = int(binary.LittleEndian.Uint16(info[14:]))
		h.compression = binary.LittleEndian.Uint32(info[16:])
	}
	if h.height < 0 {
		h.height, h.topDown = -h.height, true
	}
	if h.width <= 0 || h.height <= 0 || h.width*h.height > 40_000_000 {
		return nil, nil, errBMP
	}
	switch h.compression {
	case 0: // BI_RGB
		switch h.bpp {
		case 16:
			h.rMask, h.gMask, h.bMask = 0x7C00, 0x03E0, 0x001F
		case 32:
			h.rMask, h.gMask, h.bMask = 0x00FF0000, 0x0000FF00, 0x000000FF
		}
	case 3, 6: // BI_BITFIELDS / BI_ALPHABITFIELDS
		if infoSize >= 52 {
			h.rMask = binary.LittleEndian.Uint32(info[40:])
			h.gMask = binary.LittleEndian.Uint32(info[44:])
			h.bMask = binary.LittleEndian.Uint32(info[48:])
			if infoSize >= 56 {
				h.aMask = binary.LittleEndian.Uint32(info[52:])
			}
		}
	default:
		return nil, nil, errBMP // RLE is not supported
	}
	// read the rest up to the pixel data (palette, masks after a 40-byte header)
	rest := int(h.offset) - 14 - infoSize
	if rest < 0 || rest > 1<<20 {
		return nil, nil, errBMP
	}
	extra := make([]byte, rest)
	if _, err := io.ReadFull(r, extra); err != nil {
		return nil, nil, errBMP
	}
	if infoSize == 40 && (h.compression == 3 || h.compression == 6) && len(extra) >= 12 {
		h.rMask = binary.LittleEndian.Uint32(extra[0:])
		h.gMask = binary.LittleEndian.Uint32(extra[4:])
		h.bMask = binary.LittleEndian.Uint32(extra[8:])
		if h.compression == 6 && len(extra) >= 16 {
			h.aMask = binary.LittleEndian.Uint32(extra[12:])
		}
	}
	if h.bpp <= 8 {
		entry := 4
		if infoSize == 12 {
			entry = 3
		}
		n := min(len(extra)/entry, 1<<h.bpp)
		for i := 0; i < n; i++ {
			e := extra[i*entry:]
			h.palette = append(h.palette, color.NRGBA{e[2], e[1], e[0], 255})
		}
	}
	return h, extra, nil
}

func decodeBMPConfig(r io.Reader) (image.Config, error) {
	h, _, err := readBMPHeader(r)
	if err != nil {
		return image.Config{}, err
	}
	return image.Config{ColorModel: color.NRGBAModel, Width: h.width, Height: h.height}, nil
}

func maskValue(px, mask uint32) uint8 {
	if mask == 0 {
		return 255
	}
	shift := 0
	for mask&1 == 0 {
		mask >>= 1
		shift++
	}
	v := (px >> shift) & mask
	return uint8(v * 255 / mask)
}

func decodeBMP(r io.Reader) (image.Image, error) {
	h, _, err := readBMPHeader(r)
	if err != nil {
		return nil, err
	}
	switch h.bpp {
	case 1, 4, 8, 16, 24, 32:
	default:
		return nil, errBMP
	}
	stride := ((h.width*h.bpp + 31) / 32) * 4
	img := image.NewNRGBA(image.Rect(0, 0, h.width, h.height))
	row := make([]byte, stride)
	hasAlpha := false
	for y := 0; y < h.height; y++ {
		if _, err := io.ReadFull(r, row); err != nil {
			return nil, errBMP
		}
		dy := h.height - 1 - y
		if h.topDown {
			dy = y
		}
		for x := 0; x < h.width; x++ {
			var c color.NRGBA
			switch h.bpp {
			case 1, 4, 8:
				bit := x * h.bpp
				idx := int(row[bit/8]>>(8-h.bpp-bit%8)) & (1<<h.bpp - 1)
				if idx < len(h.palette) {
					c = h.palette[idx]
				}
			case 16:
				px := uint32(binary.LittleEndian.Uint16(row[x*2:]))
				c = color.NRGBA{maskValue(px, h.rMask), maskValue(px, h.gMask), maskValue(px, h.bMask), 255}
			case 24:
				c = color.NRGBA{row[x*3+2], row[x*3+1], row[x*3], 255}
			case 32:
				px := binary.LittleEndian.Uint32(row[x*4:])
				c = color.NRGBA{maskValue(px, h.rMask), maskValue(px, h.gMask), maskValue(px, h.bMask), 255}
				if h.aMask != 0 {
					c.A = maskValue(px, h.aMask)
					if c.A != 0 {
						hasAlpha = true
					}
				}
			}
			img.SetNRGBA(x, dy, c)
		}
	}
	if h.aMask != 0 && !hasAlpha { // an all-zero alpha channel means "unused"
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
	}
	return img, nil
}
