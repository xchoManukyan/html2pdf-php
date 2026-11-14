package pdf

import (
	"fmt"
	"strings"
)

// Low-level hooks for vector content generated elsewhere (SVG): raw
// operators, graphics states, shadings, images and text in an arbitrary
// coordinate system.

// Raw appends content-stream operators to a layer.
func (p *Page) Raw(l Layer, ops string) { p.emit(l, ops) }

// GS returns the operator selecting the given opacity ("" when opaque).
func (p *Page) GS(alpha float64) string { return p.gs(alpha) }

// Num formats a number for a content stream.
func Num(v float64) string { return num(v) }

// AddRawShading creates a gradient whose coordinates are used as given
// (in the user space current when it is painted). Axial: x0 y0 x1 y1;
// radial: x0 y0 r0 x1 y1 r1.
func (d *Document) AddRawShading(radial bool, coords []float64, stops []Stop) *Shading {
	sh := &Shading{id: len(d.shadings) + 1, radial: radial, coords: append([]float64(nil), coords...), stops: stops}
	d.shadings = append(d.shadings, sh)
	return sh
}

// Name returns the resource name of a shading ("/Sh3").
func (sh *Shading) Name() string { return fmt.Sprintf("/Sh%d", sh.id) }

// Name returns the resource name of an image ("/Im2").
func (img *Image) Name() string { return fmt.Sprintf("/Im%d", img.id) }

// TextOps returns operators drawing s at (x, y) in the current user space,
// which is assumed to have y pointing down (the text matrix flips it back
// upright).
func (p *Page) TextOps(t TextStyle, x, y float64, s string) string {
	if s == "" || t.Font == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("q ")
	b.WriteString(p.gs(t.Color.Alpha()))
	fmt.Fprintf(&b, "BT /F%d %s Tf %s ", t.Font.id, num(t.Size), colorOp(t.Color, "rg"))
	if t.FakeBold {
		fmt.Fprintf(&b, "2 Tr %s %s w ", colorOp(t.Color, "RG"), num(t.Size*0.03))
	}
	skew := "0"
	if t.FakeItalic {
		skew = "0.21"
	}
	fmt.Fprintf(&b, "1 0 %s -1 %s %s Tm %s ET Q\n", skew, num(x), num(y), t.Font.show(s))
	return b.String()
}

// Form is a reusable piece of content (PDF form XObject), used for soft
// masks.
type Form struct {
	id, objID int
	ops       string
	bbox      [4]float64
	group     bool
}

type softMask struct {
	id    int
	form  *Form
	alpha bool
}

// AddForm stores content operators as a form XObject. The content uses the
// coordinate system that is current where the form is used.
func (d *Document) AddForm(ops string, bbox [4]float64, group bool) *Form {
	f := &Form{id: len(d.forms) + 1, ops: ops, bbox: bbox, group: group}
	d.forms = append(d.forms, f)
	return f
}

// Name returns the resource name of a form ("/Fm1").
func (f *Form) Name() string { return fmt.Sprintf("/Fm%d", f.id) }

// SoftMask returns the operator that applies form f as a soft mask:
// luminosity-based, or alpha-based when alpha is true. The mask content is
// drawn in the coordinate system current when the operator runs.
func (d *Document) SoftMask(f *Form, alpha bool) string {
	sm := softMask{id: len(d.softMasks) + 1, form: f, alpha: alpha}
	d.softMasks = append(d.softMasks, sm)
	return fmt.Sprintf("/SM%d gs ", sm.id)
}
