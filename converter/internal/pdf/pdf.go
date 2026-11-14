// Package pdf writes PDF 1.4 files using only the Go standard library.
//
// Coordinates passed to the drawing methods use a top-left origin with y
// growing downwards (like HTML); they are flipped to PDF space internally.
// Units are points (1/72 inch).
package pdf

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xchoManukyan/html2pdf/converter/internal/ttf"
)

// Color is an RGB color with components in 0..1. A is the opacity; the
// zero value means fully opaque, so Color{R, G, B} literals stay opaque.
type Color struct{ R, G, B, A float64 }

// Alpha returns the effective opacity of the color.
func (c Color) Alpha() float64 {
	if c.A <= 0 || c.A >= 1 {
		return 1
	}
	return c.A
}

// Common page sizes in points (portrait).
var PageSizes = map[string][2]float64{
	"A3":     {841.89, 1190.55},
	"A4":     {595.28, 841.89},
	"A5":     {419.53, 595.28},
	"LETTER": {612, 792},
	"LEGAL":  {612, 1008},
}

// Document is a PDF under construction.
type Document struct {
	Width, Height float64
	Title         string
	Author        string
	Subject       string
	Keywords      string
	Creator       string
	Encrypt       *Encryption // password protection and permissions (nil: none)
	// Scale shrinks everything uniformly when writing: pages are laid out
	// at Width x Height and printed at Width*Scale x Height*Scale. 0 or 1:
	// no scaling (used to fit a wide CSS viewport onto the paper).
	Scale     float64
	dests     map[string]dest
	pages     []*Page
	images    []*Image
	fonts     []*Font
	stdFonts  map[StdFont]*Font
	ttFonts   map[*ttf.Font]*Font
	gstates   map[int]int // alpha (per mille) -> ExtGState number
	shadings  []*Shading
	forms     []*Form
	softMasks []softMask
}

// Page holds the drawing operations of one page. Background operations are
// written before foreground ones so that fills never hide text.
type Page struct {
	doc        *Document
	background []bgOp
	foreground bytes.Buffer
	links      []link
}

type bgOp struct {
	z  int
	op string
}

type link struct {
	x1, y1, x2, y2 float64
	uri            string
	dest           string // internal destination name (for "#id" links)
}

// New creates an empty document with the given page size in points.
func New(width, height float64) *Document {
	return &Document{Width: width, Height: height}
}

func (d *Document) scale() float64 {
	if d.Scale <= 0 {
		return 1
	}
	return d.Scale
}

// AddPage appends a new blank page.
func (d *Document) AddPage() *Page {
	p := &Page{doc: d}
	d.pages = append(d.pages, p)
	return p
}

// PageCount returns the number of pages.
func (d *Document) PageCount() int { return len(d.pages) }

// Page returns page i (0-based).
func (d *Document) Page(i int) *Page { return d.pages[i] }

