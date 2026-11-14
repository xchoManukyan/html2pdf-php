// Package resource loads the files a document refers to: data: URIs,
// files from an in-memory bundle (an uploaded ZIP), files under a local
// base directory (never outside it, like dompdf's "chroot"), and — only
// when explicitly allowed — remote http(s) resources, protected against
// requests to private networks (SSRF).
package resource

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Errors returned by the loader.
var (
	ErrRemoteDisabled = errors.New("remote resources are disabled")
	ErrNotFound       = errors.New("resource not found")
	ErrOutsideRoot    = errors.New("path is outside the allowed directory")
	ErrTooLarge       = errors.New("resource is too large")
	ErrPrivateAddress = errors.New("refusing to connect to a private network address")
)

// Loader resolves and fetches resources. The zero value can only decode
// data: URIs.
type Loader struct {
	// Files is an in-memory bundle (path -> content), e.g. from a ZIP.
	Files map[string][]byte
	// BaseDir allows reading local files below this directory.
	BaseDir string
	// AllowRemote enables http(s) downloads.
	AllowRemote bool
	// MaxSize limits each resource (default 10 MB).
	MaxSize int64
	// AllowPrivate disables the private-address guard (tests only).
	AllowPrivate bool
	// Timeout limits each remote download (default 20s).
	Timeout time.Duration

	mu    sync.Mutex
	cache map[string]result
}

type result struct {
	data []byte
	typ  string
	err  error
}

func (l *Loader) maxSize() int64 {
	if l.MaxSize > 0 {
		return l.MaxSize
	}
	return 10 << 20
}

// Resolve turns ref into an absolute reference relative to base. Local
// references are "local:/dir/file"; remote ones are full URLs.
func Resolve(base, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	lower := strings.ToLower(ref)
	switch {
	case strings.HasPrefix(lower, "data:"), strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"),
		strings.HasPrefix(lower, "local:"):
		return ref
	case strings.HasPrefix(ref, "//"):
		if strings.HasPrefix(strings.ToLower(base), "http:") {
			return "http:" + ref
		}
		return "https:" + ref
	case strings.HasPrefix(lower, "file:"):
		u, err := url.Parse(ref)
		if err != nil {
			return ""
		}
		return "local:" + path.Clean("/"+u.Path)
	}
	if i := strings.IndexAny(ref, "?#"); i >= 0 && !strings.HasPrefix(strings.ToLower(base), "http") {
		ref = ref[:i]
	}
	if strings.HasPrefix(strings.ToLower(base), "http") {
		b, err := url.Parse(base)
		if err != nil {
			return ""
		}
		r, err := url.Parse(ref)
		if err != nil {
			return ""
		}
		return b.ResolveReference(r).String()
	}
	dir := "/"
	if strings.HasPrefix(base, "local:") {
		dir = path.Dir(strings.TrimPrefix(base, "local:"))
	}
	if unescaped, err := url.PathUnescape(ref); err == nil {
		ref = unescaped
	}
	if strings.HasPrefix(ref, "/") {
		return "local:" + path.Clean(ref)
	}
	return "local:" + path.Clean(path.Join(dir, ref))
}

// Fetch returns the content of an absolute reference (see Resolve).
func (l *Loader) Fetch(ref string) ([]byte, string, error) {
	if ref == "" {
		return nil, "", ErrNotFound
	}
	if strings.HasPrefix(strings.ToLower(ref), "data:") {
		return DecodeDataURI(ref)
	}
	l.mu.Lock()
	if l.cache == nil {
		l.cache = map[string]result{}
	}
	if r, ok := l.cache[ref]; ok {
		l.mu.Unlock()
		return r.data, r.typ, r.err
	}
	l.mu.Unlock()

	var r result
	switch {
	case strings.HasPrefix(ref, "local:"):
		r.data, r.err = l.local(strings.TrimPrefix(ref, "local:"))
		r.typ = typeByExt(ref)
	case strings.HasPrefix(strings.ToLower(ref), "http://"), strings.HasPrefix(strings.ToLower(ref), "https://"):
		r.data, r.typ, r.err = l.remote(ref)
	default:
		r.err = ErrNotFound
	}
	l.mu.Lock()
	l.cache[ref] = r
	l.mu.Unlock()
	return r.data, r.typ, r.err
}

