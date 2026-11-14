package platform

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLIContract(t *testing.T) {
	if os.Getenv("HOSHILET_TEST_CLI") != "1" {
		t.Skip("HOSHILET_TEST_CLI=1 enables real localhost HTTP and CLI subprocess contract tests")
	}
	a := setup(t)
	a.C.ServerMessage = "CLI contract notice\n服务器公告"
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	root, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	source := t.TempDir()
	os.Mkdir(filepath.Join(source, "empty"), 0755)
	os.WriteFile(filepath.Join(source, "index.html"), []byte("shared CLI contract"), 0644)
	token := key(t, a, "2", "namespace", "ns")
	for _, client := range []string{"python", "npm"} {
		t.Run(client, func(t *testing.T) {
			run := func(args ...string) []byte {
				t.Helper()
				var cmd *exec.Cmd
				if client == "python" {
					python := os.Getenv("HOSHILET_TEST_PYTHON")
					if python == "" {
						python = "python3"
					}
					cmd = exec.Command(python, append([]string{"-m", "hoshilet.cli"}, args...)...)
				} else {
					cmd = exec.Command("node", append([]string{filepath.Join(root, "cli/npm/dist/hoshilet.mjs")}, args...)...)
				}
				cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "cli/python/src"), "HOSHILET_SERVER="+server.URL, "HOSHILET_API_KEY="+token)
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				out, e := cmd.Output()
				if e != nil {
					t.Fatalf("CLI failed: %v: %s", e, stderr.String())
				}
				if strings.Count(stderr.String(), "CLI contract notice") != 1 {
					t.Fatalf("message missing or duplicated: %q", stderr.String())
				}
				return out
			}
			var uploadResult row
			if e = json.Unmarshal(run("--json", "deploy", source, "--project", client), &uploadResult); e != nil {
				t.Fatal(e)
			}
			pid := str(uploadResult, "project_id")
			if pid == "" {
				t.Fatal("missing project")
			}
			run("--json", "deployments", "list", "--project-id", pid)
			run("--json", "activate", str(uploadResult, "deployment_id"), "--project-id", pid)
			b, e := os.ReadFile(filepath.Join(a.C.DataDir, "public", "alice", client, "index.html"))
			if e != nil || string(b) != "shared CLI contract" {
				t.Fatal(e, string(b))
			}
			var staged row
			json.Unmarshal(run("--json", "deploy", source, "--project-id", pid, "--no-activate"), &staged)
			q := a.one("SELECT * FROM projects WHERE id=?", pid)
			if q["current_id"] == staged["deployment_id"] {
				t.Fatal("staged deployment is live")
			}
		})
	}
}

func TestBrowser(t *testing.T) {
	if os.Getenv("HOSHILET_TEST_BROWSER") != "1" {
		t.Skip("HOSHILET_TEST_BROWSER=1 enables actual browser tests")
	}
	a := setup(t)
	a.exec("INSERT INTO members VALUES('ns','1','admin')")
	a.exec("INSERT INTO namespaces(id,name,created_at) VALUES('foreign','foreign-team',?),('joined','joined-team',?)", now(), now())
	a.exec("INSERT INTO members VALUES('foreign','2','admin'),('joined','2','admin'),('joined','1','member')")
	a.exec("INSERT INTO issuers VALUES('browser-ci','https://ci.test','https://ci.test/keys',1)")
	a.exec("INSERT INTO users(id,gitlab_id,username) VALUES('pending-ui',44,'waiting-user')")
	a.exec("INSERT INTO credentials(id,hash,user_id,kind,expires_at,created_at) VALUES('pending-ui-session',?,'pending-ui','session',?,?)", digest("pending-ui-session"), now()+3600, now())
	root, _ := filepath.Abs("../..")
	if _, err := os.Stat(filepath.Join(root, "web/dist/index.html")); err != nil {
		t.Fatal("build the standalone frontend with pnpm --dir web build before browser tests")
	}
	backend := httptest.NewServer(a.Handler())
	defer backend.Close()
	upstream, _ := url.Parse(backend.URL)
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	frontend := http.NewServeMux()
	frontend.Handle("/api/", proxy)
	frontend.Handle("/auth/", proxy)
	frontend.Handle("/healthz", proxy)
	frontend.Handle("/", http.FileServer(http.Dir(filepath.Join(root, "web/dist"))))
	server := httptest.NewServer(frontend)
	defer server.Close()
	zipPath := filepath.Join(t.TempDir(), "site.zip")
	os.WriteFile(zipPath, makeZIP(), 0600)
	cmd := exec.Command("node", filepath.Join(root, "web/tests/browser.mjs"))
	cmd.Env = append(os.Environ(), "HOSHILET_BROWSER_URL="+server.URL, "HOSHILET_BROWSER_ZIP="+zipPath)
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("browser: %v: %s", e, out)
	}
	t.Log(string(out))
}

