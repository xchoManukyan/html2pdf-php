package pdf

import (
	"bytes"
	"compress/zlib"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"io"
	"regexp"
	"strconv"
	"testing"
)

// TestEncryptionRoundTrip re-derives the key from the written /O /P /ID
// entries (spec algorithms 1 and 2) and decrypts a content stream.
func TestEncryptionRoundTrip(t *testing.T) {
	d := New(595, 842)
	d.Encrypt = &Encryption{UserPassword: "pw", AllowPrint: true}
	p := d.AddPage()
	p.Text(50, 100, TextStyle{Font: d.Std(Helvetica), Size: 12}, "Hello secret")
	out := d.Bytes()

	o := regexp.MustCompile(`/O <([0-9a-f]+)>`).FindSubmatch(out)
	pm := regexp.MustCompile(`/P (-?\d+)`).FindSubmatch(out)
	id := regexp.MustCompile(`/ID \[<([0-9a-f]+)>`).FindSubmatch(out)
	if o == nil || pm == nil || id == nil {
		t.Fatal("missing /O, /P or /ID")
	}
	ob, _ := hex.DecodeString(string(o[1]))
	idb, _ := hex.DecodeString(string(id[1]))
	pv, _ := strconv.ParseInt(string(pm[1]), 10, 64)

	h := md5.New()
	h.Write(padPassword("pw"))
	h.Write(ob)
	var pb [4]byte
	binary.LittleEndian.PutUint32(pb[:], uint32(int32(pv)))
	h.Write(pb[:])
	h.Write(idb)
	key := h.Sum(nil)
	for i := 0; i < 50; i++ {
		s := md5.Sum(key[:16])
		key = s[:]
	}
	key = key[:16]

	// find "<n> 0 obj\n<< /Filter /FlateDecode /Length L >>\nstream\n"
	re := regexp.MustCompile(`(?s)(\d+) 0 obj\n<< /Filter /FlateDecode /Length (\d+) >>\nstream\n`)
	found := false
	for _, m := range re.FindAllSubmatchIndex(out, -1) {
		num, _ := strconv.Atoi(string(out[m[2]:m[3]]))
		n, _ := strconv.Atoi(string(out[m[4]:m[5]]))
		k := append(append([]byte(nil), key...), byte(num), byte(num>>8), byte(num>>16), 0, 0)
		ok := md5.Sum(k)
		plain := rc4Crypt(ok[:16], out[m[1]:m[1]+n])
		zr, err := zlib.NewReader(bytes.NewReader(plain))
		if err != nil {
			t.Fatalf("object %d does not decrypt to a zlib stream: %v", num, err)
		}
		data, _ := io.ReadAll(zr)
		if bytes.Contains(data, []byte("Hello secret")) {
			found = true
		}
	}
	if !found {
		t.Error("decrypted content stream with the text not found")
	}
}
