package platform

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLIExtraCAs(t *testing.T) {
	if os.Getenv("HOSHILET_TEST_CLI") != "1" {
		t.Skip("HOSHILET_TEST_CLI=1 enables real localhost HTTPS and CLI subprocess tests")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// Each server has its own CA and leaf, including one with a mismatched hostname.
	var servers []*httptest.Server
	var caFiles []string
	var caBundle []byte
	for i, hostname := range []string{"localhost", "localhost", "localhost", "wrong.example"} {
		caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		ca := &x509.Certificate{
			SerialNumber: big.NewInt(int64(i + 1)), Subject: pkix.Name{CommonName: "CLI test CA"},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
			IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
		}
		caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		ca, err = x509.ParseCertificate(caDER)
		if err != nil {
			t.Fatal(err)
		}
		caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
		caFile := filepath.Join(dir, big.NewInt(int64(i)).String()+".pem")
		if err := os.WriteFile(caFile, caPEM, 0600); err != nil {
			t.Fatal(err)
		}
		caFiles = append(caFiles, caFile)
		if i == 1 || i == 2 {
			caBundle = append(caBundle, caPEM...)
		}
		leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		leaf := &x509.Certificate{
			SerialNumber: big.NewInt(100), Subject: pkix.Name{CommonName: hostname},
			NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, DNSNames: []string{hostname},
			BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature,
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		if hostname == "localhost" {
			leaf.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		}
		leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/projects/test/deployments" || r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("unexpected request: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, "[]")
		}))
		server.Config.ErrorLog = log.New(io.Discard, "", 0)
		server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{leafDER}, PrivateKey: leafKey}}}
		server.StartTLS()
		t.Cleanup(server.Close)
		servers = append(servers, server)
	}
	bundleFile := filepath.Join(dir, "bundle.pem")
	invalidFile := filepath.Join(dir, "invalid.pem")
	for file, content := range map[string][]byte{bundleFile: caBundle, invalidFile: []byte("not a certificate")} {
		if err := os.WriteFile(file, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, client := range []string{"python", "npm"} {
		t.Run(client, func(t *testing.T) {
			run := func(server int, caArgs []string, wantSuccess bool, errorText string) {
				t.Helper()
				args := append([]string{"--json", "--server", servers[server].URL}, caArgs...)
				args = append(args, "--project-id", "test")
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
				cmd.Env = append(os.Environ(),
					"PYTHONPATH="+filepath.Join(root, "cli/python/src"), "HOSHILET_API_KEY=test-key",
					"SSL_CERT_FILE="+caFiles[0], "NODE_EXTRA_CA_CERTS="+caFiles[0], "NODE_TLS_REJECT_UNAUTHORIZED=1")
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				out, err := cmd.Output()
				if wantSuccess {
					if err != nil || strings.TrimSpace(string(out)) != "[]" {
						t.Fatalf("HTTPS CLI failed: %v: %s %s", err, out, stderr.String())
					}
				} else {
					if err == nil || !strings.Contains(stderr.String(), errorText) || !strings.Contains(stderr.String(), `"error"`) {
						t.Fatalf("expected JSON error containing %q: %v: %s %s", errorText, err, out, stderr.String())
					}
				}
			}
			plain := []string{"deployments", "list"}
			// Existing default trust survives, while an unknown CA is rejected.
			run(0, plain, true, "")
			run(1, plain, false, "")
			// Repeated flags accumulate across all command levels.
			repeated := []string{"--extra-ca-certs", caFiles[1], "deployments", "--extra-ca-certs", caFiles[2], "list", "--extra-ca-certs", caFiles[3]}
			for _, server := range []int{0, 1, 2} {
				run(server, repeated, true, "")
			}
			// A trusted CA does not bypass hostname verification.
			run(3, repeated, false, "")
			for _, server := range []int{1, 2} {
				run(server, append(append([]string{}, plain...), "--extra-ca-certs", bundleFile), true, "")
			}
			for _, file := range []string{invalidFile, filepath.Join(dir, "missing.pem")} {
				run(0, append(append([]string{}, plain...), "--extra-ca-certs", file), false, "Cannot load --extra-ca-certs")
			}
		})
	}
}
