package platform

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIServerMessages(t *testing.T) {
	if os.Getenv("HOSHILET_TEST_CLI") != "1" {
		t.Skip("requires local HTTP")
	}
	root, _ := filepath.Abs("../..")
	for _, client := range []string{"python", "npm"} {
		for _, status := range []int{200, 400, 500, 302} {
			t.Run(fmt.Sprintf("%s-%d", client, status), func(t *testing.T) {
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set(serverMessageHeader, base64.StdEncoding.EncodeToString([]byte("Hello\n服务器消息")))
					w.WriteHeader(status)
					if status == 200 {
						w.Write([]byte("[]"))
					} else if status == 500 {
						w.Write([]byte("not JSON"))
					} else {
						w.Write([]byte(`{"error":{"code":"denied","message":"request denied"}}`))
					}
				}))
				defer s.Close()
				args := []string{"--json", "deployments", "list", "--project-id", "project"}
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
				cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "cli/python/src"), "HOSHILET_SERVER="+s.URL, "HOSHILET_API_KEY=test", "COLUMNS=120")
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				out, e := cmd.Output()
				if (e == nil) != (status == 200) {
					t.Fatalf("exit mismatch: %v %s", e, stderr.String())
				}
				if status == 200 && !json.Valid(out) {
					t.Fatal("stdout is not JSON", string(out))
				}
				if status != 200 && len(out) != 0 {
					t.Fatal("error polluted stdout")
				}
				lines := strings.Split(stderr.String(), "\n")
				if len(lines) < 4 || len(lines[0]) != 100 || len(lines[3]) != 100 || lines[1] != "Hello" || lines[2] != "服务器消息" {
					t.Fatalf("bad message block: %q", stderr.String())
				}
			})
		}
	}
}
