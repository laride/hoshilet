package platform

// Sum each stored deployment once, including historical/staged releases and
// retired releases awaiting GC, even when their project has been deleted.
// These are logical file bytes, not filesystem allocation or staging/DB bytes.
const projectUsageCTE = `WITH usage AS (
 SELECT project_id,
 coalesce(sum(CASE WHEN status='ready' THEN size ELSE 0 END),0) retained_bytes,
 coalesce(sum(CASE WHEN status='retired' THEN size ELSE 0 END),0) retired_bytes,
 coalesce(sum(CASE WHEN status IN ('ready','retired') THEN size ELSE 0 END),0) used_bytes,
 sum(CASE WHEN status='ready' THEN 1 ELSE 0 END) retained_versions
 FROM deployments GROUP BY project_id
)`

func (a *App) namespaceUsage() []row {
	return a.rows(projectUsageCTE + `
 SELECT n.id,n.name,n.mode,n.created_at,
 (SELECT count(*) FROM members m WHERE m.namespace_id=n.id) member_count,
 coalesce(sum(CASE WHEN p.deleted=0 THEN 1 ELSE 0 END),0) project_count,
 coalesce(sum(u.retained_bytes),0) retained_bytes,
 coalesce(sum(u.retired_bytes),0) retired_bytes,
 coalesce(sum(u.used_bytes),0) used_bytes
 FROM namespaces n LEFT JOIN projects p ON p.namespace_id=n.id
 LEFT JOIN usage u ON u.project_id=p.id GROUP BY n.id ORDER BY used_bytes DESC,n.name`)
}
func (a *App) namespaceUsageDetail(nid string) row {
	n := a.one("SELECT id,name,mode,created_at FROM namespaces WHERE id=?", nid)
	if n == nil {
		fail(404, "not_found", "Namespace not found")
	}
	projects := a.rows(projectUsageCTE+`
 SELECT p.id,p.name,p.disabled,p.deleted,p.current_id,
 coalesce(u.retained_bytes,0) retained_bytes,coalesce(u.retired_bytes,0) retired_bytes,
 coalesce(u.used_bytes,0) used_bytes,coalesce(u.retained_versions,0) retained_versions
 FROM projects p LEFT JOIN usage u ON u.project_id=p.id
 WHERE p.namespace_id=? AND (p.deleted=0 OR coalesce(u.used_bytes,0)>0)
 ORDER BY used_bytes DESC,p.name`, nid)
	members := a.rows(`SELECT m.user_id,m.role,u.username,u.gitlab_id,u.status FROM members m JOIN users u ON u.id=m.user_id WHERE m.namespace_id=? ORDER BY m.role,u.username`, nid)
	return row{"namespace": n, "projects": projects, "members": members}
}
