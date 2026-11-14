package platform

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func zipEntries(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, content := range files {
		f, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.Write([]byte(content)); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func sendArchive(a *App, token, query, idem string, data []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "https://manage.test/api/v1/deploy?"+query, bytes.NewReader(data))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/zip")
	r.Header.Set("Idempotency-Key", idem)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func TestEmptyDeploymentRejected(t *testing.T) {
	a := setup(t)
	k := key(t, a, "2", "namespace", "ns")
	initial := decode(t, upload(a, k, "project=site", "initial-file-version"), 201)
	v := decode(t, sendArchive(a, k, "project=site", "directories-only-upload", zipEntries(t, map[string]string{"assets/": ""})), 422)
	if v["error"].(map[string]any)["code"] != "empty_deployment" {
		t.Fatal(v)
	}
	if a.one("SELECT current_id FROM projects WHERE id=?", initial["project_id"])["current_id"] != initial["deployment_id"] {
		t.Fatal("empty archive changed current")
	}
	if num(a.one("SELECT count(*) n FROM deployments"), "n") != 1 {
		t.Fatal("empty archive retained")
	}
	ds, _ := os.ReadDir(filepath.Join(a.C.DataDir, "staging"))
	if len(ds) != 0 {
		t.Fatal("staging leaked")
	}
	decode(t, sendArchive(a, k, "project=site", "zero-byte-file-upload", zipEntries(t, map[string]string{"empty.txt": ""})), 201)
}
func TestBackupPreservesAllDirectories(t *testing.T) {
	for _, onlyDirs := range []bool{false, true} {
		t.Run(fmt.Sprint(onlyDirs), func(t *testing.T) {
			a := setup(t)
			q := a.createProject("ns", "site", "projects", "test")
			did := id()
			root := filepath.Join(a.C.DataDir, "releases", str(q, "id"), did)
			if e := os.MkdirAll(filepath.Join(root, "assets/empty"), 0755); e != nil {
				t.Fatal(e)
			}
			if !onlyDirs {
				os.WriteFile(filepath.Join(root, "index.html"), []byte("ok"), 0644)
			}
			a.exec("INSERT INTO deployments(id,project_id,uploaded_at,size,files) VALUES(?,?,?,0,0)", did, q["id"], now())
			a.activate(q, did, 0, "test")
			backup := filepath.Join(t.TempDir(), "backup.tgz")
			if e := a.Backup(backup); e != nil {
				t.Fatal(e)
			}
			c := a.C
			c.DataDir = filepath.Join(t.TempDir(), "restored")
			if e := Restore(c, backup); e != nil {
				t.Fatal(e)
			}
			if st, e := os.Stat(filepath.Join(c.DataDir, "public/alice/site/assets/empty")); e != nil || !st.IsDir() {
				t.Fatal("empty directory lost", e)
			}
		})
	}
}
func TestRestoreFailureDoesNotInstallTarget(t *testing.T) {
	a := setup(t)
	k := key(t, a, "2", "namespace", "ns")
	u := decode(t, upload(a, k, "project=site", "restore-failure-base"), 201)
	root := filepath.Join(a.C.DataDir, "releases", str(u, "project_id"), str(u, "deployment_id"))
	os.RemoveAll(root)
	bad := filepath.Join(t.TempDir(), "bad.tgz")
	if e := a.Backup(bad); e != nil {
		t.Fatal(e)
	}
	c := a.C
	c.DataDir = filepath.Join(t.TempDir(), "restored")
	for i := 0; i < 2; i++ {
		e := Restore(c, bad)
		if e == nil || strings.Contains(e.Error(), "non-existent data_dir") {
			t.Fatal("failed restore cannot be retried", e)
		}
		if _, e = os.Lstat(c.DataDir); !os.IsNotExist(e) {
			t.Fatal("partial target left behind", e)
		}
	}
	os.MkdirAll(root, 0755)
	os.WriteFile(filepath.Join(root, "index.html"), []byte("fixed"), 0644)
	good := filepath.Join(t.TempDir(), "good.tgz")
	if e := a.Backup(good); e != nil {
		t.Fatal(e)
	}
	if e := Restore(c, good); e != nil {
		t.Fatal(e)
	}
}
func TestCollectReturnsDatabaseErrors(t *testing.T) {
	a := setup(t)
	a.DB.Close()
	if e := a.Collect(); e == nil {
		t.Fatal("expected error")
	}
}
func TestSQLiteReplacementConnection(t *testing.T) {
	a := setup(t)
	a.DB.SetMaxIdleConns(0)
	a.DB.SetMaxIdleConns(1)
	for p, want := range map[string]int{"foreign_keys": 1, "busy_timeout": 5000, "synchronous": 2} {
		var got int
		if e := a.DB.QueryRow("PRAGMA " + p).Scan(&got); e != nil || got != want {
			t.Fatalf("%s = %d, want %d (%v)", p, got, want, e)
		}
	}
	if _, e := a.DB.Exec("INSERT INTO members VALUES('missing','missing','member')"); e == nil {
		t.Fatal("FK not enforced")
	}
	tx, e := a.DB.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	var n int
	if e = tx.QueryRow("SELECT 1").Scan(&n); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if e = a.DB.QueryRowContext(ctx, "SELECT 1").Scan(&n); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("single connection invariant", e)
	}
}
func TestSettingsRequireEveryField(t *testing.T) {
	a := setup(t)
	a.exec("UPDATE settings SET max_versions=17")
	for _, missing := range []string{"max_versions", "max_bytes"} {
		for _, null := range []bool{false, true} {
			b := row{"max_versions": 10, "max_bytes": 1000}
			if null {
				b[missing] = nil
			} else {
				delete(b, missing)
			}
			decode(t, call(a, "PUT", "admin/settings", "session1", b), 400)
			if num(a.one("SELECT max_versions FROM settings"), "max_versions") != 17 {
				t.Fatal("missing field changed settings")
			}
		}
	}
	decode(t, call(a, "PUT", "admin/settings", "session1", row{"max_versions": 10, "max_bytes": 1000}), 200)
}
func TestDeleteStateAndMissingResources(t *testing.T) {
	a := setup(t)
	q := a.createProject("ns", "site", "projects", "test")
	pid := str(q, "id")
	for _, status := range []string{"purged", "retired"} {
		a.exec("INSERT INTO deployments(id,project_id,uploaded_at,size,files,status,gc_after) VALUES(?,?,?,0,0,?,123)", status, pid, now(), status)
		decode(t, call(a, "DELETE", "projects/"+pid+"/deployments/"+status, "session2", nil), 409)
		d := a.one("SELECT * FROM deployments WHERE id=?", status)
		if str(d, "status") != status || num(d, "gc_after") != 123 {
			t.Fatal("invalid state transition", d)
		}
	}
	for _, p := range []string{"projects/" + pid + "/deployments/missing", "projects/" + pid + "/trusts/missing", "keys/missing", "namespaces/ns/members/missing"} {
		decode(t, call(a, "DELETE", p, "session2", nil), 404)
	}
}
func slowUpload(t *testing.T, a *App, token, idem string) (*io.PipeWriter, <-chan *httptest.ResponseRecorder) {
	t.Helper()
	rd, wr := io.Pipe()
	r := httptest.NewRequest("POST", "https://manage.test/api/v1/deploy?project=site", rd)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/zip")
	r.Header.Set("Idempotency-Key", idem)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); a.Handler().ServeHTTP(w, r); done <- w }()
	deadline := time.Now().Add(2 * time.Second)
	for a.one("SELECT id FROM uploads WHERE idem=?", idem) == nil {
		if time.Now().After(deadline) {
			wr.Close()
			t.Fatal("slow upload not started")
		}
		time.Sleep(time.Millisecond)
	}
	t.Cleanup(func() { wr.Close() })
	return wr, done
}
func TestPerUserUploadLimitAndTimeout(t *testing.T) {
	a := setup(t)
	a.C.MaxUploadsPerPrincipal = 1
	a.C.UploadTimeoutSeconds = 1
	k := key(t, a, "2", "namespace", "ns")
	otherKey := key(t, a, "2", "account", "")
	memberKey := key(t, a, "3", "namespace", "ns")
	a.createProject("ns", "site", "projects", "test")
	_, done := slowUpload(t, a, k, "slow-limited-upload")
	decode(t, upload(a, otherKey, "project=site", "same-user-second-key"), 429)
	decode(t, upload(a, memberKey, "project=site", "different-user-allowed"), 201)
	select {
	case w := <-done:
		decode(t, w, 408)
	case <-time.After(3 * time.Second):
		t.Fatal("slow upload never expired")
	}
	decode(t, upload(a, otherKey, "project=site", "slot-released-after-timeout"), 201)
}
func TestWhitelistLifecycleWithoutNetwork(t *testing.T) {
	a := setup(t)
	old := httpClient
	defer func() { httpClient = old }()
	httpClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		t.Error("unexpected external request", r.URL)
		return nil, errors.New("no network")
	})}
	u := decode(t, call(a, "POST", "admin/users", "session1", row{"gitlab_id": 99}), 201)
	if str(u, "status") != "approved" || str(u, "personal_ns") != "" {
		t.Fatal(u)
	}
	a.exec("INSERT INTO users(id,gitlab_id,username) VALUES('pending',100,'newbie')")
	a.exec("INSERT INTO credentials(id,hash,user_id,kind,expires_at,created_at) VALUES('pending-session',?,'pending','session',?,?)", digest("pending-session"), now()+1000, now())
	decode(t, call(a, "GET", "me", "pending-session", nil), 200)
	decode(t, call(a, "GET", "namespaces", "pending-session", nil), 403)
	decode(t, call(a, "POST", "keys", "pending-session", row{}), 403)
	approved := decode(t, call(a, "PATCH", "admin/users/pending", "session1", row{"status": "approved"}), 200)
	if str(approved, "personal_ns") == "" {
		t.Fatal("namespace not created after approval")
	}
	decode(t, call(a, "PATCH", "admin/users/pending", "session1", row{"status": "approved"}), 200)
	if num(a.one("SELECT count(*) n FROM namespaces WHERE name='newbie'"), "n") != 1 {
		t.Fatal("namespace duplicated")
	}
	k := keyForPending(t, a)
	decode(t, upload(a, k, "project=site", "local-whitelist-deploy"), 201)
	decode(t, call(a, "PATCH", "admin/users/pending", "session1", row{"status": "disabled"}), 200)
	decode(t, call(a, "GET", "namespaces", "pending-session", nil), 403)
	decode(t, call(a, "GET", "namespaces", k, nil), 401)
	decode(t, call(a, "PATCH", "admin/users/1", "session1", row{"status": "disabled"}), 409)
	decode(t, call(a, "PATCH", "admin/users/absent", "session1", row{"status": "approved"}), 404)
}
func keyForPending(t *testing.T, a *App) string {
	return str(decode(t, call(a, "POST", "keys", "pending-session", row{"scope": "account"}), 201), "token")
}
func TestWhitelistRevokedDuringUpload(t *testing.T) {
	a := setup(t)
	k := key(t, a, "2", "namespace", "ns")
	a.createProject("ns", "site", "projects", "test")
	wr, done := slowUpload(t, a, k, "disable-during-upload")
	decode(t, call(a, "PATCH", "admin/users/2", "session1", row{"status": "disabled"}), 200)
	wr.Write(makeZIP())
	wr.Close()
	decode(t, <-done, 401)
	if num(a.one("SELECT count(*) n FROM deployments"), "n") != 0 {
		t.Fatal("disabled user committed upload")
	}
}

