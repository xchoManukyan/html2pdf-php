package render

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/xchoManukyan/html2pdf/converter/internal/css"
	"github.com/xchoManukyan/html2pdf/converter/internal/htmlparse"
	"github.com/xchoManukyan/html2pdf/converter/internal/pdf"
	"github.com/xchoManukyan/html2pdf/converter/internal/resource"
)

// picture is a loaded image: a raster image embedded in the PDF, or an
// SVG drawing rendered as vectors.
type picture struct {
	raster *pdf.Image
	svg    *svgDoc
	w, h   float64 // intrinsic size in CSS px
}

// fetch loads a resource referenced from the document.
func (e *engine) fetch(ref string) ([]byte, string, error) {
	abs := resource.Resolve(e.baseURL, ref)
	if e.loader == nil {
		return resource.DecodeDataURI(abs)
	}
	return e.loader.Fetch(abs)
}

func isReplaced(n *htmlparse.Node) bool { return n.Tag == "img" || n.Tag == "svg" }

// pictureFor returns the picture of an <img> or inline <svg> element.
func (e *engine) pictureFor(n *htmlparse.Node) *picture {
	if n.Tag == "svg" {
		if p, ok := e.inlineSVG[n]; ok {
			return p
		}
		var p *picture
		if doc := e.svgFromNode(n); doc != nil {
			p = &picture{svg: doc, w: doc.width, h: doc.height}
		}
		e.inlineSVG[n] = p
		return p
	}
	if p, ok := e.imgPictures[n]; ok {
		return p
	}
	var p *picture
	for _, c := range e.imageCandidates(n) {
		if p = e.loadImageDensity(c.url, c.density); p != nil {
			break
		}
	}
	e.imgPictures[n] = p
	return p
}

type imageCandidate struct {
	url     string
	density float64 // device pixels per CSS pixel (srcset "2x")
}

// supportedImageTypes are the <source type> values we can decode.
var supportedImageTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/jpg": true, "image/gif": true, "image/webp": true,
	"image/bmp": true, "image/svg+xml": true,
}

// imageCandidates lists the sources of an <img> in the order a browser
// would pick them: a matching <picture><source>, then srcset, then src.
// Later entries are fallbacks for when a download fails.
func (e *engine) imageCandidates(n *htmlparse.Node) []imageCandidate {
	var out []imageCandidate
	if p := n.Parent; p != nil && p.Tag == "picture" {
		for _, s := range p.Children {
			if s == n {
				break
			}
			if s.Type != htmlparse.ElementNode || s.Tag != "source" {
				continue
			}
			if t := strings.ToLower(strings.TrimSpace(s.Attr("type"))); t != "" && !supportedImageTypes[t] {
				continue
			}
			if m := s.Attr("media"); m != "" && !css.EvalMedia(m, e.env) {
				continue
			}
			if c, ok := bestSrcset(s.Attr("srcset")); ok {
				out = append(out, c)
				break
			}
		}
	}
	if c, ok := bestSrcset(n.Attr("srcset")); ok {
		out = append(out, c)
	}
	if src := strings.TrimSpace(n.Attr("src")); src != "" {
		out = append(out, imageCandidate{url: src, density: 1})
	}
	return out
}

// bestSrcset picks the highest-resolution srcset candidate: on paper there
// is no bandwidth to save and sharper is better. "800w" candidates are
// compared by width (shown at their natural size), "2x" by density.
func bestSrcset(srcset string) (imageCandidate, bool) {
	best, found := imageCandidate{}, false
	bestScore := -1.0
	for _, part := range splitSrcset(srcset) {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		c := imageCandidate{url: fields[0], density: 1}
		score := 1.0
		if len(fields) > 1 {
			d := strings.ToLower(fields[1])
			v, err := strconv.ParseFloat(d[:len(d)-1], 64)
			switch {
			case err != nil || v <= 0:
				continue
			case strings.HasSuffix(d, "x"):
				c.density, score = v, v
			case strings.HasSuffix(d, "w"):
				score = v
			}
		}
		if score > bestScore {
			best, bestScore, found = c, score, true
		}
	}
	return best, found
}

