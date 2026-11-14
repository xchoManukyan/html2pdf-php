// Package fonts finds font files on the machine and matches CSS font
// requests (family list, weight, style) to concrete faces, including
// per-character fallback for scripts the requested font does not cover.
package fonts

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/xchoManukyan/html2pdf/converter/internal/ttf"
)

// Face is one font face: a file on disk (Path, Index) or in-memory data
// from a CSS @font-face rule.
type Face struct {
	Family  string // lower-case family name
	Weight  int
	Stretch int // 1..9, 5 = normal width
	Italic  bool
	Path    string
	Index   int
	Data    []byte
	// Fetch, when set, supplies Data on first use (web fonts are only
	// downloaded if text actually uses them).
	Fetch func() ([]byte, error)

	dataOnce sync.Once
	dataErr  error

	once     sync.Once
	font     *ttf.Font
	err      error
	covOnce  sync.Once
	coverage *ttf.Coverage
}

// source returns the in-memory font data, fetching it once if needed. It
// is nil for faces read from Path.
func (f *Face) source() ([]byte, error) {
	if f.Fetch != nil {
		f.dataOnce.Do(func() { f.Data, f.dataErr = f.Fetch() })
		return f.Data, f.dataErr
	}
	return f.Data, nil
}

// Load parses the face (once; the result is shared and read-only).
func (f *Face) Load() (*ttf.Font, error) {
	f.once.Do(func() {
		data, err := f.source()
		if err != nil {
			f.err = err
			return
		}
		if data == nil {
			data, f.err = os.ReadFile(f.Path)
			if f.err != nil {
				return
			}
		}
		f.font, f.err = ttf.Parse(data, f.Index)
		if f.err == nil && !f.font.Embeddable() {
			f.font, f.err = nil, os.ErrPermission
		}
	})
	return f.font, f.err
}

// Has reports whether the face covers r, reading only its cmap.
func (f *Face) Has(r rune) bool {
	f.covOnce.Do(func() {
		if data, err := f.source(); err != nil {
			return
		} else if data != nil {
			f.coverage, _ = ttf.ReadCoverage(bytes.NewReader(data), f.Index)
			return
		}
		file, err := os.Open(f.Path)
		if err != nil {
			return
		}
		defer file.Close()
		f.coverage, _ = ttf.ReadCoverage(file, f.Index)
	})
	return f.coverage != nil && f.coverage.Has(r)
}

// DB is a set of faces indexed by family.
type DB struct {
	mu       sync.RWMutex
	families map[string][]*Face
	all      []*Face
	runeFace sync.Map // rune -> *Face (or nil) fallback cache
}

// NewDB returns an empty database.
func NewDB() *DB { return &DB{families: map[string][]*Face{}} }

// Add registers a face under a family name.
func (db *DB) Add(f *Face) {
	db.mu.Lock()
	defer db.mu.Unlock()
	f.Family = strings.ToLower(strings.TrimSpace(f.Family))
	db.families[f.Family] = append(db.families[f.Family], f)
	db.all = append(db.all, f)
}

// Empty reports whether the database has no faces.
func (db *DB) Empty() bool {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.all) == 0
}

// Match picks the best face of a family for the weight and style, using
// the CSS font matching rules in simplified form.
func (db *DB) Match(family string, weight int, italic bool) *Face {
	db.mu.RLock()
	faces := db.families[strings.ToLower(family)]
	db.mu.RUnlock()
	var best *Face
	bestScore := 1 << 30
	for _, f := range faces {
		score := abs(f.Weight-weight) + 1000*abs(f.Stretch-5)
		if weight > 500 && f.Weight < weight || weight <= 500 && f.Weight > weight {
			score += 1 // prefer the direction CSS prefers on ties
		}
		if f.Italic != italic {
			score += 10000
		}
		if score < bestScore {
			best, bestScore = f, score
		}
	}
	return best
}