func (l *Loader) timeout() time.Duration {
	if l.Timeout > 0 {
		return l.Timeout
	}
	return 20 * time.Second
}

// Forget drops what is known about ref, e.g. a failed fetch whose content
// was supplied another way, so it is not reported as missing.
func (l *Loader) Forget(ref string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.cache, ref)
}

// Reject records that ref was fetched but cannot be used (for example an
// HTML error page or a corrupt file where an image was expected), so it
// is reported by Missing.
func (l *Loader) Reject(ref string, reason error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cache == nil {
		l.cache = map[string]result{}
	}
	l.cache[ref] = result{err: reason}
}

// Prefetch downloads remote references in parallel (at most 8 at a time)
// so that the layout, which needs them one by one, finds them cached.
func (l *Loader) Prefetch(refs []string) {
	if !l.AllowRemote {
		return
	}
	seen := map[string]bool{}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, ref := range refs {
		lower := strings.ToLower(ref)
		if seen[ref] || !(strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")) {
			continue
		}
		seen[ref] = true
		wg.Add(1)
		sem <- struct{}{}
		go func(ref string) {
			defer func() { <-sem; wg.Done() }()
			l.Fetch(ref)
		}(ref)
	}
	wg.Wait()
}

// Missing lists the references that could not be loaded, sorted, with
// local ones shown as bundle paths ("img/logo.png").
func (l *Loader) Missing() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for ref, r := range l.cache {
		if r.err != nil {
			out = append(out, strings.TrimPrefix(ref, "local:/"))
		}
	}
	sort.Strings(out)
	return out
}

// Failure is a reference that could not be used, with the reason.
type Failure struct {
	Ref, Reason string
}

