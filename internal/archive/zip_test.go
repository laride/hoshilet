package archive

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, names []string, contents []byte, mode os.FileMode) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, name := range names {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(mode)
		f, e := w.CreateHeader(h)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.Write(contents); e != nil {
			t.Fatal(e)
		}
	}
	if e := w.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func extractBytes(t *testing.T, b []byte, limit int64) (Result, error) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "input.zip")
	if e := os.WriteFile(src, b, 0600); e != nil {
		t.Fatal(e)
	}
	return Extract(src, filepath.Join(dir, "out"), limit)
}
func TestValid(t *testing.T) {
	r, e := extractBytes(t, fixture(t, []string{"index.html", "assets/app.js"}, []byte("hello"), 0644), 100)
	if e != nil || r.Size != 10 || r.Files != 2 {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestHostilePaths(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "a/../../escape", "C:/evil", "a\\b", "a/./b", "a//b", "nul\x00name", "bad\nname", "a/../b"} {
		t.Run(name, func(t *testing.T) {
			if _, e := extractBytes(t, fixture(t, []string{name}, []byte("x"), 0644), 100); e == nil {
				t.Fatal("accepted unsafe path")
			}
		})
	}
}
func TestDuplicateAndCollision(t *testing.T) {
	for _, names := range [][]string{{"a", "a"}, {"a", "a/b"}, {"a/b", "a"}} {
		if _, e := extractBytes(t, fixture(t, names, []byte("x"), 0644), 100); e == nil {
			t.Fatalf("accepted %v", names)
		}
	}
}
func TestSymlinkAndSpecial(t *testing.T) {
	for _, m := range []os.FileMode{os.ModeSymlink | 0777, os.ModeNamedPipe | 0600, os.ModeDevice | 0600} {
		if _, e := extractBytes(t, fixture(t, []string{"x"}, []byte("../secret"), m), 100); e == nil {
			t.Fatalf("accepted mode %v", m)
		}
	}
}
func TestCorruptionAndBomb(t *testing.T) {
	base := fixture(t, []string{"a"}, bytes.Repeat([]byte("A"), 1024), 0644)
	if _, e := extractBytes(t, base, 100); e == nil {
		t.Fatal("bomb accepted")
	}
	for _, kind := range []string{"truncate", "crc", "local-crc", "local-name", "offset", "encrypted", "prefix", "overlap"} {
		t.Run(kind, func(t *testing.T) {
			b := append([]byte{}, base...)
			cd := bytes.Index(b, []byte{'P', 'K', 1, 2})
			switch kind {
			case "truncate":
				b = b[:len(b)-1]
			case "crc":
				b[cd+16] ^= 1
			case "local-crc":
				b[14] ^= 1
			case "local-name":
				b[30] = 'z'
			case "offset":
				binary.LittleEndian.PutUint32(b[cd+42:], 0xfffffffe)
			case "encrypted":
				b[6] |= 1
				b[cd+8] |= 1
			case "prefix":
				b = append([]byte("MZ"), b...)
			case "overlap":
				binary.LittleEndian.PutUint32(b[cd+20:], 99999)
			}
			if _, e := extractBytes(t, b, 2048); e == nil {
				t.Fatal("accepted corruption")
			}
		})
	}
}
func FuzzExtract(f *testing.F) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("index.html")
	w.Write([]byte("hello"))
	z.Close()
	f.Add(b.Bytes())
	f.Add([]byte("not a zip"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			t.Skip()
		}
		extractBytes(t, b, 1<<20)
	})
}

func TestUnsupportedCompressionMessage(t *testing.T) {
	b := fixture(t, []string{"index.html"}, []byte("hello"), 0644)
	cd := bytes.Index(b, []byte{'P', 'K', 1, 2})
	binary.LittleEndian.PutUint16(b[8:], 9)
	binary.LittleEndian.PutUint16(b[cd+10:], 9)
	_, err := extractBytes(t, b, 100)
	if err == nil || !strings.Contains(err.Error(), "Deflate64 (method 9)") || !strings.Contains(err.Error(), "Store or Deflate") {
		t.Fatalf("expected actionable Deflate64 error, got %v", err)
	}
}
