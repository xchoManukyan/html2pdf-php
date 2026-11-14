// Package server exposes the converter over HTTP: an upload page and a
// POST /api/convert endpoint that answers with the PDF.
package server

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/xchoManukyan/html2pdf/converter/internal/converter"
)

//go:embed static/index.html
var static embed.FS

// Config holds server-wide settings.
type Config struct {
	// AllowRemote lets users enable downloading of remote resources
	// (images, stylesheets, fonts by URL). Off by default, like dompdf.
	AllowRemote bool
}

// New returns the HTTP handler of the application.
func New(cfg Config) http.Handler {
	store := newResults()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", index)
	mux.HandleFunc("POST /api/convert", func(w http.ResponseWriter, r *http.Request) { convert(w, r, cfg, store) })
	mux.HandleFunc("GET /api/result/{id}/{name}", func(w http.ResponseWriter, r *http.Request) { result(w, r, store) })
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"allowRemote": cfg.AllowRemote})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	return logRequests(mux)
}

func index(w http.ResponseWriter, _ *http.Request) {
	page, _ := static.ReadFile("static/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(page)
}

func convert(w http.ResponseWriter, r *http.Request, cfg Config, store *results) {
	r.Body = http.MaxBytesReader(w, r.Body, converter.MaxBundleSize+1<<20)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			jsonError(w, http.StatusRequestEntityTooLarge, converter.ErrTooLarge.Error())
			return
		}
		jsonError(w, http.StatusBadRequest, "invalid upload: "+err.Error())
		return
	}
	defer r.MultipartForm.RemoveAll()

	req := converter.Request{
		PageSize:    r.FormValue("pageSize"),
		Landscape:   r.FormValue("orientation") == "landscape",
		PageNumbers: r.FormValue("pageNumbers") == "on" || r.FormValue("pageNumbers") == "true",
	}
	req.MarginMM, _ = strconv.ParseFloat(r.FormValue("margin"), 64)
	req.ViewportWidth, _ = strconv.ParseFloat(r.FormValue("viewport"), 64)
	req.CSSLib = strings.Join(r.Form["cssLib"], ",") // one field per checked box, or a list
	req.AllowRemote = cfg.AllowRemote && r.FormValue("allowRemote") == "true"
	req.UserPassword = r.FormValue("userPassword")
	req.OwnerPassword = r.FormValue("ownerPassword")
	req.DenyPrint = r.FormValue("denyPrint") == "true"
	req.DenyCopy = r.FormValue("denyCopy") == "true"
	req.DenyModify = r.FormValue("denyModify") == "true"

	if files := r.MultipartForm.File["file"]; len(files) > 0 {
		if err := readUpload(r, files, &req); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		html := r.FormValue("html")
		if strings.TrimSpace(html) == "" {
			jsonError(w, http.StatusBadRequest, "choose an HTML file or paste some HTML")
			return
		}
		req.HTML = []byte(html)
		req.FileName = "document.html"
	}

	res, err := converter.Convert(req)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, converter.ErrTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		jsonError(w, status, err.Error())
		return
	}

	disposition := "attachment"
	if r.URL.Query().Get("inline") == "1" {
		disposition = "inline"
	}
	id := store.put(res.FileName, res.PDF)
	h := w.Header()
	h.Set("X-Result-URL", "/api/result/"+id+"/"+url.PathEscape(res.FileName))
	if len(res.Missing) > 0 {
		list, _ := json.Marshal(res.Missing)
		h.Set("X-Missing-Resources", url.PathEscape(string(list)))
		type detail struct {
			Ref    string `json:"ref"`
			Reason string `json:"reason"`
		}
		var details []detail
		for _, f := range res.Failures {
			details = append(details, detail{f.Ref, friendlyReason(f.Reason)})
		}
		js, _ := json.Marshal(details)
		h.Set("X-Missing-Details", url.PathEscape(string(js)))
	}
	if len(res.CSSLibs) > 0 {
		var libs []string
		for _, d := range res.CSSLibs {
			libs = append(libs, d.Lib+"; "+d.Source)
		}
		h.Set("X-CSS-Lib", strings.Join(libs, ", "))
	}
	h.Set("X-Render-Time-Ms", strconv.FormatInt(res.Took.Milliseconds(), 10))
	writePDF(w, disposition, res.FileName, res.PDF)
}