// Failures is Missing with reasons ("HTTP 404", "remote resources are
// disabled", "not a supported image", ...).
func (l *Loader) Failures() []Failure {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Failure
	for ref, r := range l.cache {
		if r.err != nil {
			out = append(out, Failure{Ref: strings.TrimPrefix(ref, "local:/"), Reason: r.err.Error()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out
}

func (l *Loader) local(p string) ([]byte, error) {
	p = path.Clean("/" + p)
	if l.Files != nil {
		if b, ok := l.Files[strings.TrimPrefix(p, "/")]; ok {
			return b, nil
		}
		// case-insensitive fallback (Windows-made bundles)
		for name, b := range l.Files {
			if strings.EqualFold(name, strings.TrimPrefix(p, "/")) {
				return b, nil
			}
		}
	}
	if l.BaseDir == "" {
		return nil, ErrNotFound
	}
	root, err := filepath.Abs(l.BaseDir)
	if err != nil {
		return nil, err
	}
	full := filepath.Join(root, filepath.FromSlash(p))
	if rel, err := filepath.Rel(root, full); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, ErrOutsideRoot
	}
	// resolve symlinks so a link cannot escape the root either
	if real, err := filepath.EvalSymlinks(full); err == nil {
		realRoot, _ := filepath.EvalSymlinks(root)
		if rel, err := filepath.Rel(realRoot, real); err != nil || strings.HasPrefix(rel, "..") {
			return nil, ErrOutsideRoot
		}
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, ErrNotFound
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || st.IsDir() {
		return nil, ErrNotFound
	} else if st.Size() > l.maxSize() {
		return nil, ErrTooLarge
	}
	return io.ReadAll(io.LimitReader(f, l.maxSize()))
}

// privateIP reports addresses that must not be reached from a server
// fetching user-supplied URLs.
func privateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.Equal(net.ParseIP("169.254.169.254")) // cloud metadata
}

func (l *Loader) remote(raw string) ([]byte, string, error) {
	if !l.AllowRemote {
		return nil, "", ErrRemoteDisabled
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, "", ErrNotFound
	}
	dialer := &net.Dialer{
		Timeout: min(10*time.Second, l.timeout()),
		Control: func(network, address string, _ syscall.RawConn) error {
			if l.AllowPrivate {
				return nil
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip == nil || privateIP(ip) {
				return ErrPrivateAddress
			}
			return nil
		},
	}
	client := &http.Client{
		Timeout: l.timeout(),
		Transport: &http.Transport{
			Proxy:                 nil, // never route through an environment proxy
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   min(10*time.Second, l.timeout()),
			ResponseHeaderTimeout: l.timeout(),
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("redirect to a non-http URL")
			}
			return nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), l.timeout()+time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "html2pdf (Go)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s: HTTP %d", u.Host, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, l.maxSize()+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > l.maxSize() {
		return nil, "", ErrTooLarge
	}
	typ := resp.Header.Get("Content-Type")
	if typ == "" {
		typ = typeByExt(u.Path)
	}
	return data, typ, nil
}

func typeByExt(p string) string {
	switch strings.ToLower(path.Ext(strings.SplitN(p, "?", 2)[0])) {
	case ".css":
		return "text/css"
	case ".svg", ".svgz":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".webp":
		return "image/webp"
	case ".ttf", ".otf":
		return "font/ttf"
	case ".woff":
		return "font/woff"
	case ".html", ".htm":
		return "text/html"
	}
	return ""
}

// DecodeDataURI decodes a data: URI (base64 or percent-encoded).
func DecodeDataURI(src string) ([]byte, string, error) {
	if !strings.HasPrefix(strings.ToLower(src), "data:") {
		return nil, "", ErrNotFound
	}
	meta, payload, ok := strings.Cut(src[5:], ",")
	if !ok {
		return nil, "", ErrNotFound
	}
	typ := strings.ToLower(strings.TrimSpace(strings.SplitN(meta, ";", 2)[0]))
	if strings.HasSuffix(strings.ToLower(meta), ";base64") {
		payload = strings.Map(func(r rune) rune {
			if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
				return -1
			}
			return r
		}, payload)
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			data, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(payload, "="))
			if err != nil {
				return nil, "", err
			}
		}
		return data, typ, nil
	}
	data, err := url.PathUnescape(payload)
	if err != nil {
		data = payload
	}
	return []byte(data), typ, nil
}

// Limits for ZIP bundles.
const (
	maxZipFiles = 2000
	maxZipTotal = 100 << 20
)

// ReadZip loads a ZIP bundle into memory and returns its files and the
// path of the main HTML document (index.html if present).
func ReadZip(data []byte) (map[string][]byte, string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, "", fmt.Errorf("invalid ZIP file: %w", err)
	}
	if len(zr.File) > maxZipFiles {
		return nil, "", errors.New("ZIP contains too many files")
	}
	files := map[string][]byte{}
	var total int64
	var main string
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := path.Clean("/" + strings.ReplaceAll(f.Name, "\\", "/"))[1:]
		if strings.HasPrefix(name, "__MACOSX/") {
			continue
		}
		if f.UncompressedSize64 > 50<<20 {
			return nil, "", ErrTooLarge
		}
		rc, err := f.Open()
		if err != nil {
			return nil, "", err
		}
		b, err := io.ReadAll(io.LimitReader(rc, 50<<20+1))
		rc.Close()
		if err != nil {
			return nil, "", err
		}
		total += int64(len(b))
		if total > maxZipTotal {
			return nil, "", errors.New("ZIP contents are larger than 100 MB")
		}
		files[name] = b
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm") {
			if main == "" || betterMain(name, main) {
				main = name
			}
		}
	}
	if main == "" {
		return nil, "", errors.New("the ZIP file contains no .html document")
	}
	return files, main, nil
}

// betterMain decides which HTML file of a bundle is the document: an
// index.html wins, then the shallowest file, then alphabetical order.
func betterMain(a, b string) bool {
	ai := strings.EqualFold(path.Base(a), "index.html")
	bi := strings.EqualFold(path.Base(b), "index.html")
	if ai != bi {
		return ai
	}
	da, db := strings.Count(a, "/"), strings.Count(b, "/")
	if da != db {
		return da < db
	}
	return a < b
}
