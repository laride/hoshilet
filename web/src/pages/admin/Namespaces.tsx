import { useState } from 'react';
import { ChevronDown, ChevronRight, Globe, RefreshCw, Search } from 'lucide-react';
import { api } from '../../api';
import { formatBytes, useResource } from '../../lib';
import type { NamespaceUsage, NamespaceUsageDetail } from '../../types';
import { Badge, Button, Card, EmptyState, ErrorState, PageHeader, Spinner } from '../../ui';

function NamespaceDetails({ id }: { id: string }) {
  const res = useResource(() => api<NamespaceUsageDetail>(`admin/namespaces/${id}`), [id]);
  if (res.error) return <ErrorState message={res.error} onRetry={res.reload} />;
  if (!res.data) return <Spinner />;
  const { projects, members, namespace } = res.data;
  const statuses = { pending: '待授权', approved: '已授权', disabled: '已停用' };
  return (
    <div className="namespace-usage-detail" id={`namespace-detail-${id}`}>
      <h3>项目占用</h3>
      {!projects.length ? (
        <p className="muted">此空间暂无项目。</p>
      ) : (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>项目</th>
                <th>状态</th>
                <th>保留版本</th>
                <th>保留文件</th>
                <th>待清理</th>
                <th>合计</th>
              </tr>
            </thead>
            <tbody>
              {projects.map((p) => (
                <tr key={p.id}>
                  <td>
                    {p.deleted ? (
                      p.name || '根站点'
                    ) : (
                      <a href={`#/n/${namespace.name}/sites/${p.id}`}>{p.name || '根站点'}</a>
                    )}
                  </td>
                  <td>
                    <Badge tone={p.deleted ? 'warning' : 'neutral'}>
                      {p.deleted ? '已删除，待清理' : p.disabled ? '已停用' : '有效项目'}
                    </Badge>
                  </td>
                  <td>{p.retained_versions}</td>
                  <td>{formatBytes(p.retained_bytes)}</td>
                  <td>{formatBytes(p.retired_bytes)}</td>
                  <td>{formatBytes(p.used_bytes)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <h3>空间成员</h3>
      {!members.length ? (
        <p className="muted">暂无成员。</p>
      ) : (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>用户</th>
                <th>GitLab ID</th>
                <th>空间角色</th>
                <th>平台授权</th>
              </tr>
            </thead>
            <tbody>
              {members.map((m) => (
                <tr key={m.user_id}>
                  <td>{m.username || '尚未登录'}</td>
                  <td>{m.gitlab_id}</td>
                  <td>{m.role === 'admin' ? '管理员' : '成员'}</td>
                  <td>{statuses[m.status]}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

export function AdminNamespaces() {
  const [query, setQuery] = useState('');
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [refresh, setRefresh] = useState(0);
  const res = useResource(() => api<NamespaceUsage[]>('admin/namespaces'), [refresh]);
  const items = res.data ?? [];
  const shown = items.filter((n) => n.name.includes(query.trim().toLowerCase()));
  return (
    <>
      <PageHeader
        title="空间用量"
        description="查看平台全部 namespace、项目占用与成员。"
        actions={
          <Button icon={<RefreshCw size={16} />} onClick={() => setRefresh((n) => n + 1)} loading={res.loading}>
            刷新用量
          </Button>
        }
      />
      <p className="muted small">
        按已记录的部署文件字节数统计，包含历史与待清理版本；不含上传临时文件、数据库或文件系统块开销。
      </p>
      {res.error && <ErrorState message={res.error} onRetry={res.reload} />}
      {!res.data && res.loading && <Spinner />}
      {res.data && (
        <>
          <div className="stats">
            <div className="stat">
              <span className="stat-label">全部空间</span>
              <span className="stat-value">{items.length}</span>
            </div>
            <div className="stat">
              <span className="stat-label">部署文件占用</span>
              <span className="stat-value">{formatBytes(items.reduce((sum, n) => sum + n.used_bytes, 0))}</span>
            </div>
            <div className="stat">
              <span className="stat-label">待清理</span>
              <span className="stat-value">{formatBytes(items.reduce((sum, n) => sum + n.retired_bytes, 0))}</span>
            </div>
          </div>
          <div className="toolbar">
            <label className="search">
              <Search size={14} aria-hidden />
              <input
                aria-label="搜索 namespace"
                placeholder="搜索 namespace"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </label>
          </div>
          {!shown.length && <EmptyState icon={<Globe size={24} />} title="没有匹配的空间" />}
          <div className="stack">
            {shown.map((n) => (
              <Card key={n.id} flush>
                <button
                  type="button"
                  className="namespace-usage-toggle"
                  aria-expanded={expanded.has(n.id)}
                  aria-controls={`namespace-detail-${n.id}`}
                  onClick={() =>
                    setExpanded((current) => {
                      const next = new Set(current);
                      if (next.has(n.id)) next.delete(n.id);
                      else next.add(n.id);
                      return next;
                    })
                  }
                >
                  {expanded.has(n.id) ? <ChevronDown size={18} aria-hidden /> : <ChevronRight size={18} aria-hidden />}
                  <strong>~{n.name}</strong>
                  <span>
                    {n.project_count} 个项目 · {n.member_count} 位成员
                  </span>
                  <span className="namespace-usage-total">
                    {formatBytes(n.used_bytes)}{' '}
                    <small className="muted">（待清理 {formatBytes(n.retired_bytes)}）</small>
                  </span>
                </button>
                {expanded.has(n.id) && <NamespaceDetails key={`${n.id}:${refresh}`} id={n.id} />}
              </Card>
            ))}
          </div>
        </>
      )}
    </>
  );
}
