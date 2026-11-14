package platform

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"hoshilet/internal/archive"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "extract" {
		if e := archive.Worker(os.Args[2:]); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func setup(t *testing.T) *App {
	t.Helper()
	c := Config{DataDir: t.TempDir(), BaseURL: "https://manage.test", ContentURL: "https://sites.test", GitLabURL: "https://gitlab.test", MaxUploads: 4, MaxUploadsPerPrincipal: 2, UploadTimeoutSeconds: 10, AdminIDs: []int64{1}, MaxArchiveBytes: 1 << 20, ExtractionSeconds: 10, GCSeconds: 1, WorkerMemoryMB: 2048}
	a, e := Open(c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { a.Close() })
	for i := 1; i <= 3; i++ {
		uid := fmt.Sprint(i)
		a.exec("INSERT INTO users(id,gitlab_id,username,personal_ns,status) VALUES(?,?,?,?,?)", uid, i, "user"+uid, "ns", "approved")
		a.exec("INSERT INTO credentials(id,hash,user_id,kind,expires_at,created_at) VALUES(?,?,?,'session',?,?)", "session"+uid, digest("session"+uid), uid, now()+3600, now())
	}
	a.exec("INSERT INTO namespaces(id,name,created_at) VALUES('ns','alice',?)", now())
	a.exec("INSERT INTO members VALUES('ns','2','admin'),('ns','3','member')")
	return a
}
func call(a *App, method, path, token string, b any) *httptest.ResponseRecorder {
	var r io.Reader
	if b != nil {
		r = strings.NewReader(encode(b))
	}
	req := httptest.NewRequest(method, "https://manage.test/api/v1/"+path, r)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	return w
}
func decode(t *testing.T, w *httptest.ResponseRecorder, expected int) row {
	t.Helper()
	if w.Code != expected {
		t.Fatalf("status %d want %d: %s", w.Code, expected, w.Body.String())
	}
	var v row
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func key(t *testing.T, a *App, user, scope, target string) string {
	return str(decode(t, call(a, "POST", "keys", "session"+user, row{"Scope": scope, "Target": target, "Name": "test", "Days": 1}), 201), "token")
}
func makeZIP() []byte {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, _ := w.Create("index.html")
	f.Write([]byte("hello"))
	w.Close()
	return b.Bytes()
}
func upload(a *App, token, query, idem string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "https://manage.test/api/v1/deploy?"+query, bytes.NewReader(makeZIP()))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/zip")
	r.Header.Set("Idempotency-Key", idem)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func TestRolesKeysAndAutoCreation(t *testing.T) {
	a := setup(t)
	decode(t, call(a, "POST", "namespaces/ns/projects", "session3", row{"name": "bad"}), 403)
	k := key(t, a, "2", "namespace", "ns")
	u := decode(t, upload(a, k, "project=docs", "auto-create-one"), 201)
	pid := str(u, "project_id")
	q := a.one("SELECT * FROM projects WHERE id=?", pid)
	if str(q, "current_id") == "" {
		t.Fatal("not activated")
	}
	p := filepath.Join(a.C.DataDir, "public", "alice", "docs")
	target, e := os.Readlink(p)
	if e != nil || strings.Contains(p, "~") {
		t.Fatal(target, e)
	}
	b, e := os.ReadFile(filepath.Join(p, "index.html"))
	if e != nil || string(b) != "hello" {
		t.Fatal(e, string(b))
	}
	same := decode(t, upload(a, k, "project=docs", "auto-create-one"), 200)
	if same["id"] != u["id"] {
		t.Fatal("non-idempotent")
	}
	mk := key(t, a, "3", "account", "")
	decode(t, upload(a, mk, "project=other", "member-create-denied"), 403)
	decode(t, upload(a, mk, "project=docs", "member-publish-okay"), 201)
	pk := key(t, a, "2", "project", pid)
	decode(t, upload(a, pk, "project=other", "project-scope-denied"), 400)
	decode(t, call(a, "POST", "projects/"+pid+"/trusts", "session3", row{}), 403)
	decode(t, call(a, "DELETE", "namespaces/ns/members/3", "session2", nil), 200)
	decode(t, upload(a, mk, "project=docs", "removed-member-denied"), 403)
	decode(t, call(a, "DELETE", "namespaces/ns/members/2", "session2", nil), 409)
	decode(t, call(a, "DELETE", "projects/"+pid, "session2", nil), 200)
	decode(t, call(a, "POST", "namespaces/ns/projects", "session2", row{"name": "docs"}), 201)
	decode(t, upload(a, pk, "", "deleted-project-denied"), 404)
}
func TestRootAndRetention(t *testing.T) {
	a := setup(t)
	p := decode(t, call(a, "POST", "namespaces/ns/projects", "session2", row{"mode": "root"}), 201)
	pid := str(p, "id")
	decode(t, call(a, "POST", "namespaces/ns/projects", "session2", row{"name": "conflict"}), 409)
	a.exec("UPDATE settings SET max_versions=1")
	k := key(t, a, "2", "project", pid)
	first := decode(t, upload(a, k, "", "root-first-upload"), 201)
	decode(t, upload(a, k, "activate=false", "staged-at-limit-denied"), 409)
	second := decode(t, upload(a, k, "", "root-second-upload"), 201)
	if num(a.one("SELECT count(*) n FROM deployments WHERE status='ready'"), "n") != 1 {
		t.Fatal("retention not enforced")
	}
	if a.one("SELECT status FROM deployments WHERE id=?", first["deployment_id"])["status"] != "retired" {
		t.Fatal("old deployment retained")
	}
	if a.one("SELECT current_id FROM projects WHERE id=?", pid)["current_id"] != second["deployment_id"] {
		t.Fatal("new deployment not active")
	}
	a.exec("UPDATE deployments SET gc_after=0 WHERE status='retired'")
	if e := a.Collect(); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(a.C.DataDir, "releases", pid, str(first, "deployment_id"))); !os.IsNotExist(e) {
		t.Fatal("old deployment not collected")
	}
	decode(t, call(a, "DELETE", "projects/"+pid, "session2", nil), 200)
	decode(t, call(a, "POST", "namespaces/ns/projects", "session2", row{"name": "child"}), 201)
}
func TestRollbackRevisionAndRecovery(t *testing.T) {
	a := setup(t)
	k := key(t, a, "2", "namespace", "ns")
	first := decode(t, upload(a, k, "project=docs", "first-deployment"), 201)
	pid := str(first, "project_id")
	second := decode(t, upload(a, k, "project=docs", "second-deployment"), 201)
	q := a.one("SELECT * FROM projects WHERE id=?", pid)
	decode(t, call(a, "POST", "projects/"+pid+"/activate", k, row{"deployment_id": first["deployment_id"], "revision": 0}), 409)
	decode(t, call(a, "POST", "projects/"+pid+"/activate", k, row{"deployment_id": first["deployment_id"], "revision": q["revision"]}), 200)
	if num(a.one("SELECT count(*) n FROM activations WHERE project_id=?", pid), "n") != 3 {
		t.Fatal("activation history incomplete")
	}
	a.exec("INSERT INTO publications VALUES(?,?,?,?,?)", pid, second["deployment_id"], first["deployment_id"], "recovery-test", now())
	if e := a.recover(); e != nil {
		t.Fatal(e)
	}
	if a.one("SELECT current_id FROM projects WHERE id=?", pid)["current_id"] != second["deployment_id"] {
		t.Fatal("intent not recovered")
	}
	if e := a.recover(); e != nil {
		t.Fatal(e)
	}
	if num(a.one("SELECT count(*) n FROM activations"), "n") != 4 {
		t.Fatal("recovery duplicated activation")
	}
}
func TestSessionCSRFAndRevocation(t *testing.T) {
	a := setup(t)
	r := httptest.NewRequest("POST", "https://manage.test/api/v1/requests", strings.NewReader(`{"name":"team"}`))
	r.AddCookie(&http.Cookie{Name: "__Host-hoshilet-session", Value: "session2"})
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	decode(t, w, 403)
	k := key(t, a, "2", "account", "")
	a.exec("UPDATE credentials SET revoked=1 WHERE hash=?", digest(k))
	decode(t, call(a, "GET", "namespaces", k, nil), 401)
}
func TestNamespaceRequestsAndSettings(t *testing.T) {
	a := setup(t)
	q := decode(t, call(a, "POST", "requests", "session2", row{"name": "team"}), 201)
	decode(t, call(a, "POST", "admin/requests/"+str(q, "id"), "session1", row{"approve": true}), 200)
	decode(t, call(a, "POST", "admin/requests/"+str(q, "id"), "session1", row{"approve": true}), 409)
	decode(t, call(a, "PUT", "admin/settings", "session2", row{"max_versions": 2, "max_bytes": 100}), 403)
	decode(t, call(a, "PUT", "admin/settings", "session1", row{"max_versions": 2, "max_bytes": 100}), 200)
}
func TestMRClaimPolicy(t *testing.T) {
	base := jwt.MapClaims{"project_id": "42", "job_project_id": "42", "ref_type": "branch", "ref": "feature", "pipeline_source": "merge_request_event"}
	if matchClaims(base, "42", []string{"*"}, false, false) {
		t.Fatal("MR enabled unexpectedly")
	}
	if !matchClaims(base, "42", []string{"feature"}, true, false) {
		t.Fatal("same-project MR rejected")
	}
	for _, change := range []jwt.MapClaims{{"job_project_id": "43"}, {"project_id": "43"}, {"source_project_id": "43"}, {"ref_type": "tag"}, {"job_project_id": ""}, {"ref": "main"}} {
		c := jwt.MapClaims{}
		for k, v := range base {
			c[k] = v
		}
		for k, v := range change {
			c[k] = v
		}
		if matchClaims(c, "42", []string{"feature"}, true, false) {
			t.Fatalf("accepted %v", change)
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestOIDCExchangeAndPolicyRevocation(t *testing.T) {
	a := setup(t)
	q := a.createProject("ns", "docs", "projects", "test")
	a.exec("INSERT INTO issuers VALUES('gitlab','https://ci.test','https://ci.test/keys',1)")
	a.exec("INSERT INTO trusts(id,project_id,issuer_id,repository_id,branches) VALUES('trust',?,'gitlab','42','[\"*\"]')", q["id"])
	private, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	old := httpClient
	defer func() { httpClient = old }()
	httpClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://ci.test/keys" {
			t.Fatal("unexpected JWKS fetch")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(encode(row{"keys": []row{{"kty": "RSA", "kid": "k", "n": base64.RawURLEncoding.EncodeToString(private.N.Bytes()), "e": "AQAB"}}})))}, nil
	})}
	claims := jwt.MapClaims{"iss": "https://ci.test", "aud": a.C.BaseURL, "exp": now() + 600, "iat": now(), "nbf": now() - 1, "jti": "one", "project_id": "42", "job_project_id": "42", "ref_type": "branch", "ref": "feature", "pipeline_source": "merge_request_event"}
	exchange := func(c jwt.MapClaims, idem string) *httptest.ResponseRecorder {
		tkn := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
		tkn.Header["kid"] = "k"
		signed, _ := tkn.SignedString(private)
		r := httptest.NewRequest("POST", "https://manage.test/api/v1/oidc/exchange", strings.NewReader(encode(row{"token": signed, "project_id": q["id"]})))
		r.Header.Set("Idempotency-Key", idem)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		return w
	}
	// An enabled trust in another project cannot grant MR access to this one.
	other := a.createProject("ns", "other", "projects", "test")
	a.exec("INSERT INTO trusts(id,project_id,issuer_id,repository_id,branches,allow_mr_pipelines) VALUES('other',?,'gitlab','42','[\"*\"]',1)", other["id"])
	// Nor may MR permission be taken from one rule and protection from another.
	a.exec("INSERT INTO trusts(id,project_id,issuer_id,repository_id,branches,allow_mr_pipelines,require_ref_protected) VALUES('strict',?,'gitlab','42','[\"*\"]',1,1)", q["id"])
	decode(t, exchange(claims, "exchange-idempotency"), 403)
	a.exec("DELETE FROM trusts WHERE id='strict'")
	decode(t, exchange(claims, "exchange-idempotency"), 403)
	a.exec("UPDATE trusts SET allow_mr_pipelines=1 WHERE id='trust'")
	a.exec("UPDATE trusts SET require_ref_protected=1 WHERE id='trust'")
	decode(t, exchange(claims, "exchange-idempotency"), 403)
	claims["ref_protected"] = "true"
	v := decode(t, exchange(claims, "exchange-idempotency"), 200)
	same := decode(t, exchange(claims, "exchange-idempotency"), 200)
	if v["access_token"] != same["access_token"] {
		t.Fatal("exchange not idempotent")
	}
	decode(t, exchange(claims, "different-idempotency"), 409)
	token := str(v, "access_token")
	decode(t, call(a, "GET", "projects/"+str(q, "id"), token, nil), 200)
	a.exec("INSERT INTO trusts(id,project_id,issuer_id,repository_id,branches,allow_mr_pipelines) VALUES('fallback',?,'gitlab','42','[\"*\"]',1)", q["id"])
	a.exec("UPDATE trusts SET allow_mr_pipelines=0 WHERE id='trust'")
	decode(t, call(a, "GET", "projects/"+str(q, "id"), token, nil), 403)
	claims["aud"] = "https://wrong.test"
	claims["jti"] = "two"
	decode(t, exchange(claims, "exchange-second"), 401)
}
func TestCorruptUploadLeavesCurrent(t *testing.T) {
	a := setup(t)
	k := key(t, a, "2", "namespace", "ns")
	v := decode(t, upload(a, k, "project=docs", "valid-first-archive"), 201)
	r := httptest.NewRequest("POST", "https://manage.test/api/v1/deploy?project=docs", strings.NewReader("not zip"))
	r.Header.Set("Authorization", "Bearer "+k)
	r.Header.Set("Content-Type", "application/zip")
	r.Header.Set("Idempotency-Key", "bad-archive-upload")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	decode(t, w, 422)
	if a.one("SELECT current_id FROM projects WHERE id=?", v["project_id"])["current_id"] != v["deployment_id"] {
		t.Fatal("bad archive changed current")
	}
}

