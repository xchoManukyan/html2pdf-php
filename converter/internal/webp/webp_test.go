package webp

import (
	"bytes"
	"image"
	"os"
	"testing"
)

// The test images are 40x30 drawings (a red-to-blue gradient rectangle, a
// translucent yellow circle and a black bar) encoded by Chromium:
// lossless.webp (VP8L, transparent border), lossy.webp (VP8 on white) and
// alpha.webp (VP8 + ALPH, transparent border).

func decodeFile(t *testing.T, name string) image.Image {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if format != "webp" {
		t.Fatalf("%s: format %q", name, format)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 40 || cfg.Height != 30 {
		t.Fatalf("%s: config %+v %v", name, cfg, err)
	}
	if b := img.Bounds(); b.Dx() != 40 || b.Dy() != 30 {
		t.Fatalf("%s: bounds %v", name, b)
	}
	return img
}

func rgba(img image.Image, x, y int) [4]int {
	r, g, b, a := img.At(x, y).RGBA()
	if a == 0 {
		return [4]int{0, 0, 0, 0}
	}
	// un-premultiply
	return [4]int{int(r * 255 / a), int(g * 255 / a), int(b * 255 / a), int(a >> 8)}
}

func near(a, b [4]int, tol int) bool {
	for i := range a {
		d := a[i] - b[i]
		if d < -tol || d > tol {
			return false
		}
	}
	return true
}

var probes = []struct {
	x, y int
	want [4]int
}{
	{2, 2, [4]int{211, 34, 83, 255}},    // gradient start
	{5, 21, [4]int{0, 0, 0, 255}},       // black bar
	{37, 27, [4]int{51, 94, 224, 255}},  // gradient end
	{20, 15, [4]int{201, 148, 75, 255}}, // circle centre
	{30, 8, [4]int{114, 70, 168, 255}},  // gradient middle
}

func TestLossless(t *testing.T) {
	img := decodeFile(t, "lossless.webp")
	if got := rgba(img, 0, 0); got[3] != 0 {
		t.Errorf("corner should be transparent, got %v", got)
	}
	for _, p := range probes {
		if got := rgba(img, p.x, p.y); !near(got, p.want, 1) {
			t.Errorf("(%d,%d) = %v, want %v", p.x, p.y, got, p.want)
		}
	}
}

func TestLossy(t *testing.T) {
	img := decodeFile(t, "lossy.webp")
	if e := meanError(img, decodeFile(t, "lossless.webp")); e > 6 {
		t.Errorf("mean error %.2f too high", e)
	}
	if got := rgba(img, 0, 0); !near(got, [4]int{255, 255, 255, 255}, 12) {
		t.Errorf("corner should be white, got %v", got)
	}
}

func TestLossyAlpha(t *testing.T) {
	img := decodeFile(t, "alpha.webp")
	ref := decodeFile(t, "lossless.webp")
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			got, want := rgba(img, x, y), rgba(ref, x, y)
			if got[3] != want[3] {
				t.Fatalf("alpha at (%d,%d) = %d, want %d", x, y, got[3], want[3])
			}
		}
	}
	if e := meanError(img, ref); e > 6 {
		t.Errorf("mean error %.2f too high", e)
	}
}

func TestRejectsGarbage(t *testing.T) {
	for _, in := range [][]byte{
		nil,
		[]byte("RIFF\x04\x00\x00\x00WEBP"),
		[]byte("RIFF\x10\x00\x00\x00WEBPVP8 \x04\x00\x00\x00\x00\x00\x00\x00"),
		[]byte("RIFF\x10\x00\x00\x00WEBPVP8L\x04\x00\x00\x00\x2f\xff\xff\xff"),
	} {
		if _, err := Decode(bytes.NewReader(in)); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
	// truncated real files must fail or decode, never panic
	for _, name := range []string{"lossless.webp", "lossy.webp", "alpha.webp"} {
		data, _ := os.ReadFile("testdata/" + name)
		for n := 0; n < len(data); n += 7 {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s truncated at %d: panic %v", name, n, r)
					}
				}()
				Decode(bytes.NewReader(data[:n]))
			}()
		}
	}
}

// meanError compares the interior (away from the subsampled chroma edges)
// of a lossy decode against the exact lossless one.
func meanError(a, b image.Image) float64 {
	sum, n := 0, 0
	for y := 5; y < 25; y++ {
		for x := 5; x < 35; x++ {
			p, q := rgba(a, x, y), rgba(b, x, y)
			for i := 0; i < 3; i++ {
				d := p[i] - q[i]
				if d < 0 {
					d = -d
				}
				sum += d
				n++
			}
		}
	}
	return float64(sum) / float64(n)
}
