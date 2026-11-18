package platform

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type App struct {
	DB           *sql.DB
	C            Config
	mu           sync.Mutex
	workers      chan struct{}
	slotMu       sync.Mutex
	uploadCounts map[string]int
	lock         *os.File
}
type row = map[string]any

const schema = `
CREATE TABLE IF NOT EXISTS users(id TEXT PRIMARY KEY, gitlab_id INTEGER UNIQUE NOT NULL, username TEXT NOT NULL, personal_ns TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','disabled')));
CREATE TABLE IF NOT EXISTS namespaces(id TEXT PRIMARY KEY, name TEXT UNIQUE NOT NULL, mode TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS members(namespace_id TEXT NOT NULL REFERENCES namespaces(id), user_id TEXT NOT NULL REFERENCES users(id), role TEXT NOT NULL CHECK(role IN ('admin','member')), PRIMARY KEY(namespace_id,user_id));
CREATE TABLE IF NOT EXISTS requests(id TEXT PRIMARY KEY,user_id TEXT NOT NULL REFERENCES users(id),name TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'pending',reason TEXT NOT NULL DEFAULT '',created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS projects(id TEXT PRIMARY KEY,namespace_id TEXT NOT NULL REFERENCES namespaces(id),name TEXT NOT NULL,current_id TEXT NOT NULL DEFAULT '',revision INTEGER NOT NULL DEFAULT 0,disabled INTEGER NOT NULL DEFAULT 0,deleted INTEGER NOT NULL DEFAULT 0,created_at INTEGER NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS active_project_name ON projects(namespace_id,name) WHERE deleted=0;
CREATE TABLE IF NOT EXISTS deployments(id TEXT PRIMARY KEY,project_id TEXT NOT NULL REFERENCES projects(id),uploaded_at INTEGER NOT NULL,size INTEGER NOT NULL,files INTEGER NOT NULL,status TEXT NOT NULL DEFAULT 'ready',gc_after INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS activations(id TEXT PRIMARY KEY,project_id TEXT NOT NULL,deployment_id TEXT NOT NULL,previous_id TEXT NOT NULL,actor TEXT NOT NULL,activated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS publications(project_id TEXT PRIMARY KEY,deployment_id TEXT NOT NULL,previous_id TEXT NOT NULL,actor TEXT NOT NULL,created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS credentials(id TEXT PRIMARY KEY,hash TEXT UNIQUE NOT NULL,user_id TEXT NOT NULL DEFAULT '',kind TEXT NOT NULL,scope TEXT NOT NULL DEFAULT 'account',target TEXT NOT NULL DEFAULT '',name TEXT NOT NULL DEFAULT '',expires_at INTEGER NOT NULL,revoked INTEGER NOT NULL DEFAULT 0,last_used_at INTEGER NOT NULL DEFAULT 0,trust_id TEXT NOT NULL DEFAULT '',mr INTEGER NOT NULL DEFAULT 0,created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS issuers(id TEXT PRIMARY KEY,url TEXT UNIQUE NOT NULL,jwks_url TEXT NOT NULL,enabled INTEGER NOT NULL DEFAULT 1);
CREATE TABLE IF NOT EXISTS trusts(id TEXT PRIMARY KEY,project_id TEXT NOT NULL REFERENCES projects(id),issuer_id TEXT NOT NULL REFERENCES issuers(id),repository_id TEXT NOT NULL,branches TEXT NOT NULL,tags TEXT NOT NULL DEFAULT '[]',require_ref_protected INTEGER NOT NULL DEFAULT 0 CHECK(require_ref_protected IN (0,1)),allow_mr_pipelines INTEGER NOT NULL DEFAULT 0 CHECK(allow_mr_pipelines IN (0,1)));
CREATE TABLE IF NOT EXISTS exchanges(issuer_id TEXT NOT NULL,jti TEXT NOT NULL,idem TEXT NOT NULL,credential_id TEXT NOT NULL,secret TEXT NOT NULL,expires_at INTEGER NOT NULL,PRIMARY KEY(issuer_id,jti));
CREATE TABLE IF NOT EXISTS uploads(id TEXT PRIMARY KEY,project_id TEXT NOT NULL,actor TEXT NOT NULL,idem TEXT NOT NULL,revision INTEGER NOT NULL,activate INTEGER NOT NULL,status TEXT NOT NULL,deployment_id TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT '',created_at INTEGER NOT NULL,UNIQUE(actor,idem));
CREATE TABLE IF NOT EXISTS settings(id INTEGER PRIMARY KEY CHECK(id=1),max_versions INTEGER NOT NULL,max_bytes INTEGER NOT NULL);
INSERT OR IGNORE INTO settings VALUES(1,10,104857600);
CREATE TABLE IF NOT EXISTS audit(id TEXT PRIMARY KEY,actor TEXT NOT NULL,action TEXT NOT NULL,target TEXT NOT NULL,created_at INTEGER NOT NULL);
PRAGMA user_version=5;
`