func TestBackupRestore(t *testing.T) {
	a := setup(t)
	k := key(t, a, "2", "namespace", "ns")
	v := decode(t, upload(a, k, "project=docs", "backup-source-deploy"), 201)
	file := filepath.Join(t.TempDir(), "backup.tar.gz")
	if e := a.Backup(file); e != nil {
		t.Fatal(e)
	}
	c := a.C
	c.DataDir = filepath.Join(t.TempDir(), "restored")
	if e := Restore(c, file); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(c.DataDir, "public", "alice", "docs", "index.html"))
	if e != nil || string(b) != "hello" {
		t.Fatal(e, string(b))
	}
	restored, e := Open(c)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	if restored.one("SELECT current_id FROM projects WHERE id=?", v["project_id"])["current_id"] != v["deployment_id"] {
		t.Fatal("backup metadata mismatch")
	}
	if e = Restore(c, file); e == nil {
		t.Fatal("restore overwrote existing directory")
	}
}

func TestOAuthLoginAdmissionAndIdentity(t *testing.T) {
	a := setup(t)
	a.C.ClientID = "client"
	a.C.ClientSecret = "secret"

	old := httpClient
	defer func() { httpClient = old }()
	username := "first-name"
	httpClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if !a.mu.TryLock() {
			t.Error("OAuth network request held global lock")
		} else {
			a.mu.Unlock()
		}
		var content string
		switch r.URL.Path {
		case "/oauth/token":
			r.ParseForm()
			if r.Form.Get("code_verifier") != "test-verifier" {
				t.Fatal("PKCE verifier missing")
			}
			content = `{"access_token":"provider-token","token_type":"Bearer"}`
		case "/api/v4/user":
			content = encode(row{"id": 99, "username": username})
		default:
			t.Fatalf("unexpected OAuth request %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(content))}, nil
	})}
	run := func(state string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "https://manage.test/auth/callback?state="+state+"&code=code", nil)
		r.AddCookie(&http.Cookie{Name: "__Host-hoshilet-oauth", Value: "expected.test-verifier"})
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		return w
	}
	decode(t, run("wrong"), 401)
	w := run("expected")
	if w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	u := a.one("SELECT * FROM users WHERE gitlab_id=99")
	if str(u, "status") != "pending" || str(u, "personal_ns") != "" {
		t.Fatal("unapproved login provisioned a namespace", u)
	}
	approved := decode(t, call(a, "PATCH", "admin/users/"+str(u, "id"), "session1", row{"status": "approved"}), 200)
	nid := str(approved, "personal_ns")
	if a.one("SELECT name FROM namespaces WHERE id=?", nid)["name"] != "first-name" {
		t.Fatal("namespace missing")
	}
	username = "renamed"
	w = run("expected")
	if w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	if a.one("SELECT name FROM namespaces WHERE id=?", nid)["name"] != "first-name" {
		t.Fatal("namespace renamed with user")
	}
	a.exec("UPDATE users SET status='disabled' WHERE gitlab_id=99")
	if run("expected").Code != 303 {
		t.Fatal("disabled user should reach status page")
	}
}

