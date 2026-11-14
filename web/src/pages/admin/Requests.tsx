import { useState } from 'react';
import { Check, FileClock, X } from 'lucide-react';
import { api } from '../../api';
import { useResource } from '../../lib';
import { useSession } from '../../session';
import type { NamespaceRequest } from '../../types';
import {
  Avatar,
  Badge,
  Button,
  Card,
  EmptyState,
  ErrorState,
  Field,
  FormDialog,
  Input,
  PageHeader,
  Segmented,
  Spinner,
  Time,
  useAction,
} from '../../ui';
import { requestStatus } from '../Requests';

export function AdminRequests() {
  const { refreshNamespaces } = useSession();
  const { busy, run } = useAction();
  const [view, setView] = useState<'pending' | 'done'>('pending');
  const [rejecting, setRejecting] = useState<NamespaceRequest | null>(null);
  const [reason, setReason] = useState('');
  const res = useResource(() => api<NamespaceRequest[]>('admin/requests'), []);
  const all = res.data ?? [];
  const pending = all.filter((r) => r.status === 'pending');
  const shown = view === 'pending' ? pending : all.filter((r) => r.status !== 'pending');

  async function decide(r: NamespaceRequest, approve: boolean, why = '') {
    await api(`admin/requests/${r.id}`, 'POST', { approve, reason: why });
    await res.reload();
    if (approve) await refreshNamespaces();
  }

  return (
    <>
      <PageHeader title="空间审批" description="批准后会创建空间，并把申请人设为该空间的管理员。" />
      {res.error && <ErrorState message={res.error} onRetry={res.reload} />}
      {!res.data && res.loading && <Spinner />}
      {res.data && (
        <Card flush>
          <div className="toolbar">
            <Segmented
              label="筛选申请"
              value={view}
              onChange={setView}
              options={[
                { value: 'pending', label: `待审批 ${pending.length}` },
                { value: 'done', label: '已处理' },
              ]}
            />
          </div>
          {shown.length === 0 ? (
            <EmptyState
              icon={<FileClock size={24} />}
              title={view === 'pending' ? '没有待审批的申请' : '暂无已处理的申请'}
            />
          ) : (
            <ul className="list">
              {shown.map((r) => (
                <li key={r.id} className="list-item" data-request={r.name}>
                  <div className="cell-user">
                    <Avatar name={r.username || '?'} size={32} />
                    <div>
                      <strong className="mono">~{r.name}</strong>
                      <div className="muted small">
                        {r.username} 提交于 <Time value={r.created_at} />
                        {r.reason && <> · {r.reason}</>}
                      </div>
                    </div>
                  </div>
                  {r.status === 'pending' ? (
                    <span className="row-actions">
                      <Button
                        size="sm"
                        variant="danger-ghost"
                        icon={<X size={14} />}
                        disabled={!!busy}
                        onClick={() => {
                          setReason('');
                          setRejecting(r);
                        }}
                      >
                        拒绝
                      </Button>
                      <Button
                        size="sm"
                        variant="primary"
                        icon={<Check size={14} />}
                        loading={busy === r.id}
                        disabled={!!busy}
                        onClick={() => run(r.id, () => decide(r, true), `空间 ~${r.name} 已创建`)}
                      >
                        批准
                      </Button>
                    </span>
                  ) : (
                    <Badge tone={requestStatus[r.status][1]} dot>
                      {requestStatus[r.status][0]}
                    </Badge>
                  )}
                </li>
              ))}
            </ul>
          )}
        </Card>
      )}
      <FormDialog
        open={!!rejecting}
        onClose={() => setRejecting(null)}
        title={`拒绝申请 ~${rejecting?.name ?? ''}`}
        submitLabel="拒绝申请"
        danger
        onSubmit={async () => {
          if (rejecting) await decide(rejecting, false, reason.trim() || '管理员拒绝');
          setRejecting(null);
        }}
      >
        <Field label="拒绝原因" optional hint="申请人可以在“空间申请”页面看到该原因。">
          <Input
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="例如：名称与现有团队冲突"
            autoFocus
            maxLength={200}
          />
        </Field>
      </FormDialog>
    </>
  );
}
