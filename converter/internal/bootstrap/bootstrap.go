// Package bootstrap bundles the Bootstrap CSS framework (v5.3.9 and v4.6.2,
// MIT license, see LICENSE) so that pages linking it from a CDN render
// without network access, the same way the tailwind package covers pages
// that use Tailwind's browser script.
package bootstrap

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io"
	"path"
	"regexp"
	"strings"
	"sync"
)

//go:embed bootstrap5.min.css.gz bootstrap4.min.css.gz
var files embed.FS

// Versions are the bundled releases by major version.
var Versions = map[int]string{5: "5.3.9", 4: "4.6.2"}

var (
	once  [6]sync.Once
	cache [6]string
)

// CSS returns the bundled stylesheet of a major version (4 or 5).
func CSS(major int) (string, bool) {
	if _, ok := Versions[major]; !ok {
		return "", false
	}
	once[major].Do(func() {
		data, err := files.ReadFile("bootstrap" + string(rune('0'+major)) + ".min.css.gz")
		if err != nil {
			return
		}
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return
		}
		css, err := io.ReadAll(zr)
		if err == nil {
			cache[major] = string(css)
		}
	})
	return cache[major], cache[major] != ""
}

// cdnHosts serve Bootstrap releases.
var cdnHosts = []string{
	"cdn.jsdelivr.net", "cdnjs.cloudflare.com", "unpkg.com", "stackpath.bootstrapcdn.com",
	"maxcdn.bootstrapcdn.com", "netdna.bootstrapcdn.com", "getbootstrap.com", "fastly.jsdelivr.net",
}

var versionRe = regexp.MustCompile(`bootstrap[@/-]v?(\d+)(\.\d+)*`)

// Match tells whether a stylesheet URL is Bootstrap's main stylesheet and
// which major version it is. fromCDN is true for the public CDNs, whose
// copy can be replaced by the bundled one. Grid-only, reboot-only and RTL
// builds are not matched.
func Match(href string) (major int, fromCDN bool, ok bool) {
	h := strings.ToLower(strings.TrimSpace(href))
	if i := strings.IndexAny(h, "?#"); i >= 0 {
		h = h[:i]
	}
	switch path.Base(h) {
	case "bootstrap.min.css", "bootstrap.css":
	default:
		return 0, false, false
	}
	for _, host := range cdnHosts {
		if strings.Contains(h, "//"+host+"/") {
			fromCDN = true
		}
	}
	major = 5 // the current release when the URL does not say
	if m := versionRe.FindStringSubmatch(h); m != nil {
		major = int(m[1][0] - '0')
		if len(m[1]) > 1 {
			major = 0
		}
	}
	if _, ok := Versions[major]; !ok {
		return 0, fromCDN, false
	}
	return major, fromCDN, true
}