func TestCLIOIDC(t *testing.T) {
	if os.Getenv("HOSHILET_TEST_CLI") != "1" {
		t.Skip("HOSHILET_TEST_CLI=1 enables real CLI OIDC contract")
	}
	a := setup(t)
	a.C.ServerMessage = "OIDC publishing notice"
	q := a.createProject("ns", "ci-site", "projects", "test")
	a.exec("INSERT INTO issuers VALUES('gitlab','https://ci.test','https://ci.test/keys',1)")
	a.exec("INSERT INTO trusts(id,project_id,issuer_id,repository_id,branches) VALUES('trust',?,'gitlab','42','[\"main\"]')", q["id"])
	private, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	old := httpClient
	defer func() { httpClient = old }()
	httpClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(encode(row{"keys": []row{{"kty": "RSA", "kid": "key", "n": base64.RawURLEncoding.EncodeToString(private.N.Bytes()), "e": "AQAB"}}})))}, nil
	})}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	root, _ := filepath.Abs("../..")
	zipPath := filepath.Join(t.TempDir(), "site.zip")
	os.WriteFile(zipPath, makeZIP(), 0600)
	for _, client := range []string{"python", "npm"} {
		t.Run(client, func(t *testing.T) {
			claims := jwt.MapClaims{"iss": "https://ci.test", "aud": a.C.BaseURL, "exp": now() + 600, "iat": now(), "nbf": now() - 1, "jti": client, "project_id": "42", "job_project_id": "42", "ref_type": "branch", "ref": "main", "pipeline_source": "push"}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
			token.Header["kid"] = "key"
			signed, _ := token.SignedString(private)
			args := []string{"--json", "deploy", zipPath, "--oidc", "--project-id", str(q, "id")}
			var cmd *exec.Cmd
			if client == "python" {
				python := os.Getenv("HOSHILET_TEST_PYTHON")
				if python == "" {
					python = "python3"
				}
				cmd = exec.Command(python, append([]string{"-m", "hoshilet.cli"}, args...)...)
			} else {
				cmd = exec.Command("node", append([]string{filepath.Join(root, "cli/npm/dist/hoshilet.mjs")}, args...)...)
			}
			cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "cli/python/src"), "HOSHILET_SERVER="+server.URL, "HOSHILET_ID_TOKEN="+signed, "HOSHILET_API_KEY=")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, e := cmd.Output()
			if e != nil {
				t.Fatalf("OIDC CLI failed: %v %s", e, stderr.String())
			}
			if strings.Count(stderr.String(), "OIDC publishing notice") != 1 {
				t.Fatalf("message missing or duplicated: %q", stderr.String())
			}
			var v row
			if e = json.Unmarshal(out, &v); e != nil || str(v, "status") != "complete" {
				t.Fatal(e, string(out))
			}
		})
	}
}

func TestSlowHTTPUploadDeadline(t *testing.T) {
	if os.Getenv("HOSHILET_TEST_CLI") != "1" {
		t.Skip("requires loopback")
	}
	a := setup(t)
	a.C.UploadTimeoutSeconds = 1
	a.C.MaxUploadsPerPrincipal = 1
	k := key(t, a, "2", "namespace", "ns")
	a.createProject("ns", "site", "projects", "test")
	s := httptest.NewServer(a.Handler())
	defer s.Close()
	conn, e := net.Dial("tcp", strings.TrimPrefix(s.URL, "http://"))
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "POST /api/v1/deploy?project=site HTTP/1.1\r\nHost: localhost\r\nAuthorization: Bearer %s\r\nContent-Type: application/zip\r\nContent-Length: 100\r\nIdempotency-Key: slow-real-http-body\r\n\r\n", k)
	response, e := http.ReadResponse(bufio.NewReader(conn), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	if response.StatusCode != 408 {
		t.Fatal("expected request timeout", response.StatusCode)
	}
	io.Copy(io.Discard, response.Body)
	deadline := time.Now().Add(time.Second)
	for len(a.workers) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("upload slot leaked")
		}
		time.Sleep(time.Millisecond)
	}
}
