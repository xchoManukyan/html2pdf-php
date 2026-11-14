package render

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xchoManukyan/html2pdf/converter/internal/resource"
)

// External images: a local HTTP server plays the remote hosts. The loader
// normally refuses loopback addresses (SSRF protection), so the tests
// allow private addresses explicitly.

func solidPNG(c color.Color) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < 64; i++ {
		img.Set(i%8, i/8, c)
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func solidJPEG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var b bytes.Buffer
	jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

type imageServer struct {
	*httptest.Server
	mu   sync.Mutex
	hits map[string]int
}

func (s *imageServer) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

func newImageServer(t *testing.T) *imageServer {
	s := &imageServer{hits: map[string]int{}}
	webp, _ := os.ReadFile("../webp/testdata/lossless.webp")
	mux := http.NewServeMux()
	serve := func(typ string, data []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if typ != "" {
				w.Header().Set("Content-Type", typ)
			}
			w.Write(data)
		}
	}
	mux.HandleFunc("/img/red.png", serve("image/png", solidPNG(color.NRGBA{255, 0, 0, 255})))
	mux.HandleFunc("/img/photo.jpg", serve("image/jpeg", solidJPEG()))
	mux.HandleFunc("/img/pic.webp", serve("image/webp", webp))
	mux.HandleFunc("/img/logo.svg", serve("image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="40" height="20" fill="#00ff00"/></svg>`)))
	mux.HandleFunc("/img/no-type", serve("", solidPNG(color.Black)))
	mux.HandleFunc("/img/wrong-type.png", serve("text/plain", solidPNG(color.Black)))
	mux.HandleFunc("/img/my%20image.png", serve("image/png", solidPNG(color.Black)))
	mux.HandleFunc("/img/q.png", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("w") != "200" {
			http.NotFound(w, r)
			return
		}
		w.Write(solidPNG(color.Black))
	})
	mux.HandleFunc("/img/html-page.png", serve("text/html", []byte("<html><body>Hotlinking not allowed</body></html>")))
	mux.HandleFunc("/img/broken.png", serve("image/png", []byte("\x89PNG\r\n\x1a\nbroken")))
	mux.HandleFunc("/img/missing.png", http.NotFound)
	mux.HandleFunc("/img/error.png", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", 500) })
	mux.HandleFunc("/img/moved.png", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/img/red.png", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/img/loop.png", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/img/loop.png", http.StatusFound)
	})
	mux.HandleFunc("/img/to-file.png", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	})
	mux.HandleFunc("/img/huge.png", func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte{0}, 3<<20))
	})
	mux.HandleFunc("/img/slow.png", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	})
	mux.HandleFunc("/css/site.css", serve("text/css", []byte(`.hero { height: 40px; background: url(../img/red.png) }
		ul.icons { list-style-image: url("/img/logo.svg") }`)))
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits[r.URL.Path]++
		s.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func remoteLoader() *resource.Loader {
	return &resource.Loader{AllowRemote: true, AllowPrivate: true, MaxSize: 2 << 20, Timeout: 2 * time.Second}
}

// imageCount counts image XObjects (soft masks are images too, so callers
// use opaque images).
func imageCount(pdf string) int { return strings.Count(pdf, "/Subtype /Image") }

func TestRemoteImagesDisabledByDefault(t *testing.T) {
	s := newImageServer(t)
	l := &resource.Loader{} // the default: no remote access
	out, _ := convertWith(t, `<img src="`+s.URL+`/img/red.png" alt="Red square">`, Options{Loader: l})
	if imageCount(out) != 0 || s.count("/img/red.png") != 0 {
		t.Error("remote images must not be fetched unless allowed")
	}
	if m := l.Missing(); len(m) != 1 || !strings.HasSuffix(m[0], "/img/red.png") {
		t.Errorf("the blocked image should be reported, got %q", m)
	}
}

func TestRemoteImageFormats(t *testing.T) {
	s := newImageServer(t)
	for _, path := range []string{"/img/red.png", "/img/photo.jpg", "/img/pic.webp", "/img/no-type",
		"/img/wrong-type.png", "/img/my%20image.png", "/img/my image.png", "/img/q.png?w=200&amp;h=100", "/img/moved.png"} {
		l := remoteLoader()
		out, _ := convertWith(t, `<img src="`+s.URL+path+`">`, Options{Loader: l})
		if imageCount(out) < 1 {
			t.Errorf("%s: image not embedded (missing %q)", path, l.Missing())
		}
	}
	// SVG becomes vector drawing, not an image XObject
	l := remoteLoader()
	_, c := convertWith(t, `<img src="`+s.URL+`/img/logo.svg">`, Options{Loader: l})
	if !strings.Contains(c, "0 1 0 rg") {
		t.Error("remote SVG should be drawn")
	}
}

