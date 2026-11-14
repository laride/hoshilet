package platform

import (
	"golang.org/x/sys/unix"
	"net/http"
	"net/url"
	"strings"
)

func validOrigin(s string) bool {
	u, e := url.Parse(s)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.Path == "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
func (a *App) adminAPI(w http.ResponseWriter, r *http.Request, p principal, parts []string) {
	if len(parts) < 2 {
		fail(404, "not_found", "Endpoint not found")
	}
	switch parts[1] {
	case "namespaces":
		if r.Method == "GET" && len(parts) == 2 {
			send(w, 200, a.namespaceUsage())
			return
		}
		if r.Method == "GET" && len(parts) == 3 {
			send(w, 200, a.namespaceUsageDetail(parts[2]))
			return
		}
	case "metrics":
		if r.Method == "GET" {
			var fs unix.Statfs_t
			if e := unix.Statfs(a.C.DataDir, &fs); e != nil {
				panic(e)
			}
			send(w, 200, row{"disk_free_bytes": fs.Bavail * uint64(fs.Bsize), "upload_slots_in_use": len(a.workers), "uploads": a.rows("SELECT status,count(*) count FROM uploads GROUP BY status"), "deployments": a.rows("SELECT status,count(*) count,coalesce(sum(size),0) bytes FROM deployments GROUP BY status"), "pending_publications": num(a.one("SELECT count(*) n FROM publications"), "n")})
			return
		}

	case "settings":
		if r.Method == "GET" {
			s := a.one("SELECT * FROM settings")
			s["retired_bytes"] = num(a.one("SELECT coalesce(sum(size),0) n FROM deployments WHERE status='retired'"), "n")
			s["recovery_pending"] = num(a.one("SELECT count(*) n FROM publications"), "n")
			send(w, 200, s)
			return
		}
		if r.Method == "PUT" {
			var b struct {
				MaxVersions *int   `json:"max_versions"`
				MaxBytes    *int64 `json:"max_bytes"`
			}
			body(r, &b)
			if b.MaxVersions == nil || b.MaxBytes == nil {
				fail(400, "missing_fields", "All settings fields are required")
			}
			if *b.MaxVersions < 1 || *b.MaxVersions > 1000 || *b.MaxBytes < 1 || *b.MaxBytes > 4<<30 {
				fail(400, "invalid_limits", "Versions must be 1–1000; bytes must be 1–4 GiB")
			}
			a.exec("UPDATE settings SET max_versions=?,max_bytes=?", *b.MaxVersions, *b.MaxBytes)
			for _, q := range a.rows("SELECT * FROM projects WHERE deleted=0") {
				a.trim(str(q, "id"), "")
			}
			a.audit(p.ID, "settings.update", encode(b))
			send(w, 200, a.one("SELECT * FROM settings"))
			return
		}
	case "requests":
		if r.Method == "GET" {
			send(w, 200, a.rows("SELECT r.*,u.username FROM requests r JOIN users u ON u.id=r.user_id ORDER BY r.created_at DESC"))
			return
		}
		if r.Method == "POST" && len(parts) == 3 {
			var b struct {
				Approve bool   `json:"approve"`
				Reason  string `json:"reason"`
			}
			body(r, &b)
			q := a.one("SELECT * FROM requests WHERE id=? AND status='pending'", parts[2])
			if q == nil {
				fail(409, "not_pending", "Request is not pending")
			}
			status := "rejected"
			if b.Approve {
				if a.one("SELECT id FROM namespaces WHERE name=?", q["name"]) != nil {
					fail(409, "name_taken", "Namespace exists")
				}
				nid := id()
				tx, e := a.DB.Begin()
				if e != nil {
					panic(e)
				}
				defer tx.Rollback()
				if _, e = tx.Exec("INSERT INTO namespaces(id,name,created_at) VALUES(?,?,?)", nid, q["name"], now()); e != nil {
					panic(e)
				}
				if _, e = tx.Exec("INSERT INTO members VALUES(?,?,'admin')", nid, q["user_id"]); e != nil {
					panic(e)
				}
				if _, e = tx.Exec("UPDATE requests SET status='approved',reason=? WHERE id=?", b.Reason, parts[2]); e != nil {
					panic(e)
				}
				if e = tx.Commit(); e != nil {
					panic(e)
				}
				status = "approved"
			} else {
				a.exec("UPDATE requests SET status=?,reason=? WHERE id=?", status, b.Reason, parts[2])
			}
			a.audit(p.ID, "namespace.request."+status, parts[2])
			send(w, 200, row{"status": status})
			return
		}
	case "users":
		if r.Method == "GET" {
			send(w, 200, a.rows("SELECT * FROM users ORDER BY username"))
			return
		}
		if r.Method == "POST" {
			var b struct {
				GitlabID int64 `json:"gitlab_id"`
			}
			body(r, &b)
			if b.GitlabID <= 0 {
				fail(400, "invalid_user", "Positive GitLab user ID required")
			}
			if a.one("SELECT id FROM users WHERE gitlab_id=?", b.GitlabID) != nil {
				fail(409, "user_exists", "Update the existing user instead")
			}
			uid := id()
			a.exec("INSERT INTO users(id,gitlab_id,username,status) VALUES(?,?,'','approved')", uid, b.GitlabID)
			a.audit(p.ID, "user.approve", uid)
			send(w, 201, a.one("SELECT * FROM users WHERE id=?", uid))
			return
		}
		if r.Method == "PATCH" && len(parts) == 3 {
			var b struct {
				Status string `json:"status"`
			}
			body(r, &b)
			if b.Status != "approved" && b.Status != "disabled" {
				fail(400, "invalid_status", "Expected approved or disabled")
			}
			u := a.one("SELECT * FROM users WHERE id=?", parts[2])
			if u == nil {
				fail(404, "not_found", "User not found")
			}
			if b.Status != "approved" && includes(a.C.AdminIDs, num(u, "gitlab_id")) {
				fail(409, "bootstrap_admin", "Configured administrators cannot be disabled")
			}
			a.exec("UPDATE users SET status=? WHERE id=?", b.Status, parts[2])
			if b.Status == "disabled" {
				a.exec("UPDATE credentials SET revoked=1 WHERE user_id=? AND kind='key'", parts[2])
			}
			a.ensurePersonalNamespace(a.one("SELECT * FROM users WHERE id=?", parts[2]))
			a.audit(p.ID, "user."+b.Status, parts[2])
			send(w, 200, a.one("SELECT * FROM users WHERE id=?", parts[2]))
			return
		}

	case "issuers":
		if r.Method == "GET" {
			send(w, 200, a.rows("SELECT * FROM issuers"))
			return
		}
		if r.Method == "POST" {
			var b struct {
				URL     string `json:"url"`
				JWKSURL string `json:"jwks_url"`
			}
			body(r, &b)
			u, e := url.Parse(b.JWKSURL)
			if !validOrigin(b.URL) || e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || !strings.HasPrefix(b.JWKSURL, b.URL+"/") {
				fail(400, "invalid_issuer", "Use an HTTPS origin and a JWKS URL on that origin")
			}
			if a.one("SELECT id FROM issuers WHERE url=?", b.URL) != nil {
				fail(409, "duplicate_issuer", "Issuer exists")
			}
			iid := id()
			a.exec("INSERT INTO issuers(id,url,jwks_url) VALUES(?,?,?)", iid, b.URL, b.JWKSURL)
			a.audit(p.ID, "issuer.create", iid)
			send(w, 201, row{"id": iid})
			return
		}
		if r.Method == "PATCH" && len(parts) == 3 {
			var b struct {
				Enabled bool `json:"enabled"`
			}
			body(r, &b)
			a.exec("UPDATE issuers SET enabled=? WHERE id=?", b.Enabled, parts[2])
			a.audit(p.ID, "issuer.enabled", parts[2]+":"+encode(b))
			send(w, 200, row{"ok": true})
			return
		}
	case "projects":
		if r.Method == "PATCH" && len(parts) == 3 {
			var b struct {
				Disabled bool `json:"disabled"`
			}
			body(r, &b)
			q := a.one("SELECT p.*,n.name namespace_name,n.mode FROM projects p JOIN namespaces n ON n.id=p.namespace_id WHERE p.id=? AND p.deleted=0", parts[2])
			if q == nil {
				fail(404, "not_found", "Project not found")
			}
			if a.one("SELECT project_id FROM publications WHERE project_id=?", q["id"]) != nil {
				fail(409, "recovering", "Publication recovery pending")
			}
			a.exec("UPDATE projects SET disabled=?,revision=revision+1 WHERE id=?", b.Disabled, parts[2])
			q["disabled"] = int64(0)
			if b.Disabled {
				q["disabled"] = int64(1)
			}
			if e := a.syncLink(q, str(q, "current_id")); e != nil {
				panic(e)
			}
			a.audit(p.ID, "project.disabled", parts[2]+":"+encode(b))
			send(w, 200, row{"ok": true})
			return
		}
	case "audit":
		if r.Method == "GET" {
			send(w, 200, a.rows("SELECT * FROM audit ORDER BY created_at DESC,rowid DESC LIMIT 500"))
			return
		}
	}
	fail(404, "not_found", "Endpoint not found")
}
