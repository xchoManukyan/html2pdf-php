package resource

import (
	"archive/zip"
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	cases := map[[2]string]string{
		{"local:/site/index.html", "css/a.css"}:             "local:/site/css/a.css",
		{"local:/site/css/a.css", "../img/b.png"}:           "local:/site/img/b.png",
		{"local:/index.html", "../../etc/passwd"}:           "local:/etc/passwd", // clamped at the root
		{"local:/index.html", "/abs.png"}:                   "local:/abs.png",
		{"https://example.com/a/b.html", "c.png"}:           "https://example.com/a/c.png",
		{"https://example.com/a/b.html", "//cdn.example/x"}: "https://cdn.example/x",
		{"local:/index.html", "data:image/png;base64,AA"}:   "data:image/png;base64,AA",
	}
	for in, want := range cases {
		if got := Resolve(in[0], in[1]); got != want {
			t.Errorf("Resolve(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestLocalChroot(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	os.MkdirAll(root, 0o755)
	os.WriteFile(filepath.Join(root, "ok.txt"), []byte("ok"), 0o644)
	os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("secret"), 0o644)
	l := &Loader{BaseDir: root}
	if b, _, err := l.Fetch("local:/ok.txt"); err != nil || string(b) != "ok" {
		t.Fatalf("ok.txt: %q %v", b, err)
	}
	if _, _, err := l.Fetch(Resolve("local:/index.html", "../secret.txt")); err == nil {
		t.Error("files outside the base directory must not be readable")
	}
}

func TestZipBundle(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{"site/index.html": "<p>x</p>", "site/css/a.css": "p{}", "other.html": "<p>y</p>"} {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	files, main, err := ReadZip(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if main != "site/index.html" {
		t.Errorf("main document = %q", main)
	}
	l := &Loader{Files: files}
	if b, _, err := l.Fetch(Resolve("local:/"+main, "css/a.css")); err != nil || string(b) != "p{}" {
		t.Errorf("bundled css: %q %v", b, err)
	}
}

func TestRemoteGuards(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write([]byte("body{}"))
	}))
	defer srv.Close()
	off := &Loader{}
	if _, _, err := off.Fetch(srv.URL + "/a.css"); !errors.Is(err, ErrRemoteDisabled) {
		t.Errorf("remote must be off by default, got %v", err)
	}
	guarded := &Loader{AllowRemote: true}
	if _, _, err := guarded.Fetch(srv.URL + "/a.css"); err == nil {
		t.Error("loopback addresses must be refused (SSRF protection)")
	}
	open := &Loader{AllowRemote: true, AllowPrivate: true}
	b, typ, err := open.Fetch(srv.URL + "/a.css")
	if err != nil || string(b) != "body{}" || typ != "text/css" {
		t.Errorf("remote fetch: %q %q %v", b, typ, err)
	}
}
