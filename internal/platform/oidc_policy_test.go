package platform

import (
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBranchWildcardAndProtectedPolicy(t *testing.T) {
	c := jwt.MapClaims{"project_id": "42", "job_project_id": "42", "ref_type": "branch", "ref": "main", "pipeline_source": "push"}
	if matchClaims(c, "42", []string{"all"}, nil, false, false) {
		t.Fatal("literal all acted as a wildcard")
	}
	if !matchClaims(c, "42", []string{"*"}, nil, false, false) {
		t.Fatal("star did not match main")
	}
	c["ref"] = "all"
	if !matchClaims(c, "42", []string{"all"}, nil, false, false) {
		t.Fatal("literal all branch rejected")
	}
	c["ref"] = "main"
	for _, test := range []struct {
		value any
		want  bool
	}{{nil, false}, {false, false}, {"false", false}, {1, false}, {"1", false}, {"TRUE", false}, {true, true}, {"true", true}} {
		t.Run(fmt.Sprintf("%T-%v", test.value, test.value), func(t *testing.T) {
			c["ref_protected"] = test.value
			if got := matchClaims(c, "42", []string{"*"}, nil, false, true); got != test.want {
				t.Fatalf("protected claim %v: got %v", test.value, got)
			}
		})
	}
	delete(c, "ref_protected")
	if !matchClaims(c, "42", []string{"main"}, nil, false, false) {
		t.Fatal("optional protection rejected missing claim")
	}
	c["pipeline_source"] = "merge_request_event"
	c["ref_protected"] = true
	if matchClaims(c, "42", []string{"*"}, nil, false, true) {
		t.Fatal("protected bypassed MR policy")
	}
	if !matchClaims(c, "42", []string{"*"}, nil, true, true) {
		t.Fatal("protected same-repo MR rejected")
	}
	c["ref_type"] = "tag"
	if matchClaims(c, "42", []string{"*"}, nil, true, true) {
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
	decode(t, call(a, "POST", route, "session2", row{"issuer_id": "ci", "repository_id": "42", "branches": []string{"release/*"}}), 201)
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

func TestRefAllowlists(t *testing.T) {
	for _, tc := range []struct {
		name, kind, ref string
		branches, tags  []string
		want            bool
	}{
		{"empty", "branch", "main", nil, nil, false},
		{"branch glob", "branch", "release/1/stable", []string{"release/*"}, nil, true},
		{"anchored", "branch", "pre-release/1", []string{"release/*"}, nil, false},
		{"branch mismatch", "branch", "main", []string{"release/*"}, nil, false},
		{"tag glob", "tag", "v1.2.3", nil, []string{"v*"}, true},
		{"zero chars", "tag", "v", nil, []string{"v*"}, true},
		{"tag star", "tag", "release/1", nil, []string{"*"}, true},
		{"exact tag", "tag", "v1.0", nil, []string{"v1.0"}, true},
		{"literal dot", "tag", "v1x0", nil, []string{"v1.0"}, false},
		{"literal question", "branch", "release/a", []string{"release/?"}, nil, false},
		{"multiple stars", "tag", "v1-rc2", nil, []string{"v*-rc*"}, true},
		{"tag mismatch", "tag", "nightly", nil, []string{"v*"}, false},
		{"branch cannot grant tag", "tag", "main", []string{"*"}, nil, false},
		{"tag cannot grant branch", "branch", "v1", nil, []string{"*"}, false},
		{"unknown type", "other", "v1", []string{"*"}, []string{"*"}, false},
		{"empty ref", "tag", "", nil, []string{"*"}, false},
		{"case sensitive", "tag", "V1", nil, []string{"v*"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := jwt.MapClaims{"project_id": "42", "ref_type": tc.kind, "ref": tc.ref}
			if got := matchClaims(claims, "42", tc.branches, tc.tags, false, false); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	for _, change := range []jwt.MapClaims{
		{"project_id": "43"}, {"job_project_id": "43"}, {"source_project_id": "43"},
		{"ref_protected": false}, {"pipeline_source": "merge_request_event"},
	} {
		c := jwt.MapClaims{"project_id": "42", "ref_type": "tag", "ref": "v1", "ref_protected": true}
		for k, v := range change {
			c[k] = v
		}
		if matchClaims(c, "42", nil, []string{"v*"}, true, true) {
			t.Fatalf("tag bypassed policy: %v", change)
		}
	}
}

func TestTrustEmptyAndTagAllowlistsAPI(t *testing.T) {
	a := setup(t)
	q := a.createProject("ns", "tags", "projects", "test")
	a.exec("INSERT INTO issuers VALUES('ci','https://ci.test','https://ci.test/keys',1)")
	route := "projects/" + str(q, "id") + "/trusts"
	for _, input := range []row{
		{"tags": []string{"v*"}},
		{"branches": []string{}, "tags": []string{}},
		{},
		{"branches": []string{"release/*"}, "tags": []string{"*"}},
	} {
		input["issuer_id"] = "ci"
		input["repository_id"] = "42"
		result := decode(t, call(a, "POST", route, "session2", input), 201)
		stored := a.one("SELECT * FROM trusts WHERE id=?", result["id"])
		for _, field := range []string{"branches", "tags"} {
			want := input[field]
			if want == nil {
				want = []string{}
			}
			if str(stored, field) != encode(want) {
				t.Fatalf("%s: %v", field, stored)
			}
		}
	}
	for _, field := range []string{"branches", "tags"} {
		for _, values := range [][]string{{""}, {"   "}, {strings.Repeat("x", 1025)}, make([]string, 101)} {
			decode(t, call(a, "POST", route, "session2", row{"issuer_id": "ci", "repository_id": "42", field: values}), 400)
		}
	}
}

func TestSchema4TagMigration(t *testing.T) {
	a := setup(t)
	q := a.createProject("ns", "migrate", "projects", "test")
	a.exec("INSERT INTO issuers VALUES('ci','https://ci.test','https://ci.test/keys',1)")
	a.exec("INSERT INTO trusts(id,project_id,issuer_id,repository_id,branches) VALUES('old',?,'ci','42','[\"main\"]')", q["id"])
	a.exec("ALTER TABLE trusts DROP COLUMN tags")
	a.exec("PRAGMA user_version=4")
	c := a.C
	a.Close()
	reopened, err := Open(c)
	if err != nil {
		t.Fatal(err)
	}
	a.DB, a.lock = reopened.DB, reopened.lock
	stored := a.one("SELECT * FROM trusts WHERE id='old'")
	if str(stored, "branches") != `["main"]` || str(stored, "tags") != "[]" {
		t.Fatal("migration changed existing permissions", stored)
	}
	if num(a.one("PRAGMA user_version"), "user_version") != 5 {
		t.Fatal("schema version not upgraded")
	}
	a.Close()
	reopened, err = Open(c)
	if err != nil {
		t.Fatal(err)
	}
	a.DB, a.lock = reopened.DB, reopened.lock
}
