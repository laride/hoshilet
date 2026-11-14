import { useState } from 'react';
import { Search, UserPlus, Users } from 'lucide-react';
import { api } from '../../api';
import { useResource } from '../../lib';
import { useSession } from '../../session';
import type { User, UserStatus } from '../../types';
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
  useAction,
  useConfirm,
} from '../../ui';
import type { Tone } from '../../ui';

const statusText: Record<UserStatus, [string, Tone]> = {
  pending: ['待授权', 'warning'],
  approved: ['已授权', 'success'],
  disabled: ['已停用', 'danger'],
};

function PreapproveDialog({
  open,
  onClose,
  onDone,
}: {
  open: boolean;
  onClose: () => void;
  onDone: () => Promise<void>;
}) {
  const [gitlabId, setGitlabId] = useState('');
  const invalid = !!gitlabId && !/^[1-9]\d*$/.test(gitlabId);
  return (
    <FormDialog
      open={open}
      onClose={onClose}
      title="预先授权用户"
      description="用户首次登录时即可直接使用，并自动创建个人空间。"
      submitLabel="加入白名单"
      canSubmit={!!gitlabId && !invalid}
      onSubmit={async () => {
        await api('admin/users', 'POST', { gitlab_id: Number(gitlabId) });
        setGitlabId('');
        await onDone();
        onClose();
      }}
    >
      <Field
        label="GitLab 用户 ID"
        error={invalid ? '请输入正整数' : ''}
        hint="授权始终绑定数字 ID，用户名变更不影响授权。"
      >
        <Input
          value={gitlabId}
          onChange={(e) => setGitlabId(e.target.value.trim())}
          inputMode="numeric"
          placeholder="1024"
          autoFocus
        />
      </Field>
    </FormDialog>
  );
}

export function AdminUsers() {
  const { me, refreshNamespaces } = useSession();
  const confirm = useConfirm();
  const { busy, run } = useAction();
  const [filter, setFilter] = useState<'all' | UserStatus>('all');
  const [query, setQuery] = useState('');
  const [adding, setAdding] = useState(false);
  const res = useResource(() => api<User[]>('admin/users'), []);
  const users = res.data ?? [];
  const count = (s: UserStatus) => users.filter((u) => u.status === s).length;
  const q = query.trim().toLowerCase();
  const order: Record<UserStatus, number> = { pending: 0, approved: 1, disabled: 2 };
  const shown = users
    .filter(
      (u) =>
        (filter === 'all' || u.status === filter) &&
        (!q || u.username.toLowerCase().includes(q) || String(u.gitlab_id).includes(q)),
    )
    .sort((a, b) => order[a.status] - order[b.status]);

  async function setStatus(u: User, status: 'approved' | 'disabled') {
    await run(
      u.id,
      async () => {
        await api(`admin/users/${u.id}`, 'PATCH', { status });
        await res.reload();
        await refreshNamespaces();
      },
      status === 'approved'
        ? `已授权 ${u.username || '#' + u.gitlab_id}`
        : `已撤销 ${u.username || '#' + u.gitlab_id} 的授权`,
    );
  }

  return (
    <>
      <PageHeader
        title="用户授权"
        description="GitLab 只提供身份信息，只有获得授权的用户才能发布站点或申请空间。"
        actions={
          <Button variant="primary" icon={<UserPlus size={16} />} onClick={() => setAdding(true)}>
            预先授权
          </Button>
        }
      />
      {res.error && <ErrorState message={res.error} onRetry={res.reload} />}
      {!res.data && res.loading && <Spinner />}
      {res.data && (
        <Card flush>
          <div className="toolbar">
            <Segmented
              label="按状态筛选"
              value={filter}
              onChange={setFilter}
              options={[
                { value: 'all', label: `全部 ${users.length}` },
                { value: 'pending', label: `待授权 ${count('pending')}` },
                { value: 'approved', label: `已授权 ${count('approved')}` },
                { value: 'disabled', label: `已停用 ${count('disabled')}` },
              ]}
            />
            <label className="search">
              <Search size={14} aria-hidden />
              <input
                placeholder="搜索用户名或 ID"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                aria-label="搜索用户"
              />
            </label>
          </div>
          {shown.length === 0 ? (
            <EmptyState icon={<Users size={24} />} title="没有匹配的用户" />
          ) : (
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    <th>用户</th>
                    <th>GitLab ID</th>
                    <th>状态</th>
                    <th className="actions-col">
                      <span className="visually-hidden">操作</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {shown.map((u) => (
                    <tr key={u.id} data-user={u.username || u.gitlab_id}>
                      <td>
                        <span className="cell-user">
                          <Avatar name={u.username || '?'} size={28} />
                          {u.username ? <strong>{u.username}</strong> : <span className="muted">尚未登录</span>}
                          {u.id === me.user.id && <Badge>你</Badge>}
                        </span>
                      </td>
                      <td className="mono">{u.gitlab_id}</td>
                      <td>
                        <Badge tone={statusText[u.status][1]} dot>
                          {statusText[u.status][0]}
                        </Badge>
                      </td>
                      <td className="actions-col">
                        {u.status === 'approved' ? (
                          <Button
                            size="sm"
                            variant="danger-ghost"
                            disabled={u.id === me.user.id}
                            loading={busy === u.id}
                            onClick={() =>
                              confirm({
                                title: `撤销 ${u.username || '#' + u.gitlab_id} 的授权？`,
                                description:
                                  '该用户的会话将立即受限，已有 API Key 会被撤销且重新授权后也不会恢复。已上线的站点和 CI 信任不受影响。',
                                confirmLabel: '撤销授权',
                                danger: true,
                                action: () => setStatus(u, 'disabled'),
                              })
                            }
                          >
                            撤销授权
                          </Button>
                        ) : (
                          <Button
                            size="sm"
                            variant={u.status === 'pending' ? 'primary' : 'secondary'}
                            loading={busy === u.id}
                            onClick={() => setStatus(u, 'approved')}
                          >
                            {u.status === 'pending' ? '批准授权' : '重新授权'}
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      )}
      <PreapproveDialog open={adding} onClose={() => setAdding(false)} onDone={res.reload} />
    </>
  );
}
