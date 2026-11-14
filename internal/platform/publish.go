package platform

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

func syncDir(p string) error {
	f, e := os.Open(p)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func (a *App) linkPath(q row) string {
	p := filepath.Join(a.C.DataDir, "public", str(q, "namespace_name"))
	if str(q, "mode") == "projects" {
		p = filepath.Join(p, str(q, "name"))
	}
	return p
}
func (a *App) syncLink(q row, did string) error {
	p := a.linkPath(q)
	if num(q, "disabled") != 0 || num(q, "deleted") != 0 || did == "" {
		e := os.Remove(p)
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		return syncDir(filepath.Dir(p))
	}
	target := filepath.Join(a.C.DataDir, "releases", str(q, "id"), did)
	st, e := os.Lstat(target)
	if e != nil {
		return e
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("release must be a real directory")
	}
	if e = os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		return e
	}
	if e = syncDir(filepath.Join(a.C.DataDir, "public")); e != nil {
		return e
	}
	relative, e := filepath.Rel(filepath.Dir(p), target)
	if e != nil {
		return e
	}
	tmp := filepath.Join(a.C.DataDir, "staging", "publish-"+id())
	if e = os.Symlink(relative, tmp); e != nil {
		return e
	}
	defer os.Remove(tmp)
	if e = os.Rename(tmp, p); e != nil {
		return e
	}
	return syncDir(filepath.Dir(p))
}
func (a *App) activate(q row, did string, revision int64, actor string) {
	if num(q, "disabled") != 0 {
		fail(409, "disabled", "Project is disabled")
	}
	if num(q, "revision") != revision {
		fail(409, "revision_conflict", "Project changed; refresh before publishing")
	}
	if a.one("SELECT project_id FROM publications WHERE project_id=?", str(q, "id")) != nil {
		fail(409, "recovering", "Publication recovery is pending")
	}
	if a.one("SELECT id FROM deployments WHERE id=? AND project_id=? AND status='ready'", did, q["id"]) == nil {
		fail(404, "not_found", "Deployment not available")
	}
	a.exec("INSERT INTO publications VALUES(?,?,?,?,?)", q["id"], did, q["current_id"], actor, now())
	if e := a.finishPublication(q); e != nil {
		panic(e)
	}
	a.trim(str(q, "id"), did)
}
func (a *App) finishPublication(q row) error {
	op := a.one("SELECT * FROM publications WHERE project_id=?", q["id"])
	if op == nil {
		return nil
	}
	if e := a.syncLink(q, str(op, "deployment_id")); e != nil {
		return e
	}
	tx, e := a.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, x := range []struct {
		s string
		v []any
	}{{"UPDATE projects SET current_id=?,revision=revision+1 WHERE id=?", []any{op["deployment_id"], q["id"]}}, {"INSERT INTO activations VALUES(?,?,?,?,?,?)", []any{id(), q["id"], op["deployment_id"], op["previous_id"], op["actor"], now()}}, {"DELETE FROM publications WHERE project_id=?", []any{q["id"]}}, {"UPDATE uploads SET status='complete' WHERE deployment_id=?", []any{op["deployment_id"]}}} {
		if _, e = tx.Exec(x.s, x.v...); e != nil {
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	a.audit(str(op, "actor"), "deployment.activate", str(op, "deployment_id"))
	return nil
}
func (a *App) trim(pid, protect string) {
	q := a.one("SELECT * FROM projects WHERE id=?", pid)
	limit := num(a.one("SELECT max_versions FROM settings"), "max_versions")
	all := a.rows("SELECT * FROM deployments WHERE project_id=? AND status='ready' ORDER BY uploaded_at ASC,rowid ASC", pid)
	remaining := int64(len(all))
	for _, d := range all {
		if remaining <= limit {
			break
		}
		if d["id"] == q["current_id"] || str(d, "id") == protect {
			continue
		}
		a.exec("UPDATE deployments SET status='retired',gc_after=? WHERE id=?", now()+int64(a.C.GCSeconds), d["id"])
		remaining--
	}
}
func (a *App) removeProject(q row, actor string) {
	if a.one("SELECT project_id FROM publications WHERE project_id=?", q["id"]) != nil {
		fail(409, "recovering", "Publication recovery pending")
	}
	a.exec("UPDATE projects SET deleted=1,revision=revision+1 WHERE id=?", q["id"])
	q["deleted"] = int64(1)
	if e := a.syncLink(q, ""); e != nil {
		panic(e)
	}
	a.exec("UPDATE deployments SET status='retired',gc_after=? WHERE project_id=?", now()+int64(a.C.GCSeconds), q["id"])
	if str(q, "mode") == "projects" {
		_ = os.Remove(filepath.Dir(a.linkPath(q)))
	}
	a.audit(actor, "project.delete", str(q, "id"))
}
func (a *App) recover() (err error) {
	defer errorBoundary(&err)
	for _, q := range a.rows("SELECT p.*,n.name namespace_name,n.mode FROM projects p JOIN namespaces n ON n.id=p.namespace_id WHERE p.deleted=0") {
		if e := a.finishPublication(q); e != nil {
			return e
		}
		q = a.one("SELECT p.*,n.name namespace_name,n.mode FROM projects p JOIN namespaces n ON n.id=p.namespace_id WHERE p.id=?", q["id"])
		if e := a.syncLink(q, str(q, "current_id")); e != nil {
			return e
		}
		a.trim(str(q, "id"), "")
	}
	a.exec("UPDATE uploads SET status='complete' WHERE status!='complete' AND deployment_id IN (SELECT id FROM deployments WHERE status='ready')")
	a.exec("UPDATE uploads SET status='failed',error='Server restarted before commit' WHERE status IN ('receiving','validating')")
	// Staging is private, and a process lock excludes other writers.
	entries, e := os.ReadDir(filepath.Join(a.C.DataDir, "staging"))
	if e != nil {
		return e
	}
	for _, x := range entries {
		if e = os.RemoveAll(filepath.Join(a.C.DataDir, "staging", x.Name())); e != nil {
			return e
		}
	}
	// Rebuild public projection to remove links left by a crash during deletion.
	wanted := map[string]bool{}
	for _, q := range a.rows("SELECT p.*,n.name namespace_name,n.mode FROM projects p JOIN namespaces n ON n.id=p.namespace_id WHERE p.deleted=0") {
		if str(q, "current_id") != "" && num(q, "disabled") == 0 {
			wanted[a.linkPath(q)] = true
		}
	}
	err = filepath.WalkDir(filepath.Join(a.C.DataDir, "public"), func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 && !wanted[p] {
			return os.Remove(p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	dirs, _ := os.ReadDir(filepath.Join(a.C.DataDir, "public"))
	for _, d := range dirs {
		if d.IsDir() {
			_ = os.Remove(filepath.Join(a.C.DataDir, "public", d.Name()))
		}
	}
	return nil
}
func (a *App) Collect() (err error) {
	defer errorBoundary(&err)
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, q := range a.rows("SELECT p.*,n.name namespace_name,n.mode FROM projects p JOIN namespaces n ON n.id=p.namespace_id JOIN publications x ON x.project_id=p.id") {
		if e := a.finishPublication(q); e != nil {
			return e
		}
		a.trim(str(q, "id"), "")
	}
	for _, d := range a.rows("SELECT * FROM deployments WHERE status='retired' AND gc_after<=?", now()) {
		if a.one("SELECT id FROM projects WHERE current_id=? AND deleted=0", d["id"]) != nil || a.one("SELECT project_id FROM publications WHERE deployment_id=? OR previous_id=?", d["id"], d["id"]) != nil {
			continue
		}
		if e := os.RemoveAll(filepath.Join(a.C.DataDir, "releases", str(d, "project_id"), str(d, "id"))); e != nil {
			return e
		}
		a.exec("UPDATE deployments SET status='purged' WHERE id=?", d["id"])
	}
	// A crash after rename but before DB commit can leave an unreferenced release.
	roots, _ := os.ReadDir(filepath.Join(a.C.DataDir, "releases"))
	for _, root := range roots {
		if !root.IsDir() {
			continue
		}
		ds, _ := os.ReadDir(filepath.Join(a.C.DataDir, "releases", root.Name()))
		for _, d := range ds {
			if a.one("SELECT id FROM deployments WHERE id=?", d.Name()) != nil {
				continue
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			if time.Since(info.ModTime()) > time.Duration(a.C.GCSeconds)*time.Second {
				if e = os.RemoveAll(filepath.Join(a.C.DataDir, "releases", root.Name(), d.Name())); e != nil {
					return e
				}
			}
		}
	}
	a.exec("DELETE FROM exchanges WHERE expires_at<=?", now())
	a.exec("DELETE FROM credentials WHERE kind IN ('session','oidc') AND expires_at<=?", now()-86400)
	return nil
}
func (a *App) Maintenance(stop <-chan struct{}) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	a.maintenanceLoop(stop, t.C)
}
func (a *App) maintenanceLoop(stop <-chan struct{}, ticks <-chan time.Time) {
	for {
		select {
		case <-stop:
			return
		case _, ok := <-ticks:
			if !ok {
				return
			}
			if e := a.Collect(); e != nil {
				slog.Error("maintenance failed", "error", e)
			}
		}
	}
}
