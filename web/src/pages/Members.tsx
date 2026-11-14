import { useState } from 'react';
import { Lock, UserPlus } from 'lucide-react';
import { api } from '../api';
import { useResource } from '../lib';
import { canManage, useSession } from '../session';
import type { Member, Namespace, Role } from '../types';
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
  Select,
  Spinner,
  useAction,
  useConfirm,
  useToast,
} from '../ui';

const roleText: Record<Role, string> = { admin: '管理员', member: '成员' };

function AddMemberDialog({
  ns,
  open,
  onClose,
  onDone,
}: {
  ns: Namespace;
  open: boolean;
  onClose: () => void;
  onDone: () => Promise<void>;
}) {
  const [gitlabId, setGitlabId] = useState('');
  const [role, setRole] = useState<Role>('member');
  const invalid = !!gitlabId && !/^[1-9]\d*$/.test(gitlabId);
  return (
    <FormDialog
      open={open}
      onClose={onClose}
      title="添加成员"
      description={`被添加的用户需要已登录过 hoshilet。`}
      submitLabel="添加成员"
      canSubmit={!!gitlabId && !invalid}
      onSubmit={async () => {
        await api(`namespaces/${ns.id}/members`, 'PUT', { gitlab_id: Number(gitlabId), role });
        setGitlabId('');
        setRole('member');
        await onDone();
        onClose();
      }}
    >
      <Field
        label="GitLab 用户 ID"
        error={invalid ? '请输入正整数' : ''}
        hint="在 GitLab 个人资料页可以找到数字用户 ID。"
      >
        <Input
          value={gitlabId}
          onChange={(e) => setGitlabId(e.target.value.trim())}
          inputMode="numeric"
          placeholder="1024"
          autoFocus
        />
      </Field>
      <Field label="角色">
        <Select value={role} onChange={(e) => setRole(e.target.value as Role)}>
          <option value="member">成员 — 发布和回滚已有站点</option>
          <option value="admin">管理员 — 另可管理站点、成员和 CI 信任</option>
        </Select>
      </Field>
    </FormDialog>
  );
}

export function Members({ ns }: { ns: Namespace }) {
  const session = useSession();
  const manage = canManage(session, ns);
  const confirm = useConfirm();
  const toast = useToast();
  const { busy, run } = useAction();
  const [adding, setAdding] = useState(false);
  const members = useResource(manage ? () => api<Member[]>(`namespaces/${ns.id}/members`) : null, [ns.id, manage]);

  if (!manage) {
    return (
      <>
        <PageHeader title="空间成员" />
        <EmptyState icon={<Lock size={24} />} title="需要空间管理员权限">
          你可以发布和回滚 ~{ns.name} 中的站点。成员管理仅对空间管理员开放。
        </EmptyState>
      </>
    );
  }
  const list = [...(members.data ?? [])].sort((a, b) =>
    a.role === b.role ? a.username.localeCompare(b.username) : a.role === 'admin' ? -1 : 1,
  );
  const adminCount = list.filter((m) => m.role === 'admin').length;

  return (
    <>
      <PageHeader
        title="空间成员"
        description={
          <>
            管理谁可以访问 <span className="mono">~{ns.name}</span>。空间至少保留一名管理员。
          </>
        }
        actions={
          <Button variant="primary" icon={<UserPlus size={16} />} onClick={() => setAdding(true)}>
            添加成员
          </Button>
        }
      />
      {members.error && <ErrorState message={members.error} onRetry={members.reload} />}
      {!members.data && members.loading && <Spinner />}
      {members.data && (
        <Card flush>
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>用户</th>
                  <th>GitLab ID</th>
                  <th>角色</th>
                  <th className="actions-col">
                    <span className="visually-hidden">操作</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {list.map((m) => {
                  const isLastAdmin = m.role === 'admin' && adminCount === 1;
                  return (
                    <tr key={m.user_id} data-member={m.username}>
                      <td>
                        <span className="cell-user">
                          <Avatar name={m.username || '?'} size={28} />
                          <strong>{m.username || '尚未登录'}</strong>
                          {m.user_id === session.me.user.id && <Badge>你</Badge>}
                        </span>
                      </td>
                      <td className="mono">{m.gitlab_id}</td>
                      <td>
                        <Select
                          aria-label={`${m.username} 的角色`}
                          value={m.role}
                          disabled={busy === m.user_id || isLastAdmin}
                          title={isLastAdmin ? '空间至少需要一名管理员' : undefined}
                          onChange={(e) => {
                            const role = e.target.value as Role;
                            void run(
                              m.user_id,
                              async () => {
                                await api(`namespaces/${ns.id}/members`, 'PUT', { gitlab_id: m.gitlab_id, role });
                                await members.reload();
                              },
                              `已将 ${m.username} 设为${roleText[role]}`,
                            );
                          }}
                        >
                          <option value="member">{roleText.member}</option>
                          <option value="admin">{roleText.admin}</option>
                        </Select>
                      </td>
                      <td className="actions-col">
                        <Button
                          size="sm"
                          variant="danger-ghost"
                          disabled={isLastAdmin}
                          onClick={() =>
                            confirm({
                              title: `移除 ${m.username}？`,
                              description: `该用户将无法再访问 ~${ns.name}。`,
                              confirmLabel: '移除成员',
                              danger: true,
                              action: async () => {
                                await api(`namespaces/${ns.id}/members/${m.user_id}`, 'DELETE');
                                await members.reload();
                                toast.success('成员已移除');
                              },
                            })
                          }
                        >
                          移除
                        </Button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </Card>
      )}
      <AddMemberDialog ns={ns} open={adding} onClose={() => setAdding(false)} onDone={members.reload} />
    </>
  );
}
