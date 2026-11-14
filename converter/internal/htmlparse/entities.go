package htmlparse

import (
	"strconv"
	"strings"
)

var entities = map[string]rune{
	"quot": '"', "amp": '&', "lt": '<', "gt": '>', "apos": '\'',
	"OElig": 338, "oelig": 339, "Scaron": 352, "scaron": 353, "Yuml": 376,
	"fnof": 402, "circ": 710, "tilde": 732, "ensp": 8194, "emsp": 8195,
	"thinsp": 8201, "zwnj": 8204, "zwj": 8205, "ndash": 8211, "mdash": 8212,
	"lsquo": 8216, "rsquo": 8217, "sbquo": 8218, "ldquo": 8220, "rdquo": 8221,
	"bdquo": 8222, "dagger": 8224, "Dagger": 8225, "bull": 8226, "hellip": 8230,
	"permil": 8240, "prime": 8242, "Prime": 8243, "lsaquo": 8249, "rsaquo": 8250,
	"euro": 8364, "trade": 8482, "larr": 8592, "rarr": 8594, "minus": 8722,
}

// Latin-1 entity names in code point order, starting at U+00A0.
const latin1Names = "nbsp iexcl cent pound curren yen brvbar sect uml copy ordf laquo not shy reg macr " +
	"deg plusmn sup2 sup3 acute micro para middot cedil sup1 ordm raquo frac14 frac12 frac34 iquest " +
	"Agrave Aacute Acirc Atilde Auml Aring AElig Ccedil Egrave Eacute Ecirc Euml Igrave Iacute Icirc Iuml " +
	"ETH Ntilde Ograve Oacute Ocirc Otilde Ouml times Oslash Ugrave Uacute Ucirc Uuml Yacute THORN szlig " +
	"agrave aacute acirc atilde auml aring aelig ccedil egrave eacute ecirc euml igrave iacute icirc iuml " +
	"eth ntilde ograve oacute ocirc otilde ouml divide oslash ugrave uacute ucirc uuml yacute thorn yuml"

func init() {
	for i, name := range strings.Fields(latin1Names) {
		entities[name] = rune(0xA0 + i)
	}
}

// DecodeEntities replaces character references (&amp; &#169; &#xA9;) with
// the characters they stand for. Unknown references are left untouched.
func DecodeEntities(s string) string {
	if strings.IndexByte(s, '&') < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c != '&' {
			b.WriteByte(c)
			i++
			continue
		}
		semi := strings.IndexByte(s[i:], ';')
		if semi < 2 || semi > 32 {
			b.WriteByte(c)
			i++
			continue
		}
		ref := s[i+1 : i+semi]
		var r rune = -1
		if ref[0] == '#' {
			var v int64
			var err error
			if len(ref) > 1 && (ref[1] == 'x' || ref[1] == 'X') {
				v, err = strconv.ParseInt(ref[2:], 16, 32)
			} else {
				v, err = strconv.ParseInt(ref[1:], 10, 32)
			}
			if err == nil && v > 0 && v <= 0x10FFFF {
				r = rune(v)
			}
		} else if v, ok := entities[ref]; ok {
			r = v
		}
		if r < 0 {
			b.WriteByte(c)
			i++
			continue
		}
		if r != 0xAD { // soft hyphen is dropped
			b.WriteRune(r)
		}
		i += semi + 1
	}
	return b.String()
}
