package platform

import (
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"net/http/httptest"
	"testing"
)

func TestBranchWildcardAndProtectedPolicy(t *testing.T) {
	c := jwt.MapClaims{"project_id": "42", "job_project_id": "42", "ref_type": "branch", "ref": "main", "pipeline_source": "push"}
	if matchClaims(c, "42", []string{"all"}, false, false) {
		t.Fatal("literal all acted as a wildcard")
	}
	if !matchClaims(c, "42", []string{"*"}, false, false) {
		t.Fatal("star did not match main")
	}
	c["ref"] = "all"
	if !matchClaims(c, "42", []string{"all"}, false, false) {
		t.Fatal("literal all branch rejected")
	}
	c["ref"] = "main"
	for _, test := range []struct {
		value any
		want  bool
	}{{nil, false}, {false, false}, {"false", false}, {1, false}, {"1", false}, {"TRUE", false}, {true, true}, {"true", true}} {
		t.Run(fmt.Sprintf("%T-%v", test.value, test.value), func(t *testing.T) {
			c["ref_protected"] = test.value
			if got := matchClaims(c, "42", []string{"*"}, false, true); got != test.want {
				t.Fatalf("protected claim %v: got %v", test.value, got)
			}
		})
	}
	delete(c, "ref_protected")
	if !matchClaims(c, "42", []string{"main"}, false, false) {
		t.Fatal("optional protection rejected missing claim")
	}
	c["pipeline_source"] = "merge_request_event"
	c["ref_protected"] = true
	if matchClaims(c, "42", []string{"*"}, false, true) {
		t.Fatal("protected bypassed MR policy")
	}
	if !matchClaims(c, "42", []string{"*"}, true, true) {
		t.Fatal("protected same-repo MR rejected")
	}
	c["ref_type"] = "tag"
	if matchClaims(c, "42", []string{"*"}, true, true) {
		t.Fatal("wildcard allowed tags")
	}
}
func TestTrustProtectionAPI(t *testing.T) {
	a := setup(t)
	q := a.createProject("ns", "site", "projects", "test")
	a.exec("INSERT INTO issuers VALUES('ci','https://ci.test','https://ci.test/keys',1)")
	route := "projects/" + str(q, "id") + "/trusts"
	created := decode(t, call(a, "POST", route, "session2", row{"issuer_id": "ci", "repository_id": "42", "branches": []string{"*"}, "require_ref_protected": true, "allow_mr_pipelines": true}), 201)
	if num(a.one("SELECT * FROM trusts WHERE id=?", created["id"]), "require_ref_protected") != 1 {
		t.Fatal("protected requirement not stored")
	}
	if num(a.one("SELECT * FROM trusts WHERE id=?", created["id"]), "allow_mr_pipelines") != 1 {
		t.Fatal("MR policy not stored")
	}
	literal := decode(t, call(a, "POST", route, "session2", row{"issuer_id": "ci", "repository_id": "42", "branches": []string{"all"}}), 201)
	d := a.one("SELECT * FROM trusts WHERE id=?", literal["id"])
	if str(d, "branches") != `["all"]` || num(d, "require_ref_protected") != 0 || num(d, "allow_mr_pipelines") != 0 {
		t.Fatal("literal/default changed", d)
	}
	decode(t, call(a, "POST", route, "session2", row{"issuer_id": "ci", "repository_id": "42", "branches": []string{"release/*"}}), 400)
	decode(t, call(a, "POST", route, "session3", row{"issuer_id": "ci", "repository_id": "42", "branches": []string{"*"}}), 403)
}
func TestBackendDoesNotServeFrontend(t *testing.T) {
	a := setup(t)
	for _, path := range []string{"/", "/index.html", "/assets/app.js"} {
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatalf("backend served frontend path %s: %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 {
		t.Fatal("health endpoint disappeared")
	}
}
