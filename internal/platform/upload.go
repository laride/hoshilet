package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"hoshilet/internal/archive"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (a *App) resolveProject(p principal, r *http.Request) row {
	qs := r.URL.Query()
	pid := qs.Get("project_id")
	if pid != "" {
		q := a.project(p, pid, false)
		if (qs.Get("namespace") != "" && qs.Get("namespace") != str(q, "namespace_name")) || (qs.Has("project") && qs.Get("project") != str(q, "name")) {
			fail(400, "target_conflict", "Conflicting target arguments")
		}
		return q
	}
	if str(p.Credential, "kind") == "oidc" || str(p.Credential, "scope") == "project" {
		q := a.project(p, str(p.Credential, "target"), false)
		if (qs.Get("namespace") != "" && qs.Get("namespace") != str(q, "namespace_name")) || (qs.Has("project") && qs.Get("project") != str(q, "name")) {
			fail(400, "target_conflict", "Conflicting target arguments")
		}
		return q
	}
	var ns row
	if qs.Get("namespace") != "" {
		ns = a.one("SELECT * FROM namespaces WHERE name=?", qs.Get("namespace"))
	} else if str(p.Credential, "scope") == "namespace" {
		ns = a.one("SELECT * FROM namespaces WHERE id=?", p.Credential["target"])
	} else {
		ns = a.one("SELECT * FROM namespaces WHERE id=?", p.User["personal_ns"])
	}
	if ns == nil {
		fail(404, "not_found", "Namespace not found")
	}
	a.namespace(p, str(ns, "id"), false)
	name := qs.Get("project")
	q := a.one("SELECT id FROM projects WHERE namespace_id=? AND name=? AND deleted=0", ns["id"], name)
	if q != nil {
		return a.project(p, str(q, "id"), false)
	}
	if str(p.Credential, "kind") != "key" || name == "" {
		fail(404, "not_found", "Create the project in the management interface first")
	}
	a.namespace(p, str(ns, "id"), true)
	return a.createProject(str(ns, "id"), name, "projects", p.ID)
}
func (a *App) deploy(w http.ResponseWriter, r *http.Request, p principal) {
	if r.Header.Get("Content-Type") != "application/zip" {
		fail(415, "unsupported_type", "Use application/zip")
	}
	idem := r.Header.Get("Idempotency-Key")
	if len(idem) < 8 || len(idem) > 128 {
		fail(400, "idempotency_required", "Idempotency-Key must contain 8–128 characters")
	}
	releaseSlot := a.acquireUpload(p)
	defer releaseSlot()
	ctx, cancelUpload := context.WithTimeout(r.Context(), time.Duration(a.C.UploadTimeoutSeconds)*time.Second)
	defer cancelUpload()
	r = r.WithContext(ctx)
	controller := http.NewResponseController(w)
	if e := controller.SetReadDeadline(time.Now().Add(time.Duration(a.C.UploadTimeoutSeconds) * time.Second)); e != nil && !errors.Is(e, http.ErrNotSupported) {
		panic(e)
	}
	defer controller.SetReadDeadline(time.Time{})
	stopClose := context.AfterFunc(ctx, func() { _ = r.Body.Close() })
	defer stopClose()
	var uid, pid string
	var revision, maxBytes int64
	values := r.URL.Query()
	for k, vs := range values {
		if (k != "project_id" && k != "namespace" && k != "project" && k != "activate") || len(vs) != 1 {
			fail(400, "invalid_query", "Unknown or repeated deployment parameter")
		}
	}
	if values.Has("activate") && values.Get("activate") != "true" && values.Get("activate") != "false" {
		fail(400, "invalid_query", "activate must be true or false")
	}
	activate := values.Get("activate") != "false"
	func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.recheck(p)
		q := a.resolveProject(p, r)
		pid = str(q, "id")
		if a.one("SELECT project_id FROM publications WHERE project_id=?", pid) != nil {
			fail(409, "recovering", "Publication recovery pending")
		}
		if num(q, "disabled") != 0 {
			fail(409, "disabled", "Project is disabled")
		}
		existing := a.one("SELECT * FROM uploads WHERE actor=? AND idem=?", p.ID, idem)
		if existing != nil {
			if str(existing, "project_id") != pid || num(existing, "activate") != boolInt(activate) {
				fail(409, "idempotency_conflict", "Idempotency key reused for another request")
			}
			send(w, 200, existing)
			return
		}
		revision = num(q, "revision")
		if v := r.Header.Get("If-Match"); v != "" {
			n, e := strconv.ParseInt(strings.Trim(v, "\""), 10, 64)
			if e != nil || n != revision {
				fail(409, "revision_conflict", "Project changed")
			}
		}
		settings := a.one("SELECT * FROM settings")
		maxBytes = num(settings, "max_bytes")
		if !activate && num(settings, "max_versions") == 1 && str(q, "current_id") != "" {
			fail(409, "retention_conflict", "X=1 does not allow a staged version alongside the current version")
		}
		var fs unix.Statfs_t
		if e := unix.Statfs(a.C.DataDir, &fs); e != nil {
			panic(e)
		}
		required := a.C.MinFreeBytes + uint64(a.C.MaxArchiveBytes) + uint64(maxBytes)
		if fs.Bavail*uint64(fs.Bsize) < required {
			fail(507, "disk_full", "Insufficient free disk space")
		}
		uid = id()
		a.exec("INSERT INTO uploads(id,project_id,actor,idem,revision,activate,status,created_at) VALUES(?,?,?,?,?,?,'receiving',?)", uid, pid, p.ID, idem, revision, activate, now())
	}()
	if uid == "" {
		return
	}
	done := false
	defer func() {
		if !done {
			a.exec("UPDATE uploads SET status='failed',error='Upload failed; see response or server log' WHERE id=? AND status IN ('receiving','validating')", uid)
		}
	}()
	dir := filepath.Join(a.C.DataDir, "staging", uid)
	if e := os.Mkdir(dir, 0700); e != nil {
		panic(e)
	}
	defer os.RemoveAll(dir)
	src := filepath.Join(dir, "upload.zip")
	f, e := os.OpenFile(src, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		panic(e)
	}
	_, e = io.Copy(f, http.MaxBytesReader(w, r.Body, a.C.MaxArchiveBytes))
	ce := f.Close()
	if e != nil {
		var timeout net.Error
		if ctx.Err() != nil || (errors.As(e, &timeout) && timeout.Timeout()) {
			fail(408, "upload_timeout", "Upload deadline exceeded")
		}
		fail(413, "upload_limit", "Upload incomplete or exceeds archive limit")
	}
	if ce != nil {
		panic(ce)
	}
	a.exec("UPDATE uploads SET status='validating' WHERE id=?", uid)
	executable, e := os.Executable()
	if e != nil {
		panic(e)
	}
	dst := filepath.Join(dir, "files")
	extractCtx, cancel := context.WithTimeout(r.Context(), time.Duration(a.C.ExtractionSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(extractCtx, executable, "extract", src, dst, strconv.FormatInt(maxBytes, 10), strconv.Itoa(a.C.ExtractionSeconds), strconv.FormatUint(a.C.WorkerMemoryMB, 10))
	var output bytes.Buffer
	cmd.Stdout = &output
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{W: &stderr, N: 4096}
	if e = cmd.Run(); e != nil {
		a.exec("UPDATE uploads SET status='failed',error=? WHERE id=?", strings.TrimSpace(stderr.String()), uid)
		slog.Warn("archive rejected", "upload_id", uid, "error", strings.TrimSpace(stderr.String()))
		if ctx.Err() != nil {
			fail(408, "upload_timeout", "Upload deadline exceeded")
		}
		if strings.TrimSpace(stderr.String()) == archive.ErrEmptyDeployment.Error() {
			fail(422, "empty_deployment", archive.ErrEmptyDeployment.Error())
		}
		fail(422, "invalid_archive", "Archive rejected: "+strings.TrimSpace(stderr.String()))
	}
	var result archive.Result
	if e = json.Unmarshal(output.Bytes(), &result); e != nil {
		panic(e)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.recheck(p)
	if ctx.Err() != nil {
		fail(408, "upload_timeout", "Upload deadline exceeded")
	}
	q := a.project(p, pid, false)
	if num(q, "disabled") != 0 {
		fail(409, "disabled", "Project is disabled")
	}
	if num(q, "revision") != revision {
		fail(409, "revision_conflict", "Project changed during upload; retry explicitly")
	}
	settings := a.one("SELECT * FROM settings")
	if result.Size > num(settings, "max_bytes") {
		fail(413, "size_limit", "Size policy changed during upload")
	}
	if !activate && num(settings, "max_versions") == 1 && str(q, "current_id") != "" {
		fail(409, "retention_conflict", "Retention policy changed during upload")
	}
	did := id()
	release := filepath.Join(a.C.DataDir, "releases", pid, did)
	if e = os.MkdirAll(filepath.Dir(release), 0755); e != nil {
		panic(e)
	}
	if e = syncDir(filepath.Join(a.C.DataDir, "releases")); e != nil {
		panic(e)
	}
	if e = os.Rename(dst, release); e != nil {
		panic(e)
	}
	if e = syncDir(filepath.Dir(release)); e != nil {
		panic(e)
	}
	tx, e := a.DB.Begin()
	if e != nil {
		panic(e)
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT INTO deployments(id,project_id,uploaded_at,size,files) VALUES(?,?,?,?,?)", did, pid, now(), result.Size, result.Files); e != nil {
		panic(e)
	}
	status := "complete"
	if activate {
		status = "publishing"
		if _, e = tx.Exec("INSERT INTO publications VALUES(?,?,?,?,?)", pid, did, q["current_id"], p.ID, now()); e != nil {
			panic(e)
		}
	}
	if _, e = tx.Exec("UPDATE uploads SET deployment_id=?,status=? WHERE id=?", did, status, uid); e != nil {
		panic(e)
	}
	if e = tx.Commit(); e != nil {
		panic(e)
	}
	done = true
	if activate {
		if e = a.finishPublication(q); e != nil {
			panic(e)
		}
	}
	a.trim(pid, did)
	a.audit(p.ID, "deployment.upload", did)
	send(w, 201, a.one("SELECT * FROM uploads WHERE id=?", uid))
}
func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

type limitedWriter struct {
	W io.Writer
	N int
}

func (w *limitedWriter) Write(b []byte) (int, error) {
	n := len(b)
	if w.N > 0 {
		take := len(b)
		if take > w.N {
			take = w.N
		}
		_, e := w.W.Write(b[:take])
		w.N -= take
		if e != nil {
			return 0, e
		}
	}
	return n, nil
}

// All user credentials share a quota. CI tokens share issuer+repository quota,
// so minting another Key/JWT cannot bypass per-principal limits.
func (a *App) acquireUpload(p principal) func() {
	owner := "user:" + str(p.User, "id")
	if p.User == nil {
		t := a.one("SELECT issuer_id,repository_id FROM trusts WHERE id=?", p.Credential["trust_id"])
		if t == nil {
			fail(403, "trust_revoked", "Publisher trust unavailable")
		}
		owner = "oidc:" + str(t, "issuer_id") + ":" + str(t, "repository_id")
	}
	a.slotMu.Lock()
	defer a.slotMu.Unlock()
	if a.uploadCounts[owner] >= a.C.MaxUploadsPerPrincipal {
		fail(429, "principal_busy", "Your upload concurrency limit is reached")
	}
	select {
	case a.workers <- struct{}{}:
	default:
		fail(429, "busy", "Upload capacity reached; retry later")
	}
	a.uploadCounts[owner]++
	return func() {
		a.slotMu.Lock()
		defer a.slotMu.Unlock()
		<-a.workers
		a.uploadCounts[owner]--
		if a.uploadCounts[owner] == 0 {
			delete(a.uploadCounts, owner)
		}
	}
}
