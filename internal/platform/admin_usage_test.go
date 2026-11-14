package platform

import (
	"encoding/json"
	"testing"
)

func TestAdminNamespaceUsage(t *testing.T) {
	a := setup(t)
	p := a.createProject("ns", "live", "projects", "test")
	deleted := a.createProject("ns", "gone", "projects", "test")
	a.createProject("ns", "zero", "projects", "test")
	a.exec("UPDATE projects SET deleted=1 WHERE id=?", deleted["id"])
	a.exec("INSERT INTO namespaces(id,name,created_at) VALUES('other','other',?),('empty','empty',?)", now(), now())
	other := a.createProject("other", "", "root", "test")
	for _, d := range []struct {
		id, pid, status string
		size            int
	}{{"current", str(p, "id"), "ready", 10}, {"history", str(p, "id"), "ready", 20}, {"retired", str(p, "id"), "retired", 30}, {"purged", str(p, "id"), "purged", 1000}, {"deleted-pending", str(deleted, "id"), "retired", 40}, {"other-current", str(other, "id"), "ready", 70}} {
		a.exec("INSERT INTO deployments(id,project_id,uploaded_at,size,files,status) VALUES(?,?,?,?,1,?)", d.id, d.pid, now(), d.size, d.status)
	}
	a.exec("UPDATE projects SET current_id='current' WHERE id=?", p["id"])
	w := call(a, "GET", "admin/namespaces", "session1", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var rows []row
	json.Unmarshal(w.Body.Bytes(), &rows)
	if len(rows) != 3 {
		t.Fatal(rows)
	}
	for _, r := range rows {
		switch r["id"] {
		case "ns":
			if num(r, "used_bytes") != 100 || num(r, "retained_bytes") != 30 || num(r, "retired_bytes") != 70 || num(r, "project_count") != 2 || num(r, "member_count") != 2 {
				t.Fatal(r)
			}
		case "empty":
			if num(r, "used_bytes") != 0 || num(r, "project_count") != 0 {
				t.Fatal(r)
			}
		case "other":
			if num(r, "used_bytes") != 70 {
				t.Fatal(r)
			}
		}
	}
	d := decode(t, call(a, "GET", "admin/namespaces/ns", "session1", nil), 200)
	projects := d["projects"].([]any)
	if len(projects) != 3 {
		t.Fatal(projects)
	}
	total := int64(0)
	for _, p := range projects {
		total += num(p.(map[string]any), "used_bytes")
	}
	if total != 100 {
		t.Fatal(total)
	}
	members := d["members"].([]any)
	if len(members) != 2 {
		t.Fatal(members)
	}
	for _, m := range members {
		r := m.(map[string]any)
		if r["username"] == nil || r["gitlab_id"] == nil || r["role"] == nil || r["status"] == nil {
			t.Fatal(r)
		}
	}
	decode(t, call(a, "GET", "admin/namespaces/missing", "session1", nil), 404)
	for _, path := range []string{"admin/namespaces", "admin/namespaces/ns"} {
		decode(t, call(a, "GET", path, "session2", nil), 403)
		decode(t, call(a, "GET", path, "session3", nil), 403)
		decode(t, call(a, "GET", path, key(t, a, "1", "account", ""), nil), 403)
	}
	a.exec("UPDATE deployments SET status='purged' WHERE id='deleted-pending'")
	d = decode(t, call(a, "GET", "admin/namespaces/ns", "session1", nil), 200)
	if len(d["projects"].([]any)) != 2 {
		t.Fatal("purged deleted project should disappear")
	}
}
