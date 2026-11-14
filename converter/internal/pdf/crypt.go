package pdf

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/rc4"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf16"
)

// Encryption protects the document with the PDF Standard Security
// Handler (revision 3, 128-bit RC4) — the scheme dompdf/CPDF uses.
type Encryption struct {
	UserPassword  string // needed to open the document ("" = opens without a password)
	OwnerPassword string // full access; defaults to a random value
	AllowPrint    bool
	AllowCopy     bool
	AllowModify   bool
	AllowAnnotate bool
}

var padding = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
	0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80, 0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

func padPassword(p string) []byte {
	b := []byte(p)
	if len(b) > 32 {
		b = b[:32]
	}
	return append(b, padding[:32-len(b)]...)
}

func rc4Crypt(key, data []byte) []byte {
	c, _ := rc4.NewCipher(key)
	out := make([]byte, len(data))
	c.XORKeyStream(out, data)
	return out
}

type cryptor struct {
	key  []byte
	o, u []byte
	p    int32
	id   []byte
}

func newCryptor(enc *Encryption) *cryptor {
	owner := enc.OwnerPassword
	if owner == "" {
		r := make([]byte, 16)
		rand.Read(r)
		owner = hex.EncodeToString(r)
	}
	// permissions: bits 7-8 and 13-32 must be 1, bits 1-2 must be 0
	p := uint32(0xFFFFF0C0)
	if enc.AllowPrint {
		p |= 4 | 2048
	}
	if enc.AllowModify {
		p |= 8 | 1024
	}
	if enc.AllowCopy {
		p |= 16 | 512
	}
	if enc.AllowAnnotate {
		p |= 32 | 256
	}
	c := &cryptor{p: int32(p), id: make([]byte, 16)}
	rand.Read(c.id)

	// Algorithm 3: the O entry
	h := md5.Sum(padPassword(owner))
	for i := 0; i < 50; i++ {
		h = md5.Sum(h[:])
	}
	okey := h[:16]
	o := rc4Crypt(okey, padPassword(enc.UserPassword))
	for i := 1; i <= 19; i++ {
		k := make([]byte, len(okey))
		for j := range okey {
			k[j] = okey[j] ^ byte(i)
		}
		o = rc4Crypt(k, o)
	}
	c.o = o

	// Algorithm 2: the file encryption key
	hh := md5.New()
	hh.Write(padPassword(enc.UserPassword))
	hh.Write(c.o)
	var pb [4]byte
	binary.LittleEndian.PutUint32(pb[:], p)
	hh.Write(pb[:])
	hh.Write(c.id)
	key := hh.Sum(nil)
	for i := 0; i < 50; i++ {
		s := md5.Sum(key[:16])
		key = s[:]
	}
	c.key = key[:16]

	// Algorithm 5: the U entry
	uh := md5.New()
	uh.Write(padding)
	uh.Write(c.id)
	u := rc4Crypt(c.key, uh.Sum(nil))
	for i := 1; i <= 19; i++ {
		k := make([]byte, len(c.key))
		for j := range c.key {
			k[j] = c.key[j] ^ byte(i)
		}
		u = rc4Crypt(k, u)
	}
	c.u = append(u, make([]byte, 16)...)
	return c
}

// objectKey derives the RC4 key of one object (Algorithm 1).
func (c *cryptor) objectKey(id int) []byte {
	b := append([]byte(nil), c.key...)
	b = append(b, byte(id), byte(id>>8), byte(id>>16), 0, 0)
	s := md5.Sum(b)
	return s[:16]
}

func (c *cryptor) encrypt(id int, data []byte) []byte {
	return rc4Crypt(c.objectKey(id), data)
}

func (c *cryptor) dict() string {
	return fmt.Sprintf("<< /Filter /Standard /V 2 /R 3 /Length 128 /P %d /O <%s> /U <%s> >>",
		c.p, hex.EncodeToString(c.o), hex.EncodeToString(c.u))
}

// textBytes encodes a text string as PDFDocEncoding-compatible ASCII or
// UTF-16BE with a byte-order mark.
func textBytes(s string) []byte {
	ascii := true
	for _, r := range s {
		if r > 126 || r < 32 {
			ascii = false
			break
		}
	}
	if ascii {
		return []byte(s)
	}
	b := []byte{0xFE, 0xFF}
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u>>8), byte(u))
	}
	return b
}

// str writes a string object belonging to object id, encrypted when the
// document is protected.
func (w *writer) str(id int, s string) string {
	if w.crypt == nil {
		if strings.IndexFunc(s, func(r rune) bool { return r > 126 || r < 32 }) >= 0 {
			return pdfText(s)
		}
		return escape([]byte(s))
	}
	return "<" + hex.EncodeToString(w.crypt.encrypt(id, textBytes(s))) + ">"
}
