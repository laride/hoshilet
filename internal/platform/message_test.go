package platform

import (
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerMessageValidation(t *testing.T) {
	for _, s := range []string{"hello\n世界", "", strings.Repeat("a", 512), strings.Repeat("界", 170) + "ab"} {
		if _, e := validateServerMessage(s); e != nil {
			t.Fatal(e)
		}
	}
	for _, s := range []string{strings.Repeat("界", 171), strings.Repeat("a", 513), "\x1b[31mred", "\u009b31mred", "hello\rworld", "a\tb", "\x7f", "\x00", string([]byte{0xff})} {
		if _, e := validateServerMessage(s); e == nil {
			t.Fatalf("accepted invalid message %q", s)
		}
	}
	s, e := validateServerMessage("first\r\nsecond")
	if e != nil || s != "first\nsecond" {
		t.Fatal(s, e)
	}
}
func TestServerMessageSuccessOnly(t *testing.T) {
	a := setup(t)
	a.C.ServerMessage = "Maintenance at midnight\n维护提示"
	w := call(a, "GET", "me", "session1", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	decoded, e := base64.StdEncoding.DecodeString(w.Header().Get(serverMessageHeader))
	if e != nil || string(decoded) != a.C.ServerMessage {
		t.Fatal(string(decoded), e)
	}
	for _, r := range []*httptest.ResponseRecorder{call(a, "GET", "me", "bad-token", nil), call(a, "GET", "admin/namespaces", "session2", nil), call(a, "GET", "unknown", "session1", nil), call(a, "PUT", "admin/settings", "session1", map[string]any{})} {
		if r.Code < 400 || r.Header().Get(serverMessageHeader) != "" {
			t.Fatal("message on error", r.Code, r.Header())
		}
	}
	for _, path := range []string{"/auth/login", "/missing"} {
		r := httptest.NewRecorder()
		a.Handler().ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Header().Get(serverMessageHeader) != "" {
			t.Fatal("message on redirect/missing")
		}
	}
	a.C.ServerMessage = ""
	if call(a, "GET", "me", "session1", nil).Header().Get(serverMessageHeader) != "" {
		t.Fatal("empty message emitted")
	}
	a.C.ServerMessage = "configured"
	a.DB.Close()
	w = call(a, "GET", "me", "session1", nil)
	if w.Code != 500 || w.Header().Get(serverMessageHeader) != "" {
		t.Fatal("panic response emitted message")
	}
}
