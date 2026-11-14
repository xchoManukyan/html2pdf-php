package bootstrap

import (
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		href          string
		major         int
		fromCDN, isBS bool
	}{
		{"https://cdn.jsdelivr.net/npm/bootstrap@5.3.3/dist/css/bootstrap.min.css", 5, true, true},
		{"https://cdn.jsdelivr.net/npm/bootstrap@4.6.2/dist/css/bootstrap.min.css", 4, true, true},
		{"https://cdnjs.cloudflare.com/ajax/libs/twitter-bootstrap/4.6.2/css/bootstrap.min.css", 4, true, true},
		{"https://stackpath.bootstrapcdn.com/bootstrap/4.5.2/css/bootstrap.min.css", 4, true, true},
		{"https://unpkg.com/bootstrap@5/dist/css/bootstrap.css?v=1", 5, true, true},
		{"https://cdn.jsdelivr.net/npm/bootstrap/dist/css/bootstrap.min.css", 5, true, true},
		{"local:/css/bootstrap.min.css", 5, false, true},
		{"https://example.com/static/bootstrap.min.css", 5, false, true},
		{"https://maxcdn.bootstrapcdn.com/bootstrap/3.4.1/css/bootstrap.min.css", 0, true, false}, // v3 is not bundled
		{"https://cdn.jsdelivr.net/npm/bootstrap@5.3.3/dist/css/bootstrap-grid.min.css", 0, false, false},
		{"https://cdn.jsdelivr.net/npm/bootstrap@5.3.3/dist/js/bootstrap.bundle.min.js", 0, false, false},
		{"https://cdn.jsdelivr.net/npm/bootstrap-icons@1.11.3/font/bootstrap-icons.min.css", 0, false, false},
	}
	for _, c := range cases {
		major, cdn, ok := Match(c.href)
		if ok != c.isBS || (ok && (major != c.major || cdn != c.fromCDN)) {
			t.Errorf("%s: got %d %v %v, want %d %v %v", c.href, major, cdn, ok, c.major, c.fromCDN, c.isBS)
		}
	}
}

func TestBundledCSS(t *testing.T) {
	for major, version := range Versions {
		css, ok := CSS(major)
		if !ok || !strings.Contains(css[:300], "Bootstrap v"+version) && !strings.Contains(css[:300], "Bootstrap  v"+version) {
			t.Errorf("v%d: bundled stylesheet missing or not %s", major, version)
		}
		if !strings.Contains(css, ".btn-primary") || !strings.Contains(css, ".col-md-6") {
			t.Errorf("v%d: stylesheet looks incomplete", major)
		}
	}
	if _, ok := CSS(3); ok {
		t.Error("v3 is not bundled")
	}
}
