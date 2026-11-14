package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func pngFile() []byte {
	var b bytes.Buffer
	png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	return b.Bytes()
}

type part struct {
	name, path string
	data       []byte
}

func upload(t *testing.T, h http.Handler, parts []part, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, p := range parts {
		fw, _ := mw.CreateFormFile("file", p.name)
		fw.Write(p.data)
		mw.WriteField("path", p.path)
	}
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	mw.Close()
	req := httptest.NewRequest("POST", "/api/convert", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const page = `<html><head><title>T</title><link rel="stylesheet" href="styles.css"></head>
<body><h1>Home</h1><img src="home (long version).png"></body></html>`

func TestUploadWithAssets(t *testing.T) {
	h := New(Config{})
	rec := upload(t, h, []part{
		{"home-long-version.html", "home-long-version.html", []byte(page)},
		{"styles.css", "styles.css", []byte("h1 { color: red }")},
		{"home (long version).png", "home (long version).png", pngFile()},
	}, nil)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if m := rec.Header().Get("X-Missing-Resources"); m != "" {
		t.Errorf("nothing should be missing, got %s", m)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("/Subtype /Image")) {
		t.Error("the uploaded image should be embedded")
	}
	// the stored result is served under its file name
	u := rec.Header().Get("X-Result-URL")
	if !strings.HasSuffix(u, "/home-long-version.pdf") {
		t.Fatalf("result URL %q should end in the PDF name", u)
	}
	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest("GET", u, nil))
	if get.Code != 200 || !bytes.Equal(get.Body.Bytes(), rec.Body.Bytes()) {
		t.Errorf("stored result: status %d, %d bytes", get.Code, get.Body.Len())
	}
	if cd := get.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "inline;") || !strings.Contains(cd, `filename="home-long-version.pdf"`) {
		t.Errorf("Content-Disposition %q", cd)
	}
	missing := httptest.NewRecorder()
	h.ServeHTTP(missing, httptest.NewRequest("GET", "/api/result/nope/x.pdf", nil))
	if missing.Code != 404 {
		t.Errorf("unknown result should be 404, got %d", missing.Code)
	}
}

func TestMissingResourcesReported(t *testing.T) {
	rec := upload(t, New(Config{}), []part{{"home-long-version.html", "", []byte(page)}}, nil)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	raw, _ := url.PathUnescape(rec.Header().Get("X-Missing-Resources"))
	var list []string
	json.Unmarshal([]byte(raw), &list)
	if len(list) != 2 || list[0] != "home (long version).png" || list[1] != "styles.css" {
		t.Errorf("missing = %q", list)
	}
}

func TestFolderUploadPicksMain(t *testing.T) {
	rec := upload(t, New(Config{}), []part{
		{"a.html", "site/a.html", []byte("<p>A</p>")},
		{"b.html", "site/b.html", []byte(`<link rel="stylesheet" href="css/x.css"><p>B</p>`)},
		{"x.css", "site/css/x.css", []byte("p{color:red}")},
	}, map[string]string{"main": "site/b.html"})
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), `"b.pdf"`) {
		t.Errorf("main document should be b.html, got %q", rec.Header().Get("Content-Disposition"))
	}
	if m := rec.Header().Get("X-Missing-Resources"); m != "" {
		t.Errorf("css/x.css relative to site/b.html should resolve, missing %s", m)
	}
	io.Copy(io.Discard, rec.Body)
}

func TestFriendlyReason(t *testing.T) {
	for in, want := range map[string]string{
		"remote resources are disabled":                                           "remote loading is off",
		`Get "https://x.invalid/a.png": dial tcp: lookup x.invalid: no such host`: "host not found",
		"www.google.com: HTTP 404":                                                "HTTP 404",
		`Get "http://x/a.png": context deadline exceeded`:                         "timed out",
		"not a supported image (text/html)":                                       "not a supported image (text/html)",
	} {
		if got := friendlyReason(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestCSSLibField(t *testing.T) {
	doc := []byte(`<button class="btn btn-primary">Pay</button>`)
	post := func(libs ...string) string {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, _ := mw.CreateFormFile("file", "p.html")
		fw.Write(doc)
		for _, l := range libs { // one field per checked box
			mw.WriteField("cssLib", l)
		}
		mw.Close()
		req := httptest.NewRequest("POST", "/api/convert", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		New(Config{}).ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%q: status %d: %s", libs, rec.Code, rec.Body)
		}
		return rec.Header().Get("X-CSS-Lib")
	}
	if got := post("tailwind", "bootstrap5"); got != "bootstrap5; selected, tailwind; selected" {
		t.Errorf("two checked libraries: X-CSS-Lib = %q", got)
	}
	if got := post(); got != "" {
		t.Errorf("nothing checked, nothing to detect: X-CSS-Lib = %q", got)
	}
	if got := post("none"); got != "" {
		t.Errorf("none: X-CSS-Lib = %q", got)
	}
}
