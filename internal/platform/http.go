package platform

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
)

type apiError struct {
	Status        int
	Code, Message string
}

func fail(status int, code, msg string) { panic(apiError{status, code, msg}) }
func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func body(r *http.Request, v any) {
	d := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		fail(400, "invalid_json", e.Error())
	}
	if d.Decode(new(any)) != io.EOF {
		fail(400, "invalid_json", "Expected one JSON value")
	}
}

var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

func validSlug(s string) bool   { return slugRE.MatchString(s) && s != "." && s != ".." }
func normalize(s string) string { return strings.ToLower(s) }
func mustSlug(s string) {
	if !validSlug(s) {
		fail(400, "invalid_name", "Use 1–63 lowercase ASCII letters, digits, dots, underscores or hyphens; start with a letter or digit")
	}
}
func (a *App) Handler() http.Handler {
	encodedMessage := encodeServerMessage(a.C.ServerMessage)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if e := a.DB.PingContext(r.Context()); e != nil {
			send(w, 503, row{"status": "unhealthy"})
			return
		}
		send(w, 200, row{"status": "ok"})
	})
	mux.HandleFunc("GET /auth/login", a.login)
	mux.HandleFunc("GET /auth/callback", a.callback)
	mux.HandleFunc("POST /api/v1/oidc/exchange", a.exchange)
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/api/v1/deploy" {
			p := a.authenticate(r)
			if p.User != nil && str(p.User, "status") != "approved" {
				fail(403, "not_admitted", "Account is not on the whitelist")
			}
			a.deploy(w, r, p)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		p := a.authenticate(r)
		if p.User != nil && str(p.User, "status") != "approved" && r.URL.Path != "/api/v1/me" && r.URL.Path != "/api/v1/logout" {
			fail(403, "not_admitted", "Account is not on the whitelist")
		}
		a.api(w, r, p)
	})
	mux.Handle("/", http.NotFoundHandler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w = &messageWriter{ResponseWriter: w, encoded: encodedMessage}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		defer func() {
			if v := recover(); v != nil {
				if e, ok := v.(apiError); ok {
					send(w, e.Status, row{"error": row{"code": e.Code, "message": e.Message}})
				} else {
					slog.Error("request failed", "error", fmt.Sprint(v))
					send(w, 500, row{"error": row{"code": "internal", "message": "Internal error; consult server logs"}})
				}
			}
		}()
		mux.ServeHTTP(w, r)
	})
}
func (a *App) api(w http.ResponseWriter, r *http.Request, p principal) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/"), "/")
	method := r.Method
	if len(parts) == 1 {
		switch parts[0] {
		case "me":
			if method == "GET" {
				if p.User == nil {
					send(w, 200, row{"credential": p.Credential["kind"], "project_id": p.Credential["target"]})
					return
				}
				send(w, 200, row{"user": p.User, "admin": p.Admin, "content_url": a.C.ContentURL})
				return
			}
		case "logout":
			if method == "POST" {
				a.exec("UPDATE credentials SET revoked=1 WHERE id=?", p.ID)
				cookie(w, "__Host-hoshilet-session", "", -1)
				send(w, 200, row{"ok": true})
				return
			}
		case "namespaces":
			if method == "GET" {
				if p.User == nil {
					fail(403, "forbidden", "User required")
				}
				scope := r.URL.Query().Get("scope")
				if scope != "" && scope != "member" && scope != "all" {
					fail(400, "invalid_scope", "Expected member or all namespace scope")
				}
				if scope == "all" && !p.Admin {
					fail(403, "forbidden", "Platform administrator required for all namespaces")
				}
				var result []row
				if scope == "all" {
					result = a.rows("SELECT n.*,coalesce(m.role,'admin') role FROM namespaces n LEFT JOIN members m ON m.namespace_id=n.id AND m.user_id=? ORDER BY n.name", p.User["id"])
				} else {
					result = a.rows("SELECT n.*,m.role FROM namespaces n JOIN members m ON m.namespace_id=n.id WHERE m.user_id=? ORDER BY n.name", p.User["id"])
				}
				if str(p.Credential, "kind") == "key" && str(p.Credential, "scope") != "account" {
					filtered := []row{}
					for _, n := range result {
						if str(p.Credential, "scope") == "namespace" && n["id"] == p.Credential["target"] {
							filtered = append(filtered, n)
						}
					}
					result = filtered
				}
				send(w, 200, result)
				return
			}
		case "requests":
			a.session(p)
			if method == "GET" {
				send(w, 200, a.rows("SELECT * FROM requests WHERE user_id=? ORDER BY created_at DESC", p.User["id"]))
				return
			}
			if method == "POST" {
				var b struct {
					Name string `json:"name"`
				}
				body(r, &b)
				mustSlug(b.Name)
				if a.one("SELECT id FROM namespaces WHERE name=?", b.Name) != nil {
					fail(409, "name_taken", "Namespace exists")
				}
				if a.one("SELECT id FROM requests WHERE user_id=? AND name=? AND status='pending'", p.User["id"], b.Name) != nil {
					fail(409, "pending", "Request already pending")
				}
				rid := id()
				a.exec("INSERT INTO requests(id,user_id,name,created_at) VALUES(?,?,?,?)", rid, p.User["id"], b.Name, now())
				send(w, 201, row{"id": rid})
				return
			}
		case "keys":
			a.session(p)
			if method == "GET" {
				send(w, 200, a.rows("SELECT id,name,scope,target,expires_at,revoked,last_used_at,created_at FROM credentials WHERE user_id=? AND kind='key' ORDER BY created_at DESC", p.User["id"]))
				return
			}
			if method == "POST" {
				var b struct {
					Name, Scope, Target string
					Days                int
				}
				body(r, &b)
				if b.Scope == "" {
					b.Scope = "account"
				}
				switch b.Scope {
				case "account":
					b.Target = ""
				case "namespace":
					a.namespace(p, b.Target, false)
				case "project":
					a.project(p, b.Target, false)
				default:
					fail(400, "invalid_scope", "Expected account, namespace or project")
				}
				if b.Days == 0 {
					b.Days = 90
				}
				if b.Days < 1 || b.Days > 365 {
					fail(400, "invalid_expiry", "Key expiry must be 1–365 days")
				}
				secret, kid := "hoshilet_key_"+id()+id(), id()
				a.exec("INSERT INTO credentials(id,hash,user_id,kind,scope,target,name,expires_at,created_at) VALUES(?,?,?,'key',?,?,?,?,?)", kid, digest(secret), p.User["id"], b.Scope, b.Target, b.Name, now()+int64(b.Days)*86400, now())
				a.audit(p.ID, "key.create", kid)
				send(w, 201, row{"id": kid, "token": secret})
				return
			}
		}
	}
	if parts[0] == "keys" && len(parts) == 2 && method == "DELETE" {
		a.session(p)
		if a.one("SELECT id FROM credentials WHERE id=? AND user_id=? AND kind='key'", parts[1], p.User["id"]) == nil {
			fail(404, "not_found", "Key not found")
		}
		a.exec("UPDATE credentials SET revoked=1 WHERE id=? AND user_id=? AND kind='key'", parts[1], p.User["id"])
		a.audit(p.ID, "key.revoke", parts[1])
		send(w, 200, row{"ok": true})
		return
	}
	if parts[0] == "namespaces" && len(parts) >= 3 {
		nid := parts[1]
		switch parts[2] {
		case "projects":
			if method == "GET" {
				a.namespace(p, nid, false)
				send(w, 200, a.rows("SELECT * FROM projects WHERE namespace_id=? AND deleted=0 ORDER BY name", nid))
				return
			}
			if method == "POST" {
				a.session(p)
				a.namespace(p, nid, true)
				var b struct {
					Name string `json:"name"`
					Mode string `json:"mode"`
				}
				body(r, &b)
				q := a.createProject(nid, b.Name, b.Mode, p.ID)
				send(w, 201, q)
				return
			}
		case "members":
			a.session(p)
			a.namespace(p, nid, true)
			if method == "GET" {
				send(w, 200, a.rows("SELECT m.*,u.username,u.gitlab_id FROM members m JOIN users u ON u.id=m.user_id WHERE namespace_id=?", nid))
				return
			}
			if method == "PUT" {
				var b struct {
					GitlabID int64  `json:"gitlab_id"`
					Role     string `json:"role"`
				}
				body(r, &b)
				if b.Role == "" {
					b.Role = "member"
				}
				if b.Role != "admin" && b.Role != "member" {
					fail(400, "invalid_role", "Invalid role")
				}
				u := a.one("SELECT * FROM users WHERE gitlab_id=?", b.GitlabID)
				if u == nil {
					fail(404, "not_found", "User must sign in first")
				}
				a.protectAdmin(nid, str(u, "id"), b.Role)
				a.exec("INSERT INTO members VALUES(?,?,?) ON CONFLICT(namespace_id,user_id) DO UPDATE SET role=excluded.role", nid, u["id"], b.Role)
				a.audit(p.ID, "member.set", nid+":"+str(u, "id")+":"+b.Role)
				send(w, 200, row{"ok": true})
				return
			}
			if method == "DELETE" && len(parts) == 4 {
				if a.one("SELECT user_id FROM members WHERE namespace_id=? AND user_id=?", nid, parts[3]) == nil {
					fail(404, "not_found", "Member not found")
				}
				a.protectAdmin(nid, parts[3], "")
				a.exec("DELETE FROM members WHERE namespace_id=? AND user_id=?", nid, parts[3])
				a.audit(p.ID, "member.remove", nid+":"+parts[3])
				send(w, 200, row{"ok": true})
				return
			}
		}
	}
	if parts[0] == "projects" && len(parts) >= 2 {
		q := a.project(p, parts[1], false)
		pid := str(q, "id")
		if len(parts) == 2 {
			if method == "GET" {
				send(w, 200, q)
				return
			}
			if method == "DELETE" {
				a.session(p)
				a.project(p, pid, true)
				a.removeProject(q, p.ID)
				send(w, 200, row{"ok": true})
				return
			}
		}
		if len(parts) >= 3 {
			switch parts[2] {
			case "deployments":
				if method == "GET" {
					send(w, 200, a.rows("SELECT d.*,(SELECT MAX(activated_at) FROM activations WHERE deployment_id=d.id) last_activated_at FROM deployments d WHERE project_id=? AND status='ready' ORDER BY uploaded_at DESC,id DESC", pid))
					return
				}
				if method == "DELETE" && len(parts) == 4 {
					a.session(p)
					a.project(p, pid, true)
					if str(q, "current_id") == parts[3] {
						fail(409, "current_version", "Cannot delete current deployment")
					}
					d := a.one("SELECT status FROM deployments WHERE id=? AND project_id=?", parts[3], pid)
					if d == nil {
						fail(404, "not_found", "Deployment not found")
					}
					if str(d, "status") != "ready" {
						fail(409, "invalid_state", "Deployment is already retired or purged")
					}
					a.exec("UPDATE deployments SET status='retired',gc_after=? WHERE id=? AND project_id=? AND status='ready'", now()+int64(a.C.GCSeconds), parts[3], pid)
					a.audit(p.ID, "deployment.retire", parts[3])
					send(w, 200, row{"ok": true})
					return
				}
			case "activations":
				if method == "GET" {
					send(w, 200, a.rows("SELECT * FROM activations WHERE project_id=? ORDER BY activated_at DESC,rowid DESC", pid))
					return
				}
			case "activate":
				if method == "POST" {
					var b struct {
						DeploymentID string `json:"deployment_id"`
						Revision     int64  `json:"revision"`
					}
					body(r, &b)
					a.activate(q, b.DeploymentID, b.Revision, p.ID)
					send(w, 200, a.one("SELECT * FROM projects WHERE id=?", pid))
					return
				}
			case "trusts":
				a.session(p)
				a.project(p, pid, true)
				if method == "GET" {
					send(w, 200, a.rows("SELECT t.*,i.url issuer_url FROM trusts t JOIN issuers i ON i.id=t.issuer_id WHERE project_id=?", pid))
					return
				}
				if method == "POST" {
					var b struct {
						IssuerID            string   `json:"issuer_id"`
						RepositoryID        string   `json:"repository_id"`
						Branches            []string `json:"branches"`
						Tags                []string `json:"tags"`
						RequireRefProtected bool     `json:"require_ref_protected"`
						AllowMR             bool     `json:"allow_mr_pipelines"`
					}
					body(r, &b)
					if !digits(b.RepositoryID) || len(b.Branches) > 100 || len(b.Tags) > 100 || a.one("SELECT id FROM issuers WHERE id=? AND enabled=1", b.IssuerID) == nil {
						fail(400, "invalid_trust", "Valid issuer, numeric repository ID and at most 100 patterns per allowlist required")
					}
					for _, patterns := range [][]string{b.Branches, b.Tags} {
						for _, pattern := range patterns {
							if strings.TrimSpace(pattern) == "" || len(pattern) > 1024 {
								fail(400, "invalid_ref_pattern", "Invalid branch or tag pattern")
							}
						}
					}
					if b.Branches == nil {
						b.Branches = []string{}
					}
					if b.Tags == nil {
						b.Tags = []string{}
					}
					tid := id()
					a.exec("INSERT INTO trusts(id,project_id,issuer_id,repository_id,branches,tags,require_ref_protected,allow_mr_pipelines) VALUES(?,?,?,?,?,?,?,?)", tid, pid, b.IssuerID, b.RepositoryID, encode(b.Branches), encode(b.Tags), b.RequireRefProtected, b.AllowMR)
					a.audit(p.ID, "trust.create", tid)
					send(w, 201, row{"id": tid})
					return
				}
				if method == "DELETE" && len(parts) == 4 {
					if a.one("SELECT id FROM trusts WHERE id=? AND project_id=?", parts[3], pid) == nil {
						fail(404, "not_found", "Trust not found")
					}
					a.exec("DELETE FROM trusts WHERE id=? AND project_id=?", parts[3], pid)
					a.audit(p.ID, "trust.delete", parts[3])
					send(w, 200, row{"ok": true})
					return
				}
			}
		}
	}
	if parts[0] == "uploads" && len(parts) == 2 && method == "GET" {
		u := a.one("SELECT * FROM uploads WHERE id=?", parts[1])
		if u == nil {
			fail(404, "not_found", "Upload not found")
		}
		a.project(p, str(u, "project_id"), false)
		send(w, 200, u)
		return
	}
	if parts[0] == "issuers" && method == "GET" {
		a.session(p)
		send(w, 200, a.rows("SELECT id,url,enabled FROM issuers"))
		return
	}
	if parts[0] == "admin" {
		a.platformAdmin(p)
		a.adminAPI(w, r, p, parts)
		return
	}
	fail(404, "not_found", "Endpoint not found")
}
func (a *App) protectAdmin(nid, uid, role string) {
	if role == "admin" {
		return
	}
	m := a.one("SELECT role FROM members WHERE namespace_id=? AND user_id=?", nid, uid)
	if str(m, "role") == "admin" && num(a.one("SELECT count(*) n FROM members WHERE namespace_id=? AND role='admin'", nid), "n") == 1 {
		fail(409, "last_admin", "Cannot remove the last namespace admin")
	}
}
func (a *App) createProject(nid, name, mode, actor string) row {
	n := a.one("SELECT * FROM namespaces WHERE id=?", nid)
	if n == nil {
		fail(404, "not_found", "Namespace not found")
	}
	if mode == "" {
		mode = "projects"
	}
	if mode != "projects" && mode != "root" {
		fail(400, "invalid_mode", "Expected root or projects")
	}
	if mode == "root" {
		name = ""
	} else {
		mustSlug(name)
	}
	if num(a.one("SELECT count(*) n FROM projects WHERE namespace_id=? AND deleted=0", nid), "n") > 0 && str(n, "mode") != mode {
		fail(409, "mode_conflict", "Namespace already uses the other mode")
	}
	if q := a.one("SELECT * FROM projects WHERE namespace_id=? AND name=? AND deleted=0", nid, name); q != nil {
		fail(409, "name_taken", "Project exists")
	}
	pid := id()
	tx, e := a.DB.Begin()
	if e != nil {
		panic(e)
	}
	defer tx.Rollback()
	if _, e = tx.Exec("UPDATE namespaces SET mode=? WHERE id=?", mode, nid); e != nil {
		panic(e)
	}
	if _, e = tx.Exec("INSERT INTO projects(id,namespace_id,name,created_at) VALUES(?,?,?,?)", pid, nid, name, now()); e != nil {
		panic(e)
	}
	if e = tx.Commit(); e != nil {
		panic(e)
	}
	a.audit(actor, "project.create", pid)
	return a.one("SELECT p.*,n.name namespace_name,n.mode FROM projects p JOIN namespaces n ON n.id=p.namespace_id WHERE p.id=?", pid)
}