// splitSrcset splits on the commas between candidates; a comma inside a
// URL (as in data: URIs or "w_200,h_100" CDN paths) is kept when it is not
// followed by whitespace.
func splitSrcset(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\n' || s[i+1] == '\t') {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// loadImageDensity loads an image shown at 1/density of its pixel size.
func (e *engine) loadImageDensity(src string, density float64) *picture {
	p := e.loadImage(src)
	if p == nil || density == 1 || p.svg != nil {
		return p
	}
	cp := *p
	cp.w, cp.h = p.w/density, p.h/density
	return &cp
}

// loadImage loads an image by reference (data: URI, bundled or local file,
// or remote URL when allowed).
func (e *engine) loadImage(src string) *picture {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil
	}
	if p, ok := e.pictures[src]; ok {
		return p
	}
	var p *picture
	if data, typ, err := e.fetch(src); err == nil {
		var reason error
		if isSVGData(data, typ) {
			if doc := parseSVGBytes(data); doc != nil {
				p = &picture{svg: doc, w: doc.width, h: doc.height}
			} else {
				reason = errors.New("invalid SVG")
			}
		} else if im, err := e.doc.AddImage(data); err == nil {
			p = &picture{raster: im, w: float64(im.Width), h: float64(im.Height)}
		} else {
			reason = fmt.Errorf("not a supported image (%s)", firstNonEmpty(typ, "unknown type"))
		}
		// downloaded but unusable (an HTML error page, a corrupt or AVIF
		// file): report it like a missing file
		if reason != nil && e.loader != nil {
			e.loader.Reject(resource.Resolve(e.baseURL, src), reason)
		}
	}
	e.pictures[src] = p
	return p
}

func isSVGData(data []byte, typ string) bool {
	if strings.Contains(typ, "svg") {
		return true
	}
	head := bytes.TrimSpace(data[:min(len(data), 1024)])
	return bytes.HasPrefix(head, []byte("<svg")) || (bytes.HasPrefix(head, []byte("<?xml")) && bytes.Contains(data[:min(len(data), 4096)], []byte("<svg")))
}

// draw paints the picture into the box (x, y, w, h), clipped to clip.
func (pic *picture) draw(e *engine, p *pdf.Page, l pdf.Layer, clip *pdf.Rect, x, y, w, h, alpha float64) {
	if pic.raster != nil {
		if clip == nil {
			if l.Back {
				p.DrawImageTiles(l, pic.raster, pdf.Rect{X: x, Y: y, W: w, H: h}, [][4]float64{{x, y, w, h}}, alpha)
			} else {
				p.DrawImageAlpha(pic.raster, x, y, w, h, alpha)
			}
			return
		}
		p.DrawImageTiles(l, pic.raster, *clip, [][4]float64{{x, y, w, h}}, alpha)
		return
	}
	if pic.svg != nil {
		e.drawSVG(p, l, pic.svg, clip, x, y, w, h, alpha)
	}
}

// imageSize returns the box size of an image in points, fitted into maxW.
func (e *engine) imageSize(n *htmlparse.Node, st *style, maxW float64) (float64, float64) {
	img := e.pictureFor(n)
	nw, nh := 0.0, 0.0
	if img != nil {
		nw, nh = img.w*0.75, img.h*0.75
	}
	w, _ := e.resolveLength(st.widthExpr, st, maxW)
	h := e.boxHeight(st)
	if mx, ok := e.resolveLength(st.maxWidthExpr, st, maxW); ok && mx < maxW {
		maxW = mx
	}
	switch {
	case w > 0 && h > 0:
	case w > 0 && nw > 0:
		h = w * nh / nw
	case h > 0 && nh > 0:
		w = h * nw / nh
	case w == 0 && h == 0:
		w, h = nw, nh
	}
	if w > maxW && w > 0 {
		h = h * maxW / w
		w = maxW
	}
	return w, h
}