func Open(c Config) (*App, error) {
	message, err := validateServerMessage(c.ServerMessage)
	if err != nil {
		return nil, err
	}
	c.ServerMessage = message
	if c.MaxUploads == 0 {
		c.MaxUploads = 4
	}
	if c.MaxUploadsPerPrincipal == 0 {
		c.MaxUploadsPerPrincipal = 1
	}
	if c.UploadTimeoutSeconds == 0 {
		c.UploadTimeoutSeconds = 120
	}
	for _, p := range []string{"database", "staging", "releases", "public"} {
		if e := os.MkdirAll(filepath.Join(c.DataDir, p), 0700); e != nil {
			return nil, e
		}
	}
	// Public traversal is necessary for an independent static-server account.
	for _, p := range []string{"", "public", "releases"} {
		if e := os.Chmod(filepath.Join(c.DataDir, p), 0755); e != nil {
			return nil, e
		}
	}
	f, e := os.OpenFile(filepath.Join(c.DataDir, "database/process.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		f.Close()
		return nil, fmt.Errorf("another server or maintenance command is running: %w", e)
	}
	db, e := sql.Open("sqlite", sqliteDSN(filepath.Join(c.DataDir, "database/hoshilet.db")))
	if e != nil {
		f.Close()
		return nil, e
	}
	// A transaction owns the only connection. Inside a transaction use tx.Query/Exec,
	// never a.one/a.rows/a.exec: DB-level helpers would wait for that same connection.
	db.SetMaxOpenConns(1)
	var version int
	if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil {
		db.Close()
		f.Close()
		return nil, e
	}
	if version != 0 && version != 4 && version != 5 {
		db.Close()
		f.Close()
		return nil, fmt.Errorf("database schema %d is incompatible with pre-release schema 5; initialize a new data directory", version)
	}
	if version == 4 {
		if _, e = db.Exec("BEGIN; ALTER TABLE trusts ADD COLUMN tags TEXT NOT NULL DEFAULT '[]'; PRAGMA user_version=5; COMMIT;"); e != nil {
			db.Close()
			f.Close()
			return nil, e
		}
	}
	if _, e = db.Exec("PRAGMA journal_mode=WAL;" + schema); e != nil {
		db.Close()
		f.Close()
		return nil, e
	}
	a := &App{DB: db, C: c, workers: make(chan struct{}, c.MaxUploads), uploadCounts: make(map[string]int), lock: f}
	if e = a.recover(); e != nil {
		a.Close()
		return nil, e
	}
	return a, nil
}
func (a *App) Close() { a.DB.Close(); a.lock.Close() }
func id() string {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func now() int64             { return time.Now().Unix() }
func num(r row, k string) int64 {
	switch v := r[k].(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return 0
}
func str(r row, k string) string { v, _ := r[k].(string); return v }
func query(db interface {
	Query(string, ...any) (*sql.Rows, error)
}, q string, args ...any) ([]row, error) {
	rs, e := db.Query(q, args...)
	if e != nil {
		return nil, e
	}
	defer rs.Close()
	cols, e := rs.Columns()
	if e != nil {
		return nil, e
	}
	out := []row{}
	for rs.Next() {
		v := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range v {
			ptr[i] = &v[i]
		}
		if e = rs.Scan(ptr...); e != nil {
			return nil, e
		}
		r := row{}
		for i, k := range cols {
			if b, ok := v[i].([]byte); ok {
				v[i] = string(b)
			}
			r[k] = v[i]
		}
		out = append(out, r)
	}
	return out, rs.Err()
}
func (a *App) rows(q string, args ...any) []row {
	r, e := query(a.DB, q, args...)
	if e != nil {
		panic(e)
	}
	return r
}
func (a *App) one(q string, args ...any) row {
	r := a.rows(q, args...)
	if len(r) == 0 {
		return nil
	}
	return r[0]
}
func (a *App) exec(q string, args ...any) {
	if _, e := a.DB.Exec(q, args...); e != nil {
		panic(e)
	}
}
func (a *App) audit(actor, action, target string) {
	a.exec("INSERT INTO audit VALUES(?,?,?,?,?)", id(), actor, action, target, now())
}
func encode(v any) string { b, _ := json.Marshal(v); return string(b) }

func sqliteDSN(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	q := url.Values{}
	for _, p := range []string{"foreign_keys(1)", "busy_timeout(5000)", "synchronous(FULL)"} {
		q.Add("_pragma", p)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// errorBoundary is for non-HTTP entry points using the existing DB helpers.
// Convert helper panics into errors so a recoverable DB failure cannot kill the process.
func errorBoundary(err *error) {
	if v := recover(); v != nil {
		if e, ok := v.(error); ok {
			*err = e
		} else {
			*err = fmt.Errorf("operation panicked: %v", v)
		}
	}
}