func TestConcurrentPublishAndRevocation(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(fmt.Sprint("revoke=", revoke), func(t *testing.T) {
			a := setup(t)
			k := key(t, a, "2", "namespace", "ns")
			initial := decode(t, upload(a, k, "project=docs", "concurrency-initial"), 201)
			reader, writer := io.Pipe()
			req := httptest.NewRequest("POST", "https://manage.test/api/v1/deploy?project=docs", reader)
			req.Header.Set("Authorization", "Bearer "+k)
			req.Header.Set("Content-Type", "application/zip")
			req.Header.Set("Idempotency-Key", "slow-concurrent-upload")
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { defer close(done); a.Handler().ServeHTTP(w, req) }()
			defer writer.Close()
			deadline := time.Now().Add(5 * time.Second)
			for a.one("SELECT id FROM uploads WHERE idem='slow-concurrent-upload'") == nil {
				if time.Now().After(deadline) {
					t.Fatal("upload did not start")
				}
				time.Sleep(time.Millisecond)
			}
			expected := 409
			current := initial["deployment_id"]
			if revoke {
				a.exec("UPDATE credentials SET revoked=1 WHERE hash=?", digest(k))
				expected = 401
			} else {
				fresh := decode(t, upload(a, k, "project=docs", "fast-concurrent-upload"), 201)
				current = fresh["deployment_id"]
			}
			writer.Write(makeZIP())
			writer.Close()
			<-done
			decode(t, w, expected)
			if a.one("SELECT current_id FROM projects WHERE id=?", initial["project_id"])["current_id"] != current {
				t.Fatal("stale upload replaced current")
			}
		})
	}
}
