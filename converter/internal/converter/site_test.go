package converter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSite converts every HTML page of a real site and checks that each one
// renders: no error, not blank, and every local file it references found.
// It runs only when HTML2PDF_SITE names the site's root folder, e.g.
//
//	HTML2PDF_SITE=path/to/site go test ./internal/converter -run TestSite -v
//
// HTML2PDF_VIEWPORT sets the layout width (default 1280). Missing local files are
// logged; HTML2PDF_STRICT=1 turns them into failures.
func TestSite(t *testing.T) {
	root := os.Getenv("HTML2PDF_SITE")
	if root == "" {
		t.Skip("set HTML2PDF_SITE to a folder of HTML pages")
	}
	viewport := 1280.0
	if v := os.Getenv("HTML2PDF_VIEWPORT"); v != "" {
		if _, err := fmt.Sscan(v, &viewport); err != nil {
			t.Fatal(err)
		}
	}
	var pages []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".")) {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".html") {
			pages = append(pages, p)
		}
		return nil
	})
	if len(pages) == 0 {
		t.Fatalf("no .html files below %s", root)
	}
	for _, p := range pages {
		rel, _ := filepath.Rel(root, p)
		t.Run(filepath.ToSlash(rel), func(t *testing.T) {
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			res, err := Convert(Request{
				FileName: filepath.Base(p), HTML: data, BaseDir: root, DocPath: filepath.ToSlash(rel),
				PageSize: "A4", MarginMM: 10, ViewportWidth: viewport,
			})
			if err != nil {
				t.Fatal(err)
			}
			// a page with content but no drawing is ~650 bytes
			if len(res.PDF) < 2000 && len(strings.TrimSpace(stripTags(string(data)))) > 0 {
				t.Errorf("PDF is blank (%d bytes)", len(res.PDF))
			}
			// files the page links to but the site lacks are reported; with
			// HTML2PDF_STRICT=1 they fail the test
			for _, m := range res.Missing {
				if strings.HasPrefix(m, "http://") || strings.HasPrefix(m, "https://") {
					continue
				}
				if os.Getenv("HTML2PDF_STRICT") == "1" {
					t.Errorf("missing local resource: %s", m)
				} else {
					t.Logf("missing local resource: %s", m)
				}
			}
		})
	}
}

// stripTags drops markup, scripts and styles, keeping text content.
func stripTags(s string) string {
	for _, tag := range []string{"script", "style", "head"} {
		for {
			i := strings.Index(strings.ToLower(s), "<"+tag)
			if i < 0 {
				break
			}
			j := strings.Index(strings.ToLower(s[i:]), "</"+tag+">")
			if j < 0 {
				s = s[:i]
				break
			}
			s = s[:i] + s[i+j+len(tag)+3:]
		}
	}
	var sb strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
