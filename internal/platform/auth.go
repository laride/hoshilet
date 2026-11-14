package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/oauth2"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type principal struct {
	User       row
	Credential row
	ID         string
	Admin      bool
}

func includes(ids []int64, n int64) bool {
	for _, i := range ids {
		if i == n {
			return true
		}
	}
	return false
}

var httpClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}

func getJSON(ctx context.Context, url, token string, out any) error {
	req, e := http.NewRequestWithContext(ctx, "GET", url, nil)
	if e != nil {
		return e
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, e := httpClient.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("identity provider returned %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(out)
}
func (a *App) oauth() *oauth2.Config {
	return &oauth2.Config{ClientID: a.C.ClientID, ClientSecret: a.C.ClientSecret, RedirectURL: a.C.BaseURL + "/auth/callback", Scopes: []string{"read_user"}, Endpoint: oauth2.Endpoint{AuthURL: a.C.GitLabURL + "/oauth/authorize", TokenURL: a.C.GitLabURL + "/oauth/token", AuthStyle: oauth2.AuthStyleInParams}}
}
func cookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age})
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	state, verifier := id(), oauth2.GenerateVerifier()
	cookie(w, "__Host-hoshilet-oauth", state+"."+verifier, 600)
	http.Redirect(w, r, a.oauth().AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}