// friendlyReason shortens loader errors for people: "HTTP 404", "host not
// found", "remote loading is off" ...
func friendlyReason(s string) string {
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "remote resources are disabled"):
		return "remote loading is off"
	case strings.Contains(l, "no such host"):
		return "host not found"
	case strings.Contains(l, "timeout") || strings.Contains(l, "deadline exceeded"):
		return "timed out"
	case strings.Contains(l, "private"):
		return "private network address refused"
	case strings.Contains(l, "connection refused"):
		return "connection refused"
	case strings.Contains(l, "too many redirects"):
		return "too many redirects"
	case strings.Contains(l, "http "):
		return s[strings.Index(l, "http "):]
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

// result serves a stored PDF; the URL ends in its file name, so the
// browser's viewer saves it under that name.
func result(w http.ResponseWriter, r *http.Request, store *results) {
	it := store.get(r.PathValue("id"))
	if it == nil {
		http.Error(w, "this PDF has expired, convert the file again", http.StatusNotFound)
		return
	}
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Cache-Control", "private, max-age=1800")
	writePDF(w, disposition, it.name, it.pdf)
}

func writePDF(w http.ResponseWriter, disposition, name string, data []byte) {
	h := w.Header()
	h.Set("Content-Type", "application/pdf")
	h.Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disposition, name, url.PathEscape(name)))
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("X-Content-Type-Options", "nosniff")
	w.Write(data)
}

// readUpload takes one HTML or ZIP file, or an HTML document together with
// the files it references (stylesheets, images, fonts). Optional "path"
// fields carry each file's relative path (folder drops); "main" names the
// document when several HTML files are present.
func readUpload(r *http.Request, files []*multipart.FileHeader, req *converter.Request) error {
	paths := r.MultipartForm.Value["path"]
	contents := map[string][]byte{}
	var names []string
	total := 0
	for i, fh := range files {
		name := fh.Filename
		if i < len(paths) && paths[i] != "" {
			name = paths[i]
		}
		name = strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(name, `\`, "/")), "/")
		if name == "" || name == "." {
			continue
		}
		f, err := fh.Open()
		if err != nil {
			return errors.New("could not read " + fh.Filename)
		}
		data, err := io.ReadAll(io.LimitReader(f, converter.MaxBundleSize+1))
		f.Close()
		if err != nil {
			return errors.New("could not read " + fh.Filename)
		}
		if total += len(data); total > converter.MaxBundleSize {
			return fmt.Errorf("the files are larger than %d MB together; upload only the page and the files it uses", converter.MaxBundleSize>>20)
		}
		if _, dup := contents[name]; !dup {
			names = append(names, name)
		}
		contents[name] = data
	}
	if len(names) == 0 {
		return errors.New("choose an HTML file")
	}
	if len(names) == 1 {
		req.FileName, req.HTML = names[0], contents[names[0]]
		return nil
	}
	main := mainDocument(names, r.FormValue("main"))
	if main == "" {
		return errors.New("add an .html file to the selection (or upload a single ZIP)")
	}
	req.FileName, req.HTML = main, contents[main]
	req.Assets = contents
	return nil
}

func isHTML(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm", ".xhtml":
		return true
	}
	return false
}

// mainDocument picks the requested HTML file, else index.html, else the
// shallowest, alphabetically first HTML file.
func mainDocument(names []string, want string) string {
	want = strings.TrimPrefix(path.Clean("/"+want), "/")
	best := ""
	for _, n := range names {
		if !isHTML(n) {
			continue
		}
		if n == want {
			return n
		}
		if best == "" || better(n, best) {
			best = n
		}
	}
	return best
}

func better(a, b string) bool {
	ai := strings.EqualFold(path.Base(a), "index.html")
	bi := strings.EqualFold(path.Base(b), "index.html")
	if ai != bi {
		return ai
	}
	if da, db := strings.Count(a, "/"), strings.Count(b, "/"); da != db {
		return da < db
	}
	return a < b
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}