func TestRemoteImageFailuresAreReported(t *testing.T) {
	s := newImageServer(t)
	for _, path := range []string{"/img/missing.png", "/img/error.png", "/img/html-page.png", "/img/broken.png",
		"/img/loop.png", "/img/to-file.png", "/img/huge.png", "/img/slow.png"} {
		l := remoteLoader()
		start := time.Now()
		out, c := convertWith(t, `<p>before</p><img src="`+s.URL+path+`" alt="Alt text"><p>after</p>`, Options{Loader: l})
		if imageCount(out) != 0 {
			t.Errorf("%s: nothing should be embedded", path)
		}
		if !strings.Contains(c, "(after)") {
			t.Errorf("%s: the rest of the page must still render", path)
		}
		if m := l.Missing(); len(m) != 1 || !strings.Contains(m[0], path) {
			t.Errorf("%s: should be reported as missing, got %q", path, m)
		}
		if d := time.Since(start); d > 4*time.Second {
			t.Errorf("%s: took %s, the timeout should cut it short", path, d)
		}
	}
}

func TestRemoteImagesInCSSAndReuse(t *testing.T) {
	s := newImageServer(t)
	l := remoteLoader()
	page := `<html><head><link rel="stylesheet" href="` + s.URL + `/css/site.css"></head><body>
		<div class="hero"></div><ul class="icons"><li>one</li><li>two</li></ul>
		<img src="` + s.URL + `/img/red.png"><img src="` + s.URL + `/img/red.png"></body></html>`
	out, c := convertWith(t, page, Options{Loader: l})
	if s.count("/img/red.png") != 1 {
		t.Errorf("the same image should be downloaded once, got %d requests", s.count("/img/red.png"))
	}
	if s.count("/img/logo.svg") != 1 || !strings.Contains(c, "0 1 0 rg") {
		t.Error("list-style-image from a remote stylesheet (root-relative URL) should load")
	}
	if imageCount(out) != 1 {
		t.Errorf("one image XObject shared by background and <img>, got %d", imageCount(out))
	}
	if len(l.Missing()) != 0 {
		t.Errorf("nothing should be missing: %q", l.Missing())
	}
}

func TestProtocolRelativeURLs(t *testing.T) {
	// "//host/x.png" takes the page's scheme; a local file has none, so https
	if got := resource.Resolve("local:/index.html", "//cdn.example.com/x.png"); got != "https://cdn.example.com/x.png" {
		t.Errorf("local page: %s", got)
	}
	if got := resource.Resolve("http://site.example/a/page.html", "//cdn.example.com/x.png"); got != "http://cdn.example.com/x.png" {
		t.Errorf("http page: %s", got)
	}
}

func TestRemoteImagesInParallel(t *testing.T) {
	// 12 images that each take 300ms must not take 12 x 300ms
	var inFlight, peak int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(300 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		w.Write(solidPNG(color.NRGBA{0, 0, byte(len(r.URL.Path)), 255}))
	}))
	defer srv.Close()
	var page strings.Builder
	for i := 0; i < 12; i++ {
		page.WriteString(`<img src="` + srv.URL + `/img` + strings.Repeat("x", i) + `.png">`)
	}
	start := time.Now()
	out, _ := convertWith(t, page.String(), Options{Loader: remoteLoader()})
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("12 slow images took %s; downloads should overlap (peak %d at once)", d, peak)
	}
	if imageCount(out) != 12 {
		t.Errorf("want 12 images, got %d", imageCount(out))
	}
}

func TestSrcsetAndPicture(t *testing.T) {
	s := newImageServer(t)
	cases := map[string]string{
		// the widest candidate is best for print
		`<img src="` + s.URL + `/img/missing.png" srcset="` + s.URL + `/img/photo.jpg 400w, ` + s.URL + `/img/red.png 800w">`:                                                                  "/img/red.png",
		`<img srcset="` + s.URL + `/img/photo.jpg 1x, ` + s.URL + `/img/red.png 2x">`:                                                                                                          "/img/red.png",
		`<picture><source type="image/avif" srcset="` + s.URL + `/img/x.avif"><source type="image/webp" srcset="` + s.URL + `/img/pic.webp"><img src="` + s.URL + `/img/photo.jpg"></picture>`: "/img/pic.webp",
		`<picture><source media="(max-width: 100px)" srcset="` + s.URL + `/img/photo.jpg"><img src="` + s.URL + `/img/red.png"></picture>`:                                                     "/img/red.png",
	}
	for html, want := range cases {
		l := remoteLoader()
		out, _ := convertWith(t, html, Options{Loader: l})
		if s.count(want) == 0 || imageCount(out) == 0 {
			t.Errorf("%s\nshould load %s (missing %q)", html, want, l.Missing())
		}
	}
	if s.count("/img/x.avif") != 0 {
		t.Error("unsupported AVIF source should be skipped without downloading")
	}
}