func (a *App) callback(w http.ResponseWriter, r *http.Request) {
	c, e := r.Cookie("__Host-hoshilet-oauth")
	if e != nil {
		fail(401, "invalid_oauth", "Missing OAuth state")
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 || parts[0] != r.URL.Query().Get("state") {
		fail(401, "invalid_oauth", "Invalid OAuth state")
	}
	cookie(w, "__Host-hoshilet-oauth", "", -1)
	ctx := context.WithValue(r.Context(), oauth2.HTTPClient, httpClient)
	tok, e := a.oauth().Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(parts[1]))
	if e != nil {
		fail(401, "invalid_oauth", "OAuth exchange failed")
	}
	var u struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	if getJSON(ctx, a.C.GitLabURL+"/api/v4/user", tok.AccessToken, &u) != nil || u.ID <= 0 || u.Username == "" {
		fail(403, "not_admitted", "GitLab user does not satisfy admission rules")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	user := a.one("SELECT * FROM users WHERE gitlab_id=?", u.ID)
	if user == nil {
		uid := id()
		a.exec("INSERT INTO users(id,gitlab_id,username) VALUES(?,?,?)", uid, u.ID, u.Username)
		user = a.one("SELECT * FROM users WHERE id=?", uid)
	}
	a.exec("UPDATE users SET username=? WHERE id=?", u.Username, user["id"])
	if includes(a.C.AdminIDs, u.ID) {
		a.exec("UPDATE users SET status='approved' WHERE id=?", user["id"])
	}
	user = a.one("SELECT * FROM users WHERE id=?", user["id"])
	a.ensurePersonalNamespace(user)
	secret := id() + id()
	a.exec("INSERT INTO credentials(id,hash,user_id,kind,expires_at,created_at) VALUES(?,?,?,'session',?,?)", id(), digest(secret), user["id"], now()+86400, now())
	cookie(w, "__Host-hoshilet-session", secret, 86400)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (a *App) authenticate(r *http.Request) principal {
	secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	fromCookie := false
	if secret == "" {
		if c, e := r.Cookie("__Host-hoshilet-session"); e == nil {
			secret = c.Value
			fromCookie = true
		}
	}
	if secret == "" {
		fail(401, "unauthenticated", "Sign in or provide a bearer token")
	}
	c := a.one("SELECT * FROM credentials WHERE hash=? AND revoked=0 AND expires_at>?", digest(secret), now())
	if c == nil {
		fail(401, "invalid_token", "Token expired or revoked")
	}
	if fromCookie && str(c, "kind") != "session" {
		fail(401, "invalid_token", "Invalid session")
	}
	if fromCookie && r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("Origin") != a.C.BaseURL {
		fail(403, "csrf", "Origin check failed")
	}
	p := principal{Credential: c, ID: str(c, "id")}
	if str(c, "kind") == "oidc" {
		a.checkOIDCCredential(c)
		return p
	}
	p.User = a.one("SELECT * FROM users WHERE id=?", c["user_id"])
	if p.User == nil {
		fail(403, "not_admitted", "Account unavailable")
	}
	if str(c, "kind") != "session" && str(p.User, "status") != "approved" {
		fail(403, "not_admitted", "Account is not on the whitelist")
	}
	p.Admin = includes(a.C.AdminIDs, num(p.User, "gitlab_id"))
	a.exec("UPDATE credentials SET last_used_at=? WHERE id=?", now(), p.ID)
	return p
}
func (a *App) session(p principal) {
	if str(p.Credential, "kind") != "session" {
		fail(403, "session_required", "This operation requires a browser session")
	}
}
func (a *App) platformAdmin(p principal) {
	a.session(p)
	if !p.Admin {
		fail(403, "forbidden", "Platform administrator required")
	}
}
func (a *App) namespace(p principal, nid string, admin bool) {
	if p.User == nil {
		fail(403, "forbidden", "User credential required")
	}
	if !p.Admin {
		m := a.one("SELECT role FROM members WHERE namespace_id=? AND user_id=?", nid, p.User["id"])
		if m == nil || (admin && str(m, "role") != "admin") {
			fail(403, "forbidden", "Namespace permission denied")
		}
	}
	if str(p.Credential, "kind") == "key" {
		scope := str(p.Credential, "scope")
		if scope == "project" || (scope == "namespace" && str(p.Credential, "target") != nid) {
			fail(403, "scope_denied", "Key scope excludes namespace")
		}
	}
}
func (a *App) project(p principal, pid string, admin bool) row {
	q := a.one("SELECT p.*,n.name namespace_name,n.mode FROM projects p JOIN namespaces n ON n.id=p.namespace_id WHERE p.id=? AND p.deleted=0", pid)
	if q == nil {
		fail(404, "not_found", "Project not found")
	}
	if str(p.Credential, "kind") == "oidc" {
		a.checkOIDCCredential(p.Credential)
		if admin || str(p.Credential, "target") != pid {
			fail(403, "scope_denied", "OIDC credential only publishes its project")
		}
	} else if str(p.Credential, "kind") == "key" && str(p.Credential, "scope") == "project" {
		if str(p.Credential, "target") != pid {
			fail(403, "scope_denied", "Key scope excludes project")
		}
		copy := p
		copy.Credential = row{"kind": "internal"}
		a.namespace(copy, str(q, "namespace_id"), admin)
	} else {
		a.namespace(p, str(q, "namespace_id"), admin)
	}
	return q
}
func (a *App) recheck(p principal) {
	c := a.one("SELECT * FROM credentials WHERE id=? AND revoked=0 AND expires_at>?", p.ID, now())
	if c == nil {
		fail(401, "invalid_token", "Credential revoked or expired during upload")
	}
	if p.User != nil {
		u := a.one("SELECT * FROM users WHERE id=?", p.User["id"])
		if u == nil || str(u, "status") != "approved" {
			fail(403, "blocked", "Account disabled")
		}
	} else {
		a.checkOIDCCredential(c)
	}
}

// Called only under a.mu, after GitLab identity has been fetched outside the lock.
func (a *App) ensurePersonalNamespace(u row) {
	if str(u, "status") != "approved" || str(u, "personal_ns") != "" || str(u, "username") == "" {
		return
	}
	base := normalize(str(u, "username"))
	if !validSlug(base) {
		base = "user"
	}
	name := base
	for n := 0; a.one("SELECT id FROM namespaces WHERE name=?", name) != nil; n++ {
		suffix := "-" + strconv.FormatInt(num(u, "gitlab_id"), 10)
		if n > 0 {
			suffix += "-" + strconv.Itoa(n)
		}
		prefix := base
		if len(prefix)+len(suffix) > 63 {
			prefix = prefix[:63-len(suffix)]
		}
		name = prefix + suffix
	}
	nid := id()
	tx, e := a.DB.Begin()
	if e != nil {
		panic(e)
	}
	defer tx.Rollback()
	for _, q := range []struct {
		s    string
		args []any
	}{{"INSERT INTO namespaces(id,name,created_at) VALUES(?,?,?)", []any{nid, name, now()}}, {"INSERT INTO members VALUES(?,?,'admin')", []any{nid, u["id"]}}, {"UPDATE users SET personal_ns=? WHERE id=?", []any{nid, u["id"]}}} {
		if _, e = tx.Exec(q.s, q.args...); e != nil {
			panic(e)
		}
	}
	if e = tx.Commit(); e != nil {
		panic(e)
	}
}
