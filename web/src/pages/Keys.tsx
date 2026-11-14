import { useState } from 'react';
import { KeyRound, Plus } from 'lucide-react';
import { api } from '../api';
import { sitePath, useResource } from '../lib';
import { useSession } from '../session';
import type { ApiKey, Project } from '../types';
import {
  Badge,
  Button,
  Callout,
  Card,
  CopyButton,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  FormDialog,
  Input,
  PageHeader,
  RadioCards,
  Select,
  Spinner,
  Switch,
  Time,
  useConfirm,
  useToast,
} from '../ui';

type Scope = ApiKey['scope'];
const scopeText: Record<Scope, string> = { account: '账户', namespace: '空间', project: '站点' };

function keyState(k: ApiKey) {
  if (k.revoked) return <Badge tone="neutral">已撤销</Badge>;
  if (k.expires_at * 1000 < Date.now()) return <Badge tone="warning">已过期</Badge>;
  return (
    <Badge tone="success" dot>
      有效
    </Badge>
  );
}

function CreateKeyDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (token: string) => Promise<void>;
}) {
  const { namespaces, me } = useSession();
  const defaultNs = namespaces.find((n) => n.id === me.user.personal_ns)?.id ?? namespaces[0]?.id ?? '';
  const [name, setName] = useState('');
  const [scope, setScope] = useState<Scope>('project');
  const [nsId, setNsId] = useState(defaultNs);
  const [projectId, setProjectId] = useState('');
  const [days, setDays] = useState('90');
  const projects = useResource(
    open && scope === 'project' && nsId ? () => api<Project[]>(`namespaces/${nsId}/projects`) : null,
    [open, scope, nsId],
  );
  const ns = namespaces.find((n) => n.id === nsId);
  const pid = projectId || projects.data?.[0]?.id || '';
  const target = scope === 'account' ? '' : scope === 'namespace' ? nsId : pid;
  return (
    <FormDialog
      open={open}
      onClose={onClose}
      size="lg"
      title="创建 API Key"
      description="Key 的权限不会超过你在对应空间中的角色。"
      submitLabel="创建 Key"
      canSubmit={!!name.trim() && (scope === 'account' || !!target)}
      onSubmit={async () => {
        const k = await api<{ token: string }>('keys', 'POST', {
          Name: name.trim(),
          Scope: scope,
          Target: target,
          Days: Number(days),
        });
        setName('');
        await onCreated(k.token);
      }}
    >
      <Field label="名称" hint="用于识别用途，例如 “docs CI” 或 “本地发布”。">
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="docs CI" autoFocus maxLength={100} />
      </Field>
      <div className="field">
        <span className="field-label">权限范围</span>
        <RadioCards
          name="scope"
          value={scope}
          onChange={setScope}
          options={[
            { value: 'project', title: '单个站点', description: '只能发布指定站点，最安全，推荐用于 CI。' },
            {
              value: 'namespace',
              title: '整个空间',
              description: '可发布空间内全部站点；空间管理员还可自动创建子项目。',
            },
            { value: 'account', title: '账户', description: '拥有你在所有空间中的发布权限。' },
          ]}
        />
      </div>
      {scope !== 'account' && (
        <div className="form-row">
          <Field label="空间">
            <Select
              value={nsId}
              onChange={(e) => {
                setNsId(e.target.value);
                setProjectId('');
              }}
            >
              {namespaces.map((n) => (
                <option key={n.id} value={n.id}>
                  ~{n.name}
                </option>
              ))}
            </Select>
          </Field>
          {scope === 'project' && (
            <Field label="站点" error={projects.data && !projects.data.length ? '该空间还没有站点' : projects.error}>
              <Select value={pid} onChange={(e) => setProjectId(e.target.value)} disabled={!projects.data?.length}>
                {projects.loading && !projects.data && <option>加载中…</option>}
                {projects.data?.map((p) => (
                  <option key={p.id} value={p.id}>
                    {ns ? sitePath(ns.name, p.name) : p.name}
                  </option>
                ))}
              </Select>
            </Field>
          )}
        </div>
      )}
      <Field label="有效期">
        <Select value={days} onChange={(e) => setDays(e.target.value)}>
          <option value="7">7 天</option>
          <option value="30">30 天</option>
          <option value="90">90 天</option>
          <option value="180">180 天</option>
          <option value="365">365 天</option>
        </Select>
      </Field>
    </FormDialog>
  );
}