func TestMaintenanceSurvivesRepeatedDatabaseFailure(t *testing.T) {
	a := setup(t)
	a.DB.Close()
	ticks := make(chan time.Time)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); a.maintenanceLoop(stop, ticks) }()
	for i := 0; i < 2; i++ {
		select {
		case ticks <- time.Now():
		case <-done:
			t.Fatal("maintenance exited on database error")
		case <-time.After(time.Second):
			t.Fatal("maintenance stopped accepting ticks")
		}
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("maintenance did not stop")
	}
}
func TestOIDCQuotaCannotBeBypassedByNewTokens(t *testing.T) {
	a := setup(t)
	a.C.MaxUploadsPerPrincipal = 1
	q := a.createProject("ns", "site", "projects", "test")
	a.exec("INSERT INTO issuers VALUES('issuer','https://ci.test','https://ci.test/keys',1)")
	for _, tid := range []string{"t1", "t2"} {
		a.exec("INSERT INTO trusts(id,project_id,issuer_id,repository_id,branches) VALUES(?,?,'issuer','42','[\"*\"]')", tid, q["id"])
	}
	release := a.acquireUpload(principal{Credential: row{"trust_id": "t1"}})
	defer release()
	func() {
		defer func() {
			v := recover()
			e, ok := v.(apiError)
			if !ok || e.Status != 429 {
				t.Fatalf("expected shared quota, got %v", v)
			}
		}()
		undo := a.acquireUpload(principal{Credential: row{"trust_id": "t2"}})
		undo()
		t.Fatal("second token bypassed quota")
	}()
}

