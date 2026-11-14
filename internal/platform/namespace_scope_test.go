package platform

import (
	"encoding/json"
	"testing"
)

func TestNamespaceScopes(t *testing.T) {
	a := setup(t)
	a.exec("INSERT INTO namespaces(id,name,created_at) VALUES('mine','mine',?),('joined','joined',?)", now(), now())
	a.exec("INSERT INTO members VALUES('mine','1','admin'),('joined','1','member')")
	read := func(path, token string) []row {
		t.Helper()
		w := call(a, "GET", path, token, nil)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var result []row
		if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil {
			t.Fatal(e)
		}
		return result
	}
	for _, path := range []string{"namespaces", "namespaces?scope=member"} {
		rows := read(path, "session1")
		if len(rows) != 2 {
			t.Fatal(rows)
		}
		for _, r := range rows {
			if r["id"] == "ns" {
				t.Fatal("foreign namespace in own list")
			}
			if r["id"] == "joined" && r["role"] != "member" {
				t.Fatal("platform role replaced membership", r)
			}
		}
	}
	if len(read("namespaces?scope=all", "session1")) != 3 {
		t.Fatal("all scope missing namespace")
	}
	if len(read("namespaces", "session2")) != 1 {
		t.Fatal("regular user scope changed")
	}
	decode(t, call(a, "GET", "namespaces?scope=all", "session2", nil), 403)
	decode(t, call(a, "GET", "namespaces?scope=invalid", "session1", nil), 400)
	k := key(t, a, "1", "namespace", "joined")
	rows := read("namespaces?scope=all", k)
	if len(rows) != 1 || rows[0]["id"] != "joined" {
		t.Fatal("key scope escaped", rows)
	}
}