function SecretDialog({ token, onClose }: { token: string; onClose: () => void }) {
  const server = location.origin;
  const usage = `export HOSHILET_SERVER=${server}\nexport HOSHILET_API_KEY=${token}`;
  return (
    <Dialog
      open={!!token}
      onClose={onClose}
      title="API Key 已创建"
      size="lg"
      footer={
        <Button variant="primary" onClick={onClose}>
          我已保存
        </Button>
      }
    >
      <div className="form-stack">
        <Callout tone="warning" title="请立即复制保存">
          关闭此窗口后将无法再次查看该 Key。
        </Callout>
        <div className="secret">
          <code aria-label="API Key">{token}</code>
          <CopyButton value={token} />
        </div>
        <div>
          <div className="code-head">
            <span className="field-label">在 CLI 中使用</span>
            <CopyButton value={usage} />
          </div>
          <pre className="code-block">{usage}</pre>
        </div>
      </div>
    </Dialog>
  );
}

export function Keys() {
  const { namespaces } = useSession();
  const confirm = useConfirm();
  const toast = useToast();
  const [creating, setCreating] = useState(false);
  const [secret, setSecret] = useState('');
  const [showInactive, setShowInactive] = useState(false);
  const res = useResource(async () => {
    const keys = await api<ApiKey[]>('keys');
    const ids = [...new Set(keys.filter((k) => k.scope === 'project' && !k.revoked).map((k) => k.target))];
    const projects = await Promise.all(ids.map((id) => api<Project>(`projects/${id}`).catch(() => null)));
    return { keys, projects: Object.fromEntries(projects.filter((p): p is Project => !!p).map((p) => [p.id, p])) };
  }, []);

  function targetLabel(k: ApiKey) {
    if (k.scope === 'account') return <span className="muted">全部空间</span>;
    if (k.scope === 'namespace') {
      const n = namespaces.find((x) => x.id === k.target);
      return n ? <span className="mono">~{n.name}</span> : <code title={k.target}>{k.target.slice(0, 8)}</code>;
    }
    const p = res.data?.projects[k.target];
    return p ? (
      <span className="mono">{sitePath(p.namespace_name ?? '?', p.name)}</span>
    ) : (
      <code title={k.target}>{k.target.slice(0, 8)}</code>
    );
  }

  const keys = res.data?.keys ?? [];
  const active = keys.filter((k) => !k.revoked && k.expires_at * 1000 >= Date.now());
  const shown = showInactive ? keys : active;

  return (
    <>
      <PageHeader
        title="API Keys"
        description="供 CLI 和自动化脚本使用的个人发布凭据。在 GitLab CI 中推荐改用免密钥的 OIDC 发布。"
        actions={
          <Button variant="primary" icon={<Plus size={16} />} onClick={() => setCreating(true)}>
            创建 Key
          </Button>
        }
      />
      {res.error && <ErrorState message={res.error} onRetry={res.reload} />}
      {!res.data && res.loading && <Spinner />}
      {res.data && (
        <Card
          flush
          title={`有效 Key（${active.length}）`}
          actions={
            keys.length > active.length && (
              <Switch checked={showInactive} onChange={setShowInactive} label="显示已撤销 / 过期" />
            )
          }
        >
          {shown.length === 0 ? (
            <EmptyState
              icon={<KeyRound size={24} />}
              title="没有有效的 API Key"
              action={
                <Button icon={<Plus size={16} />} onClick={() => setCreating(true)}>
                  创建 Key
                </Button>
              }
            >
              创建 Key 后，即可用 <code>hoshilet deploy</code> 从本地或脚本发布站点。
            </EmptyState>
          ) : (
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    <th>名称</th>
                    <th>范围</th>
                    <th>状态</th>
                    <th>最近使用</th>
                    <th>到期</th>
                    <th className="actions-col">
                      <span className="visually-hidden">操作</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {shown.map((k) => (
                    <tr key={k.id} className={k.revoked ? 'row-muted' : undefined}>
                      <td>
                        <strong>{k.name || '未命名'}</strong>
                        <div className="muted small">
                          创建于 <Time value={k.created_at} />
                        </div>
                      </td>
                      <td>
                        <span className="cell-inline">
                          <Badge>{scopeText[k.scope]}</Badge>
                          {targetLabel(k)}
                        </span>
                      </td>
                      <td>{keyState(k)}</td>
                      <td>
                        <Time value={k.last_used_at} />
                      </td>
                      <td>
                        <Time value={k.expires_at} relative={false} />
                      </td>
                      <td className="actions-col">
                        {!k.revoked && (
                          <Button
                            size="sm"
                            variant="danger-ghost"
                            onClick={() =>
                              confirm({
                                title: `撤销 Key “${k.name || '未命名'}”？`,
                                description: '使用该 Key 的脚本和 CI 将立即无法发布，撤销后不可恢复。',
                                confirmLabel: '撤销 Key',
                                danger: true,
                                action: async () => {
                                  await api(`keys/${k.id}`, 'DELETE');
                                  await res.reload();
                                  toast.success('Key 已撤销');
                                },
                              })
                            }
                          >
                            撤销
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
      <CreateKeyDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreated={async (token) => {
          setCreating(false);
          setSecret(token);
          await res.reload();
        }}
      />
      <SecretDialog token={secret} onClose={() => setSecret('')} />
    </>
  );
}