// layoutTallImage places an image taller than the page area in slices:
// on every page the whole image is drawn, shifted up by the part already
// shown, and clipped to the part that fits.
func (e *engine) layoutTallImage(img *picture, st *style, x, iw, ih float64) {
	// do not start with a sliver at the bottom of a page
	if e.y > e.top+0.5 && e.bottom-e.y < 72 {
		e.ensureSpace(e.bottom - e.top)
	}
	top := e.y // the image's top edge in this page's coordinates
	for {
		end := top + ih
		sliceH := min(e.bottom, end) - e.y
		if !e.dry && st.visible && sliceH > 0 {
			clip := pdf.Rect{X: x, Y: e.y, W: iw, H: sliceH}
			img.draw(e, e.page(), pdf.Layer{}, &clip, x, top, iw, ih, st.alpha)
			e.addLink(e.page(), st.link, x, e.y, iw, sliceH)
		}
		if end <= e.bottom+0.01 || e.dry || e.noBreak {
			e.y = end
			break
		}
		shown := e.bottom - top
		e.newPage()
		top = e.y - shown
	}
	e.atTop = false
}

func (e *engine) layoutImage(n *htmlparse.Node, st *style, x, w float64) {
	img := e.pictureFor(n)
	if img == nil {
		if alt := strings.TrimSpace(n.Attr("alt")); alt != "" {
			var items []item
			e.collectText("["+alt+"]", st, &items)
			e.flush(items, st, x, w)
		}
		return
	}
	avail := w - st.margin[1] - st.margin[3]
	iw, ih := e.imageSize(n, st, avail-st.extrasH())
	if maxH := e.bottom - e.top - st.extrasV(); ih > maxH {
		if !e.noBreak && iw > 0 {
			// taller than a page: continue it across pages at full
			// width, as browsers print long screenshots
			e.addMargin(st.margin[0])
			e.applyMargin()
			ix := x + st.margin[3]
			switch st.align {
			case "center":
				ix += (avail - iw - st.extrasH()) / 2
			case "right":
				ix += avail - iw - st.extrasH()
			}
			e.layoutTallImage(img, st, ix+st.bw(3)+st.padding[3], iw, ih)
			e.addMargin(st.margin[2])
			return
		}
		// cannot break here (flex item, table cell...): scale it down
		iw, ih = iw*maxH/ih, maxH
	}
	if iw <= 0 || ih <= 0 {
		return
	}
	// from here on iw/ih are border-box sizes
	iw, ih = iw+st.extrasH(), ih+st.extrasV()
	e.addMargin(st.margin[0])
	e.applyMargin()
	e.ensureSpace(ih)
	ix := x + st.margin[3]
	switch st.align {
	case "center":
		ix += (avail - iw) / 2
	case "right":
		ix += avail - iw
	}
	e.drawImageBox(img, st, ix, e.y, iw-st.extrasH(), ih-st.extrasV())
	if !e.dry {
		e.addLink(e.page(), st.link, ix, e.y, iw, ih)
	}
	e.y += ih
	e.addMargin(st.margin[2])
}

var fontFaceRe = regexp.MustCompile(`(?is)@font-face\s*\{[^}]*\}`)

// imageRefs lists the absolute URLs of the images a document may show:
// <img>/<picture> sources, url() in style attributes and stylesheets.
// @font-face sources are left out (fonts download when text uses them).
func (e *engine) imageRefs(root *htmlparse.Node, sheets []string) []string {
	var refs []string
	add := func(base, ref string) {
		if abs := resource.Resolve(base, ref); abs != "" {
			refs = append(refs, abs)
		}
	}
	var walk func(n *htmlparse.Node)
	walk = func(n *htmlparse.Node) {
		if n.Type == htmlparse.ElementNode {
			if n.Tag == "img" {
				if c := e.imageCandidates(n); len(c) > 0 {
					add(e.baseURL, c[0].url)
				}
			}
			if s := n.Attr("style"); strings.Contains(s, "url(") {
				for _, m := range urlRe.FindAllStringSubmatch(s, -1) {
					add(e.baseURL, firstNonEmpty(m[1], m[2], m[3]))
				}
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	for _, css := range sheets {
		// stylesheet URLs were made absolute when the sheet was loaded
		for _, m := range urlRe.FindAllStringSubmatch(fontFaceRe.ReplaceAllString(css, ""), -1) {
			add(e.baseURL, firstNonEmpty(m[1], m[2], m[3]))
		}
	}
	return refs
}
