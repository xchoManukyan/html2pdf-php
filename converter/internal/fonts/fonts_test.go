package fonts

import (
	"testing"
	"time"
)

func TestSystemScan(t *testing.T) {
	start := time.Now()
	db := System()
	t.Logf("scanned %d faces in %s", len(db.all), time.Since(start))
	if db.Empty() {
		t.Skip("no system fonts")
	}
	for _, q := range []struct {
		fam    string
		w      int
		italic bool
	}{{"Arial", 400, false}, {"Arial", 700, false}, {"Times New Roman", 700, true}, {"Segoe UI", 600, false}, {"Courier New", 400, false}} {
		f := db.Match(q.fam, q.w, q.italic)
		if f == nil {
			t.Errorf("%v: no match", q)
			continue
		}
		t.Logf("%v -> %s #%d (w%d i%v)", q, f.Path, f.Index, f.Weight, f.Italic)
	}
	for _, r := range "ԱЖ漢ა" {
		f := db.Fallback(r)
		if f != nil {
			t.Logf("fallback %c -> %s", r, f.Path)
		} else {
			t.Logf("fallback %c -> none", r)
		}
	}
}
