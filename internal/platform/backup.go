package platform

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Backup requires the same exclusive process lock as the server. Run while stopped.
// Public symlinks are a projection and are rebuilt from the database on restore.
func (a *App) Backup(output string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	tmp, e := os.MkdirTemp(filepath.Join(a.C.DataDir, "staging"), "backup-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	snapshot := filepath.Join(tmp, "hoshilet.db")
	if _, e = a.DB.Exec("VACUUM INTO ?", snapshot); e != nil {
		return e
	}
	out, e := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	complete := false
	defer func() {
		out.Close()
		if !complete {
			os.Remove(output)
		}
	}()
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	add := func(src, name string) error {
		st, e := os.Lstat(src)
		if e != nil {
			return e
		}
		if st.IsDir() {
			return tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Typeflag: tar.TypeDir, ModTime: st.ModTime()})
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("backup contains non-regular file: %s", name)
		}
		h := &tar.Header{Name: name, Mode: 0600, Size: st.Size(), ModTime: st.ModTime()}
		if e = tw.WriteHeader(h); e != nil {
			return e
		}
		f, e := os.Open(src)
		if e != nil {
			return e
		}
		defer f.Close()
		_, e = io.Copy(tw, f)
		return e
	}
	if e = add(snapshot, "database/hoshilet.db"); e != nil {
		return e
	}
	e = filepath.WalkDir(filepath.Join(a.C.DataDir, "releases"), func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		name, e := filepath.Rel(a.C.DataDir, p)
		if e != nil {
			return e
		}
		return add(p, filepath.ToSlash(name))
	})
	if e != nil {
		return e
	}
	if e = tw.Close(); e != nil {
		return e
	}
	if e = gz.Close(); e != nil {
		return e
	}
	if e = out.Sync(); e != nil {
		return e
	}
	if e = out.Close(); e != nil {
		return e
	}
	complete = true
	return syncDir(filepath.Dir(output))
}

// Restore never overwrites an existing data directory. The archive must be trusted:
// it contains authorization state, not just static files.
func Restore(c Config, input string) error {
	if _, e := os.Lstat(c.DataDir); !os.IsNotExist(e) {
		return fmt.Errorf("restore requires a non-existent data_dir")
	}
	parent := filepath.Dir(c.DataDir)
	tmp, e := os.MkdirTemp(parent, ".hoshilet-restore-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	f, e := os.Open(input)
	if e != nil {
		return e
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		return e
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		n := h.Name
		if (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir) || (h.Typeflag == tar.TypeDir && h.Size != 0) || (n == "database/hoshilet.db" && h.Typeflag != tar.TypeReg) || h.Size < 0 || filepath.IsAbs(n) || strings.ContainsAny(n, "\\\x00") || filepath.ToSlash(filepath.Clean(n)) != n || strings.HasPrefix(n, "../") || seen[n] || (n != "database/hoshilet.db" && !(n == "releases" && h.Typeflag == tar.TypeDir) && !strings.HasPrefix(n, "releases/")) {
			return fmt.Errorf("unsafe backup entry")
		}
		seen[n] = true
		p := filepath.Join(tmp, filepath.FromSlash(n))
		if h.Typeflag == tar.TypeDir {
			if e = os.MkdirAll(p, 0755); e != nil {
				return e
			}
			continue
		}
		mode := os.FileMode(0644)
		dirMode := os.FileMode(0755)
		if n == "database/hoshilet.db" {
			mode = 0600
			dirMode = 0700
		}
		if e = os.MkdirAll(filepath.Dir(p), dirMode); e != nil {
			return e
		}
		out, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if e != nil {
			return e
		}
		_, e = io.Copy(out, tr)
		se := out.Sync()
		ce := out.Close()
		if e != nil {
			return e
		}
		if se != nil {
			return se
		}
		if ce != nil {
			return ce
		}
	}
	if !seen["database/hoshilet.db"] {
		return fmt.Errorf("backup missing database")
	}
	if _, e = io.Copy(io.Discard, gz); e != nil {
		return e
	}
	// Validate and rebuild entirely in the temporary directory. Nothing is installed
	// until the database and every retained release have been checked successfully.
	stagedConfig := c
	stagedConfig.DataDir = tmp
	a, e := Open(stagedConfig)
	if e != nil {
		return e
	}
	validationErr := func() (err error) {
		defer errorBoundary(&err)
		var integrity string
		if err = a.DB.QueryRow("PRAGMA quick_check").Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("invalid backup database: %s", integrity)
		}
		for _, d := range a.rows("SELECT project_id,id FROM deployments WHERE status='ready'") {
			st, e := os.Lstat(filepath.Join(tmp, "releases", str(d, "project_id"), str(d, "id")))
			if e != nil {
				return e
			}
			if !st.IsDir() {
				return fmt.Errorf("retained release is not a directory")
			}
		}
		return nil
	}()
	a.Close()
	if validationErr != nil {
		return validationErr
	}
	if e = filepath.WalkDir(tmp, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return syncDir(p)
		}
		return nil
	}); e != nil {
		return e
	}
	if e = unix.Renameat2(unix.AT_FDCWD, tmp, unix.AT_FDCWD, c.DataDir, unix.RENAME_NOREPLACE); e != nil {
		return e
	}
	if e = syncDir(parent); e != nil {
		if rollback := os.Rename(c.DataDir, tmp); rollback != nil {
			return fmt.Errorf("restore installed but parent sync failed: %v; rollback failed: %w", e, rollback)
		}
		return e
	}
	return nil
}