func TestMRTrustRevokedDuringUpload(t *testing.T) {
	a := setup(t)
	q := a.createProject("ns", "site", "projects", "test")
	a.exec("INSERT INTO issuers VALUES('issuer','https://ci.test','https://ci.test/keys',1)")
	a.exec("INSERT INTO trusts(id,project_id,issuer_id,repository_id,branches,allow_mr_pipelines) VALUES('mr-trust',?,'issuer','42','[\"*\"]',1)", q["id"])
	a.exec("INSERT INTO credentials(id,hash,kind,scope,target,trust_id,mr,expires_at,created_at) VALUES('mr-credential',?,'oidc','project',?,'mr-trust',1,?,?)", digest("mr-token"), q["id"], now()+600, now())
	wr, done := slowUpload(t, a, "mr-token", "mr-trust-revoked-upload")
	decode(t, call(a, "DELETE", "projects/"+str(q, "id")+"/trusts/mr-trust", "session2", nil), 200)
	wr.Write(makeZIP())
	wr.Close()
	decode(t, <-done, 403)
	if num(a.one("SELECT count(*) n FROM deployments"), "n") != 0 {
		t.Fatal("revoked MR trust published")
	}
}
func TestNoGlobalMRSetting(t *testing.T) {
	a := setup(t)
	s := decode(t, call(a, "GET", "admin/settings", "session1", nil), 200)
	if _, ok := s["allow_mr_pipelines"]; ok {
		t.Fatal("global MR setting still present")
	}
	decode(t, call(a, "PUT", "admin/settings", "session1", row{"max_versions": 10, "max_bytes": 1000, "allow_mr_pipelines": true}), 400)
}
