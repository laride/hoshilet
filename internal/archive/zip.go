// Package archive validates a deliberately narrow ZIP profile before extracting.
package archive

import (
	"archive/zip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxEntries = 10000

var ErrEmptyDeployment = errors.New("empty deployment: at least one regular file is required")
var errFormat = errors.New("invalid ZIP structure: archive is truncated or headers are inconsistent")

type Result struct {
	Size  int64 `json:"size"`
	Files int   `json:"files"`
}

func u16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }
func u32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func validName(n string) bool {
	if n == "" || len(n) > 1024 || !utf8.ValidString(n) || strings.ContainsAny(n, "\\:\x00") || strings.HasPrefix(n, "/") {
		return false
	}
	for _, r := range n {
		if unicode.IsControl(r) {
			return false
		}
	}
	n = strings.TrimSuffix(n, "/")
	parts := strings.Split(n, "/")
	if len(parts) > 32 {
		return false
	}
	for _, s := range parts {
		if s == "" || s == "." || s == ".." || len(s) > 255 {
			return false
		}
	}
	return true
}

// Extract accepts classic single-disk ZIPs, including signed/unsigned data descriptors.
// ZIP64 is unnecessary under the platform limits and intentionally rejected.
func Extract(src, dst string, limit int64) (Result, error) {
	var result Result
	f, e := os.Open(src)
	if e != nil {
		return result, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return result, e
	}
	size := st.Size()
	if size < 22 {
		return result, errFormat
	}
	read := func(off int64, n int) ([]byte, error) {
		if off < 0 || int64(n) > size-off {
			return nil, errFormat
		}
		b := make([]byte, n)
		_, e := f.ReadAt(b, off)
		return b, e
	}
	tailLen := int64(65557)
	if size < tailLen {
		tailLen = size
	}
	tail, _ := read(size-tailLen, int(tailLen))
	end := -1
	for i := len(tail) - 22; i >= 0; i-- {
		if u32(tail[i:]) == 0x06054b50 && i+22+int(u16(tail[i+20:])) == len(tail) {
			end = i
			break
		}
	}
	if end < 0 {
		return result, errFormat
	}
	eo := tail[end:]
	if u16(eo[4:]) != 0 || u16(eo[6:]) != 0 {
		return result, fmt.Errorf("multi-volume ZIP archives are not supported")
	}
	if u16(eo[10:]) == 0xffff || u32(eo[16:]) == 0xffffffff || u32(eo[12:]) == 0xffffffff {
		return result, fmt.Errorf("ZIP64 is not supported; use a classic ZIP archive")
	}
	if u16(eo[10:]) > MaxEntries {
		return result, fmt.Errorf("ZIP entry count exceeds limit of %d", MaxEntries)
	}
	cdOffset, cdSize := int64(u32(eo[16:])), int64(u32(eo[12:]))
	entries := int(u16(eo[10:]))
	if u16(eo[4:]) != 0 || u16(eo[6:]) != 0 || int(u16(eo[8:])) != entries || entries == 0 || entries > MaxEntries || cdOffset+cdSize != size-tailLen+int64(end) {
		return result, errFormat
	}
	zr, e := zip.NewReader(f, size)
	if e != nil {
		return result, e
	}
	if len(zr.File) != entries {
		return result, errFormat
	}
	type span struct{ start, end int64 }
	spans := []span{}
	seen := map[string]bool{}
	kinds := map[string]bool{}
	pos := cdOffset
	for _, z := range zr.File {
		c, e := read(pos, 46)
		if e != nil || u32(c) != 0x02014b50 {
			return result, errFormat
		}
		nl, xl, cl := int(u16(c[28:])), int(u16(c[30:])), int(u16(c[32:]))
		head, e := read(pos+46, nl+xl+cl)
		if e != nil {
			return result, e
		}
		pos += int64(46 + nl + xl + cl)
		off := int64(u32(c[42:]))
		if u16(c[34:]) != 0 || off >= cdOffset || z.CompressedSize64 >= 0xffffffff || z.UncompressedSize64 >= 0xffffffff {
			return result, errFormat
		}
		for x := nl; x < nl+xl; {
			if x+4 > nl+xl {
				return result, errFormat
			}
			tag, l := u16(head[x:]), int(u16(head[x+2:]))
			if tag == 1 {
				return result, fmt.Errorf("ZIP64 is not supported; use a classic ZIP archive")
			}
			if x+4+l > nl+xl {
				return result, errFormat
			}
			x += 4 + l
		}
		if string(head[:nl]) != z.Name || !validName(z.Name) {
			return result, fmt.Errorf("unsafe ZIP path")
		}
		if z.Flags&0x41 != 0 {
			return result, fmt.Errorf("encrypted ZIP entries are not supported: %q", z.Name)
		}
		if z.Method != zip.Store && z.Method != zip.Deflate {
			method := fmt.Sprintf("method %d", z.Method)
			if z.Method == 9 {
				method = "Deflate64 (method 9)"
			}
			return result, fmt.Errorf("unsupported ZIP compression %s: %q; use Store or Deflate", method, z.Name)
		}
		if z.Flags & ^uint16(0x080e) != 0 {
			return result, fmt.Errorf("unsupported ZIP flags 0x%04x: %q", z.Flags, z.Name)
		}
		mode := z.Mode()
		dir := strings.HasSuffix(z.Name, "/")
		if (!mode.IsRegular() && !mode.IsDir()) || mode.IsDir() != dir {
			return result, fmt.Errorf("only regular files and directories accepted")
		}
		name := strings.TrimSuffix(z.Name, "/")
		if seen[name] {
			return result, fmt.Errorf("duplicate ZIP path")
		}
		seen[name] = true
		kinds[name] = dir
		if dir && (z.UncompressedSize64 != 0 || z.CompressedSize64 != 0 || z.CRC32 != 0) {
			return result, errFormat
		}
		if z.UncompressedSize64 > uint64(limit) || result.Size > limit-int64(z.UncompressedSize64) {
			return result, fmt.Errorf("unpacked size exceeds limit")
		}
		result.Size += int64(z.UncompressedSize64)
		local, e := read(off, 30)
		if e != nil || u32(local) != 0x04034b50 {
			return result, errFormat
		}
		ln, lx := int(u16(local[26:])), int(u16(local[28:]))
		lh, e := read(off+30, ln+lx)
		if e != nil || string(lh[:ln]) != z.Name || u16(local[6:]) != z.Flags || u16(local[8:]) != z.Method {
			return result, errFormat
		}
		for x := ln; x < ln+lx; {
			if x+4 > ln+lx {
				return result, errFormat
			}
			l := int(u16(lh[x+2:]))
			if u16(lh[x:]) == 1 {
				return result, fmt.Errorf("ZIP64 is not supported; use a classic ZIP archive")
			}
			if x+4+l > ln+lx {
				return result, errFormat
			}
			x += 4 + l
		}
		dataOff, e := z.DataOffset()
		if e != nil || dataOff != off+30+int64(ln+lx) {
			return result, errFormat
		}
		dataEnd := dataOff + int64(z.CompressedSize64)
		finish := dataEnd
		if z.Flags&8 == 0 {
			if u32(local[14:]) != z.CRC32 || uint64(u32(local[18:])) != z.CompressedSize64 || uint64(u32(local[22:])) != z.UncompressedSize64 {
				return result, errFormat
			}
		} else {
			if (u32(local[14:]) != 0 && u32(local[14:]) != z.CRC32) || (u32(local[18:]) != 0 && uint64(u32(local[18:])) != z.CompressedSize64) || (u32(local[22:]) != 0 && uint64(u32(local[22:])) != z.UncompressedSize64) {
				return result, errFormat
			}
			d, e := read(dataEnd, 12)
			if e != nil {
				return result, e
			}
			if u32(d) == 0x08074b50 {
				d, e = read(dataEnd+4, 12)
				finish += 4
				if e != nil {
					return result, e
				}
			}
			if u32(d) != z.CRC32 || uint64(u32(d[4:])) != z.CompressedSize64 || uint64(u32(d[8:])) != z.UncompressedSize64 {
				return result, errFormat
			}
			finish += 12
		}
		if finish > cdOffset {
			return result, errFormat
		}
		spans = append(spans, span{off, finish})
	}
	if pos != cdOffset+cdSize {
		return result, errFormat
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	last := int64(0)
	for _, s := range spans {
		if s.start != last {
			return result, fmt.Errorf("ZIP entries overlap or contain gaps/prefix data")
		}
		last = s.end
	}
	if last != cdOffset {
		return result, errFormat
	}
	allPaths := map[string]bool{}
	for n := range kinds {
		allPaths[n] = true
		for p := path.Dir(n); p != "."; p = path.Dir(p) {
			allPaths[p] = true
		}
		if len(allPaths) > MaxEntries {
			return result, fmt.Errorf("expanded file/directory count exceeds limit")
		}
	}
	for n := range kinds {
		for p := path.Dir(n); p != "."; p = path.Dir(p) {
			if isDir, ok := kinds[p]; ok && !isDir {
				return result, fmt.Errorf("file/directory collision")
			}
		}
	}
	if e = os.Mkdir(dst, 0700); e != nil {
		return result, e
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(dst)
		}
	}()
	result.Size = 0
	for _, z := range zr.File {
		target := filepath.Join(dst, filepath.FromSlash(z.Name))
		if z.FileInfo().IsDir() {
			if e = os.MkdirAll(target, 0755); e != nil {
				return result, e
			}
			continue
		}
		if e = os.MkdirAll(filepath.Dir(target), 0755); e != nil {
			return result, e
		}
		out, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if e != nil {
			return result, e
		}
		in, e := z.Open()
		if e != nil {
			out.Close()
			return result, e
		}
		n, copyErr := io.Copy(out, io.LimitReader(in, limit-result.Size+1))
		in.Close()
		syncErr := out.Sync()
		closeErr := out.Close()
		if copyErr != nil {
			return result, copyErr
		}
		if syncErr != nil {
			return result, syncErr
		}
		if closeErr != nil {
			return result, closeErr
		}
		if n != int64(z.UncompressedSize64) || n > limit-result.Size {
			return result, fmt.Errorf("actual unpacked size exceeds limit or declaration")
		}
		result.Size += n
		result.Files++
	}
	if result.Files == 0 {
		return result, ErrEmptyDeployment
	}
	// Make all directory entries durable before the release is published.
	e = filepath.WalkDir(dst, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			return nil
		}
		h, e := os.Open(p)
		if e != nil {
			return e
		}
		defer h.Close()
		return h.Sync()
	})
	if e != nil {
		return result, e
	}
	if e = os.Chmod(dst, 0755); e != nil {
		return result, e
	}
	root, e := os.Open(dst)
	if e != nil {
		return result, e
	}
	e = root.Sync()
	root.Close()
	if e != nil {
		return result, e
	}
	success = true
	return result, nil
}