func num(v float64) string {
	s := strconv.FormatFloat(v, 'f', 3, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")
	if s == "-0" || s == "" {
		return "0"
	}
	return s
}

func colorOp(c Color, op string) string {
	return num(c.R) + " " + num(c.G) + " " + num(c.B) + " " + op
}

// escape builds a PDF literal string from WinAnsi bytes.
func escape(b []byte) string {
	var sb strings.Builder
	sb.WriteByte('(')
	for _, c := range b {
		switch {
		case c == '(' || c == ')' || c == '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case c < 32 || c > 126:
			fmt.Fprintf(&sb, "\\%03o", c)
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteByte(')')
	return sb.String()
}

// Text draws s with its baseline at y.
func (p *Page) Text(x, baseline float64, t TextStyle, s string) {
	if s == "" || t.Font == nil {
		return
	}
	// Text state (Tc, Tr, line width) is part of the graphics state and
	// would leak into later text, so every run gets its own q/Q pair when
	// it changes any of it.
	isolated := t.FakeBold || t.CharSpacing != 0 || t.Color.Alpha() < 1
	var b strings.Builder
	if isolated {
		b.WriteString("q " + p.gs(t.Color.Alpha()))
	}
	fmt.Fprintf(&b, "BT /F%d %s Tf %s ", t.Font.id, num(t.Size), colorOp(t.Color, "rg"))
	if t.FakeBold {
		fmt.Fprintf(&b, "2 Tr %s %s w ", colorOp(t.Color, "RG"), num(t.Size*0.03))
	}
	if t.CharSpacing != 0 {
		fmt.Fprintf(&b, "%s Tc ", num(t.CharSpacing))
	}
	y := p.doc.Height - baseline
	if t.FakeItalic {
		fmt.Fprintf(&b, "1 0 0.21 1 %s %s Tm ", num(x), num(y))
	} else {
		fmt.Fprintf(&b, "%s %s Td ", num(x), num(y))
	}
	fmt.Fprintf(&b, "%s ET", t.Font.show(s))
	if isolated {
		b.WriteString(" Q")
	}
	b.WriteByte('\n')
	p.foreground.WriteString(b.String())
}

// Line strokes a straight line.
func (p *Page) Line(x1, y1, x2, y2, width float64, c Color) {
	if width <= 0 {
		return
	}
	h := p.doc.Height
	fmt.Fprintf(&p.foreground, "q %s%s %s w %s %s m %s %s l S Q\n",
		p.gs(c.Alpha()), colorOp(c, "RG"), num(width), num(x1), num(h-y1), num(x2), num(h-y2))
}

// FillRect fills a rectangle on the foreground layer.
func (p *Page) FillRect(x, y, w, h float64, c Color) {
	p.foreground.WriteString(p.rect(x, y, w, h, c))
}

// Background fills a rectangle behind all text and lines of the page.
// Fills with a lower z are painted first, so nested boxes (z = nesting
// depth) always show on top of their parents.
func (p *Page) Background(x, y, w, h float64, c Color, z int) {
	if op := p.rect(x, y, w, h, c); op != "" {
		p.background = append(p.background, bgOp{z, op})
	}
}

func (p *Page) rect(x, y, w, h float64, c Color) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	return fmt.Sprintf("q %s%s %s %s %s %s re f Q\n",
		p.gs(c.Alpha()), colorOp(c, "rg"), num(x), num(p.doc.Height-y-h), num(w), num(h))
}

// Circle draws a filled or stroked circle centred at (cx, cy).
func (p *Page) Circle(cx, cy, r float64, c Color, fill bool) {
	const k = 0.5523 // Bezier control-point factor for a quarter circle
	y := p.doc.Height - cy
	op := "f"
	col := p.gs(c.Alpha()) + colorOp(c, "rg")
	if !fill {
		op = "S"
		col = p.gs(c.Alpha()) + colorOp(c, "RG") + " " + num(r*0.35) + " w"
	}
	fmt.Fprintf(&p.foreground, "q %s %s %s m %s %s %s %s %s %s c %s %s %s %s %s %s c %s %s %s %s %s %s c %s %s %s %s %s %s c %s Q\n",
		col, num(cx+r), num(y),
		num(cx+r), num(y+k*r), num(cx+k*r), num(y+r), num(cx), num(y+r),
		num(cx-k*r), num(y+r), num(cx-r), num(y+k*r), num(cx-r), num(y),
		num(cx-r), num(y-k*r), num(cx-k*r), num(y-r), num(cx), num(y-r),
		num(cx+k*r), num(y-r), num(cx+r), num(y-k*r), num(cx+r), num(y),
		op)
}

// dest is a named position in the document (an element id).
type dest struct {
	page int
	y    float64
}

// AddDest registers a named destination at height y (top-left
// coordinates) on page index. The first registration wins.
func (d *Document) AddDest(name string, page int, y float64) {
	if d.dests == nil {
		d.dests = map[string]dest{}
	}
	if _, ok := d.dests[name]; !ok {
		d.dests[name] = dest{page, y}
	}
}

// LinkDest makes a rectangle jump to a named destination.
func (p *Page) LinkDest(x, y, w, h float64, name string) {
	ph := p.doc.Height
	p.links = append(p.links, link{x1: x, y1: ph - y - h, x2: x + w, y2: ph - y, dest: name})
}

// Link makes a rectangle clickable, opening uri.
func (p *Page) Link(x, y, w, h float64, uri string) {
	ph := p.doc.Height
	p.links = append(p.links, link{x1: x, y1: ph - y - h, x2: x + w, y2: ph - y, uri: uri})
}

// DrawImage paints an image into the box (x, y, w, h).
func (p *Page) DrawImage(img *Image, x, y, w, h float64) {
	p.DrawImageAlpha(img, x, y, w, h, 1)
}

// DrawImageAlpha paints an image with the given opacity.
func (p *Page) DrawImageAlpha(img *Image, x, y, w, h, alpha float64) {
	fmt.Fprintf(&p.foreground, "q %s%s 0 0 %s %s %s cm /Im%d Do Q\n",
		p.gs(alpha), num(w), num(h), num(x), num(p.doc.Height-y-h), img.id)
}

// gs returns the operator selecting an ExtGState with the given fill and
// stroke opacity ("" for opaque).
func (p *Page) gs(alpha float64) string {
	if alpha >= 0.999 {
		return ""
	}
	alpha = max(alpha, 0)
	key := int(alpha*1000 + 0.5)
	d := p.doc
	if d.gstates == nil {
		d.gstates = map[int]int{}
	}
	id, ok := d.gstates[key]
	if !ok {
		id = len(d.gstates) + 1
		d.gstates[key] = id
	}
	return fmt.Sprintf("/GS%d gs ", id)
}

func deflate(data []byte) []byte {
	var buf bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	zw.Write(data)
	zw.Close()
	return buf.Bytes()
}

type writer struct {
	w       *bytes.Buffer
	offsets []int
	crypt   *cryptor
}

func (w *writer) begin(id int) {
	for len(w.offsets) <= id {
		w.offsets = append(w.offsets, 0)
	}
	w.offsets[id] = w.w.Len()
	fmt.Fprintf(w.w, "%d 0 obj\n", id)
}

func (w *writer) object(id int, body string) {
	w.begin(id)
	w.w.WriteString(body)
	w.w.WriteString("\nendobj\n")
}

func (w *writer) stream(id int, dict string, data []byte) {
	w.begin(id)
	if w.crypt != nil {
		data = w.crypt.encrypt(id, data) // strings and streams are encrypted per object
	}
	fmt.Fprintf(w.w, "<< %s /Length %d >>\nstream\n", dict, len(data))
	w.w.Write(data)
	w.w.WriteString("\nendstream\nendobj\n")
}

// WriteTo serialises the document.
func (d *Document) WriteTo(out io.Writer) (int64, error) {
	if len(d.pages) == 0 {
		d.AddPage()
	}
	buf := &bytes.Buffer{}
	w := &writer{w: buf}
	if d.Encrypt != nil {
		w.crypt = newCryptor(d.Encrypt)
	}
	buf.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")

	// Object numbering: 1 catalog, 2 page tree, 3 info, fonts, images, pages.
	const catalogID, pagesID, infoID = 1, 2, 3
	next := 4
	var fonts []*Font
	for _, f := range d.fonts {
		if f.refd {
			f.objID = next
			next += f.objectCount()
			fonts = append(fonts, f)
		}
	}
	for _, img := range d.images {
		img.objID = next
		next++
		if img.smask != nil {
			img.smaskID = next
			next++
		}
	}
	for _, sh := range d.shadings {
		sh.objID = next
		next++
	}
	for _, f := range d.forms {
		f.objID = next
		next++
	}
	pageIDs := make([]int, len(d.pages))
	contentIDs := make([]int, len(d.pages))
	for i := range d.pages {
		pageIDs[i] = next
		contentIDs[i] = next + 1
		next += 2
	}
	encryptID := 0
	if w.crypt != nil {
		encryptID = next
		next++
	}

	w.object(catalogID, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesID))
	var kids strings.Builder
	for _, id := range pageIDs {
		fmt.Fprintf(&kids, "%d 0 R ", id)
	}
	w.object(pagesID, fmt.Sprintf("<< /Type /Pages /Kids [ %s] /Count %d /MediaBox [0 0 %s %s] >>",
		kids.String(), len(d.pages), num(d.Width*d.scale()), num(d.Height*d.scale())))
	info := "<< /Producer " + w.str(infoID, "html2pdf - Go standard library")
	for _, kv := range [][2]string{{"Title", d.Title}, {"Author", d.Author}, {"Subject", d.Subject}, {"Keywords", d.Keywords}, {"Creator", d.Creator}} {
		if kv[1] != "" {
			info += " /" + kv[0] + " " + w.str(infoID, kv[1])
		}
	}
	info += " /CreationDate " + w.str(infoID, time.Now().UTC().Format("D:20060102150405Z"))
	w.object(infoID, info+" >>")
	if w.crypt != nil {
		// the encryption dictionary itself is never encrypted
		w.object(encryptID, w.crypt.dict())
	}

	for _, f := range fonts {
		f.write(w)
	}
	for _, img := range d.images {
		img.write(w)
	}
	for _, sh := range d.shadings {
		w.object(sh.objID, sh.dict())
	}

	var res strings.Builder
	res.WriteString("<< /ProcSet [/PDF /Text /ImageB /ImageC] /Font <<")
	for _, f := range fonts {
		fmt.Fprintf(&res, " /F%d %d 0 R", f.id, f.objID)
	}
	res.WriteString(" >>")
	if len(d.images)+len(d.forms) > 0 {
		res.WriteString(" /XObject <<")
		for _, img := range d.images {
			fmt.Fprintf(&res, " /Im%d %d 0 R", img.id, img.objID)
		}
		for _, f := range d.forms {
			fmt.Fprintf(&res, " /Fm%d %d 0 R", f.id, f.objID)
		}
		res.WriteString(" >>")
	}
	if len(d.gstates)+len(d.softMasks) > 0 {
		res.WriteString(" /ExtGState <<")
		for _, sm := range d.softMasks {
			kind := "/Luminosity"
			if sm.alpha {
				kind = "/Alpha"
			}
			fmt.Fprintf(&res, " /SM%d << /Type /ExtGState /SMask << /Type /Mask /S %s /G %d 0 R >> >>", sm.id, kind, sm.form.objID)
		}
		keys := make([]int, 0, len(d.gstates))
		for k := range d.gstates {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		for _, key := range keys {
			id := d.gstates[key]
			a := num(float64(key) / 1000)
			fmt.Fprintf(&res, " /GS%d << /Type /ExtGState /ca %s /CA %s >>", id, a, a)
		}
		res.WriteString(" >>")
	}
	if len(d.shadings) > 0 {
		res.WriteString(" /Shading <<")
		for _, sh := range d.shadings {
			fmt.Fprintf(&res, " /Sh%d %d 0 R", sh.id, sh.objID)
		}
		res.WriteString(" >>")
	}
	res.WriteString(" >>")
	resources := res.String()
	for _, f := range d.forms {
		dict := fmt.Sprintf("/Type /XObject /Subtype /Form /BBox [%s %s %s %s] /Resources %s /Filter /FlateDecode",
			num(f.bbox[0]), num(f.bbox[1]), num(f.bbox[2]), num(f.bbox[3]), resources)
		if f.group {
			dict += " /Group << /S /Transparency /CS /DeviceRGB >>"
		}
		w.stream(f.objID, dict, deflate([]byte(f.ops)))
	}

	for i, p := range d.pages {
		var annots string
		if len(p.links) > 0 {
			var a strings.Builder
			a.WriteString(" /Annots [")
			for _, l := range p.links {
				s := d.scale()
				rect := fmt.Sprintf("/Rect [%s %s %s %s] /Border [0 0 0]", num(l.x1*s), num(l.y1*s), num(l.x2*s), num(l.y2*s))
				if l.dest != "" {
					dst, ok := d.dests[l.dest]
					if !ok || dst.page >= len(pageIDs) {
						continue
					}
					fmt.Fprintf(&a, " << /Type /Annot /Subtype /Link %s /Dest [%d 0 R /XYZ 0 %s null] >>", rect, pageIDs[dst.page], num((d.Height-dst.y)*s))
					continue
				}
				fmt.Fprintf(&a, " << /Type /Annot /Subtype /Link %s /A << /S /URI /URI %s >> >>", rect, w.str(pageIDs[i], l.uri))
			}
			a.WriteString(" ]")
			annots = a.String()
		}
		w.object(pageIDs[i], fmt.Sprintf("<< /Type /Page /Parent %d 0 R /Resources %s /Contents %d 0 R%s >>",
			pagesID, resources, contentIDs[i], annots))
		sort.SliceStable(p.background, func(a, b int) bool { return p.background[a].z < p.background[b].z })
		var content bytes.Buffer
		if s := d.scale(); s != 1 {
			fmt.Fprintf(&content, "q %s 0 0 %s 0 0 cm\n", num(s), num(s))
		}
		for _, op := range p.background {
			content.WriteString(op.op)
		}
		content.Write(p.foreground.Bytes())
		if d.scale() != 1 {
			content.WriteString("\nQ\n")
		}
		w.stream(contentIDs[i], "/Filter /FlateDecode", deflate(content.Bytes()))
	}

	xref := buf.Len()
	fmt.Fprintf(buf, "xref\n0 %d\n0000000000 65535 f \n", next)
	for id := 1; id < next; id++ {
		fmt.Fprintf(buf, "%010d 00000 n \n", w.offsets[id])
	}
	extra := ""
	if w.crypt != nil {
		id := hex.EncodeToString(w.crypt.id)
		extra = fmt.Sprintf(" /Encrypt %d 0 R /ID [<%s> <%s>]", encryptID, id, id)
	}
	fmt.Fprintf(buf, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R%s >>\nstartxref\n%d\n%%%%EOF\n",
		next, catalogID, infoID, extra, xref)

	n, err := out.Write(buf.Bytes())
	return int64(n), err
}

// Bytes returns the finished PDF.
func (d *Document) Bytes() []byte {
	var b bytes.Buffer
	d.WriteTo(&b)
	return b.Bytes()
}