// Fallback returns some face that covers r, preferring regular faces.
func (db *DB) Fallback(r rune) *Face {
	if v, ok := db.runeFace.Load(r); ok {
		f, _ := v.(*Face)
		return f
	}
	db.mu.RLock()
	all := db.all
	db.mu.RUnlock()
	var found *Face
	for pass := 0; pass < 2 && found == nil; pass++ {
		for _, f := range all {
			if (pass == 0) != (f.Weight == 400 && !f.Italic) {
				continue
			}
			if f.Has(r) {
				if _, err := f.Load(); err == nil {
					found = f
					break
				}
			}
		}
	}
	db.runeFace.Store(r, found)
	return found
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

var (
	systemOnce sync.Once
	systemDB   *DB
)

// System returns the faces installed on this machine (scanned once).
// Extra directories can be listed in the HTML2PDF_FONTS environment
// variable; a "fonts" folder next to the program or in the working
// directory is scanned too.
func System() *DB {
	systemOnce.Do(func() {
		systemDB = NewDB()
		for _, dir := range fontDirs() {
			scanDir(systemDB, dir)
		}
	})
	return systemDB
}

func fontDirs() []string {
	var dirs []string
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		win := os.Getenv("WINDIR")
		if win == "" {
			win = `C:\Windows`
		}
		dirs = append(dirs, filepath.Join(win, "Fonts"))
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			dirs = append(dirs, filepath.Join(la, "Microsoft", "Windows", "Fonts"))
		}
	case "darwin":
		dirs = append(dirs, "/System/Library/Fonts", "/Library/Fonts", filepath.Join(home, "Library", "Fonts"))
	default:
		dirs = append(dirs, "/usr/share/fonts", "/usr/local/share/fonts",
			filepath.Join(home, ".fonts"), filepath.Join(home, ".local", "share", "fonts"))
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "fonts"))
	}
	dirs = append(dirs, "fonts")
	dirs = append(dirs, filepath.SplitList(os.Getenv("HTML2PDF_FONTS"))...)
	return dirs
}

func scanDir(db *DB, dir string) {
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".ttf", ".ttc", ".otf":
		default:
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()
		n := ttf.NumFaces(file)
		for i := 0; i < n && i < 64; i++ {
			info, err := ttf.ReadInfo(file, i)
			if err != nil || !info.TrueType || !info.Embeddable {
				continue
			}
			families := []string{info.Family}
			if info.LegacyFamily != "" && !strings.EqualFold(info.LegacyFamily, info.Family) {
				families = append(families, info.LegacyFamily)
			}
			for _, fam := range families {
				db.Add(&Face{Family: fam, Weight: info.Weight, Stretch: info.Stretch, Italic: info.Italic, Path: path, Index: i})
			}
			if info.FullName != "" {
				db.Add(&Face{Family: info.FullName, Weight: info.Weight, Stretch: info.Stretch, Italic: info.Italic, Path: path, Index: i})
			}
		}
		return nil
	})
}

// Generic CSS families and well-known aliases, mapped to installed fonts
// in order of preference.
var generic = map[string][]string{
	"serif":        {"Times New Roman", "Liberation Serif", "Tinos", "DejaVu Serif", "Noto Serif", "Georgia", "Times"},
	"sans-serif":   {"Arial", "Liberation Sans", "Arimo", "Helvetica", "DejaVu Sans", "Noto Sans", "Segoe UI", "Verdana"},
	"monospace":    {"Courier New", "Liberation Mono", "Cousine", "Consolas", "DejaVu Sans Mono", "Noto Sans Mono", "Menlo"},
	"system-ui":    {"Segoe UI", "Roboto", "Ubuntu", "Cantarell", "Noto Sans", "DejaVu Sans", "Arial"},
	"cursive":      {"Comic Sans MS", "Segoe Script", "Apple Chancery"},
	"fantasy":      {"Impact", "Papyrus"},
	"helvetica":    {"Helvetica", "Arial", "Liberation Sans", "Arimo"},
	"times":        {"Times", "Times New Roman", "Liberation Serif"},
	"courier":      {"Courier", "Courier New", "Liberation Mono"},
	"ui-monospace": {"Consolas", "Courier New", "DejaVu Sans Mono"},
}

func init() {
	for _, alias := range []string{"-apple-system", "blinkmacsystemfont", "ui-sans-serif"} {
		generic[alias] = generic["system-ui"]
	}
	generic["ui-serif"] = generic["serif"]
	generic["helvetica neue"] = generic["helvetica"]
}

// Expand turns one CSS family name into concrete family names to try.
func Expand(family string) []string {
	key := strings.ToLower(family)
	if g, ok := generic[key]; ok {
		return g
	}
	return []string{family}
}

// Fallback families tried, in order, for characters the requested fonts
// lack (covering Armenian, Georgian, Cyrillic, Greek, Hebrew, symbols, CJK).
var FallbackFamilies = []string{
	"Arial", "Segoe UI", "Sylfaen", "DejaVu Sans", "Noto Sans", "Noto Sans Armenian",
	"Liberation Sans", "Segoe UI Symbol", "Segoe UI Emoji", "Arial Unicode MS", "Nirmala UI",
	"Microsoft YaHei", "Yu Gothic", "Malgun Gothic", "SimSun", "MS Gothic",
}
