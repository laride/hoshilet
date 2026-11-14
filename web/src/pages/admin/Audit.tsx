import { useState } from 'react';
import { RefreshCw, ScrollText, Search } from 'lucide-react';
import { api } from '../../api';
import { useResource } from '../../lib';
import type { AuditEvent } from '../../types';
import { Badge, Button, Card, EmptyState, ErrorState, PageHeader, Select, Spinner, Time } from '../../ui';
import type { Tone } from '../../ui';

const actionText: Record<string, string> = {
  'key.create': '创建 API Key',
  'key.revoke': '撤销 API Key',
  'member.set': '设置成员角色',
  'member.remove': '移除成员',
  'project.create': '创建站点',
  'project.delete': '删除站点',
  'project.disabled': '停用/恢复站点',
  'deployment.upload': '上传版本',
  'deployment.activate': '上线版本',
  'deployment.retire': '删除版本',
  'trust.create': '添加发布信任',
  'trust.delete': '移除发布信任',
  'oidc.exchange': 'CI OIDC 换取凭据',
  'settings.update': '更新平台策略',
  'namespace.request.approved': '批准空间申请',
  'namespace.request.rejected': '拒绝空间申请',
  'user.approve': '预先授权用户',
  'user.approved': '授权用户',
  'user.disabled': '撤销用户授权',
  'issuer.create': '添加 CI Issuer',
  'issuer.enabled': '启用/禁用 Issuer',
};

const groups: Record<string, [string, Tone]> = {
  key: ['凭据', 'info'],
  oidc: ['凭据', 'info'],
  member: ['成员', 'accent'],
  project: ['站点', 'success'],
  deployment: ['部署', 'success'],
  trust: ['CI', 'info'],
  settings: ['平台', 'warning'],
  namespace: ['空间', 'accent'],
  user: ['用户', 'warning'],
  issuer: ['CI', 'info'],
};

export function AdminAudit() {
  const [query, setQuery] = useState('');
  const [group, setGroup] = useState('');
  const res = useResource(() => api<AuditEvent[]>('admin/audit'), []);
  const q = query.trim().toLowerCase();
  const events = (res.data ?? []).filter(
    (e) =>
      (!group || e.action.split('.')[0] === group) &&
      (!q ||
        e.action.includes(q) ||
        e.target.toLowerCase().includes(q) ||
        e.actor.includes(q) ||
        (actionText[e.action] ?? '').includes(q)),
  );
  const groupKeys = [...new Set(Object.keys(groups))];

  return (
    <>
      <PageHeader
        title="审计日志"
        description="最近 500 条管理与发布操作。操作者显示为发起操作的凭据 ID。"
        actions={
          <Button icon={<RefreshCw size={16} />} loading={res.loading && !!res.data} onClick={res.reload}>
            刷新
          </Button>
        }
      />
      {res.error && <ErrorState message={res.error} onRetry={res.reload} />}
      {!res.data && res.loading && <Spinner />}
      {res.data && (
        <Card flush>
          <div className="toolbar">
            <Select
              value={group}
              onChange={(e) => setGroup(e.target.value)}
              aria-label="按类别筛选"
              className="input select toolbar-select"
            >
              <option value="">全部类别</option>
              {groupKeys.map((k) => (
                <option key={k} value={k}>
                  {groups[k][0]} · {k}
                </option>
              ))}
            </Select>
            <label className="search">
              <Search size={14} aria-hidden />
              <input
                placeholder="搜索操作、目标或凭据 ID"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                aria-label="搜索审计日志"
              />
            </label>
          </div>
          {events.length === 0 ? (
            <EmptyState icon={<ScrollText size={24} />} title="没有匹配的事件" />
          ) : (
            <div className="table-wrap">
              <table className="table table-compact">
                <thead>
                  <tr>
                    <th>时间</th>
                    <th>操作</th>
                    <th>目标</th>
                    <th>凭据</th>
                  </tr>
                </thead>
                <tbody>
                  {events.map((e) => {
                    const g = groups[e.action.split('.')[0]];
                    return (
                      <tr key={e.id}>
                        <td className="nowrap">
                          <Time value={e.created_at} relative={false} />
                        </td>
                        <td>
                          <span className="cell-inline">
                            {g && <Badge tone={g[1]}>{g[0]}</Badge>}
                            <span>{actionText[e.action] ?? e.action}</span>
                            <code className="muted small">{e.action}</code>
                          </span>
                        </td>
                        <td className="mono small break">{e.target}</td>
                        <td>
                          <code className="small" title={e.actor}>
                            {e.actor.slice(0, 8)}
                          </code>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      )}
    </>
  );
}
