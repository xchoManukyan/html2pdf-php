package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // register decoders
	"image/jpeg"
	_ "image/png"

	_ "github.com/xchoManukyan/html2pdf/converter/internal/webp" // WebP (lossy, lossless, alpha)
)

// Image is a raster image embedded in the document.
type Image struct {
	id      int
	objID   int
	smaskID int
	// Pixel size of the image.
	Width, Height int

	colorSpace string
	bpc        int
	filter     string
	data       []byte
	decode     string
	smask      []byte // compressed 8-bit alpha channel, nil when opaque
}

// AddImage embeds a JPEG, PNG or GIF image. JPEG data is stored as-is;
// other formats are decoded and stored as compressed RGB (+ alpha mask).
func (d *Document) AddImage(data []byte) (*Image, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("unsupported image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > 40_000_000 {
		return nil, errors.New("image dimensions out of range")
	}
	img := &Image{id: len(d.images) + 1, Width: cfg.Width, Height: cfg.Height, bpc: 8}

	if format == "jpeg" {
		img.filter = "/DCTDecode"
		img.data = data
		switch cfg.ColorModel {
		case color.GrayModel:
			img.colorSpace = "/DeviceGray"
		case color.CMYKModel:
			img.colorSpace = "/DeviceCMYK"
			img.decode = " /Decode [1 0 1 0 1 0 1 0]" // Adobe CMYK JPEGs are inverted
		default:
			img.colorSpace = "/DeviceRGB"
		}
		// Make sure the data really decodes before embedding it.
		if _, err := jpeg.DecodeConfig(bytes.NewReader(data)); err != nil {
			return nil, err
		}
	} else {
		src, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		b := src.Bounds()
		rgb := make([]byte, 0, b.Dx()*b.Dy()*3)
		alpha := make([]byte, 0, b.Dx()*b.Dy())
		hasAlpha := false
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
				rgb = append(rgb, c.R, c.G, c.B)
				alpha = append(alpha, c.A)
				if c.A != 255 {
					hasAlpha = true
				}
			}
		}
		img.colorSpace = "/DeviceRGB"
		img.filter = "/FlateDecode"
		img.data = deflate(rgb)
		if hasAlpha {
			img.smask = deflate(alpha)
		}
	}
	d.images = append(d.images, img)
	return img, nil
}

func (img *Image) write(w *writer) {
	dict := fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent %d /Filter %s%s",
		img.Width, img.Height, img.colorSpace, img.bpc, img.filter, img.decode)
	if img.smask != nil {
		dict += fmt.Sprintf(" /SMask %d 0 R", img.smaskID)
	}
	w.stream(img.objID, dict, img.data)
	if img.smask != nil {
		w.stream(img.smaskID, fmt.Sprintf(
			"/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /FlateDecode",
			img.Width, img.Height), img.smask)
	}
}
