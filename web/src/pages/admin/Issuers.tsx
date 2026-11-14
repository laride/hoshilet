import { useState } from 'react';
import { Plus, ShieldCheck } from 'lucide-react';
import { api } from '../../api';
import { useResource } from '../../lib';
import type { Issuer } from '../../types';
import {
  Badge,
  Button,
  Card,
  EmptyState,
  ErrorState,
  Field,
  FormDialog,
  Input,
  PageHeader,
  Spinner,
  useAction,
} from '../../ui';

function isOrigin(s: string) {
  try {
    const u = new URL(s);
    return u.protocol === 'https:' && u.origin === s;
  } catch {
    return false;
  }
}

function AddIssuerDialog({
  open,
  onClose,
  onDone,
}: {
  open: boolean;
  onClose: () => void;
  onDone: () => Promise<void>;
}) {
  const [url, setUrl] = useState('');
  const [jwks, setJwks] = useState('');
  const [jwksTouched, setJwksTouched] = useState(false);
  const origin = url.replace(/\/+$/, '');
  const jwksValue = jwksTouched ? jwks : origin ? `${origin}/oauth/discovery/keys` : '';
  const urlError = url && !isOrigin(origin) ? '必须是 HTTPS origin，不能包含路径，例如 https://gitlab.example.com' : '';
  const jwksError = jwksValue && origin && !jwksValue.startsWith(origin + '/') ? 'JWKS URL 必须位于该 origin 之下' : '';
  return (
    <FormDialog
      open={open}
      onClose={onClose}
      title="添加 CI Issuer"
      description="允许该 GitLab 实例签发的 CI OIDC 令牌用于发布。它可以与网页登录使用的 GitLab 不同。"
      submitLabel="添加 Issuer"
      canSubmit={!!origin && !urlError && !!jwksValue && !jwksError}
      onSubmit={async () => {
        await api('admin/issuers', 'POST', { url: origin, jwks_url: jwksValue });
        setUrl('');
        setJwks('');
        setJwksTouched(false);
        await onDone();
        onClose();
      }}
    >
      <Field label="Issuer origin" error={urlError}>
        <Input
          value={url}
          onChange={(e) => setUrl(e.target.value.trim())}
          placeholder="https://gitlab.example.com"
          autoFocus
        />
      </Field>
      <Field label="JWKS URL" error={jwksError} hint="通常为 /oauth/discovery/keys，以实例实际配置为准。">
        <Input
          value={jwksValue}
          onChange={(e) => {
            setJwksTouched(true);
            setJwks(e.target.value.trim());
          }}
          placeholder="https://gitlab.example.com/oauth/discovery/keys"
        />
      </Field>
    </FormDialog>
  );
}

export function AdminIssuers() {
  const { busy, run } = useAction();
  const [adding, setAdding] = useState(false);
  const res = useResource(() => api<Issuer[]>('admin/issuers'), []);
  return (
    <>
      <PageHeader
        title="CI Issuers"
        description="受信任的 GitLab 实例。空间管理员可以基于这些实例为站点配置 OIDC 发布信任。"
        actions={
          <Button variant="primary" icon={<Plus size={16} />} onClick={() => setAdding(true)}>
            添加 Issuer
          </Button>
        }
      />
      {res.error && <ErrorState message={res.error} onRetry={res.reload} />}
      {!res.data && res.loading && <Spinner />}
      {res.data && (
        <Card flush>
          {res.data.length === 0 ? (
            <EmptyState
              icon={<ShieldCheck size={24} />}
              title="还没有 CI Issuer"
              action={
                <Button icon={<Plus size={16} />} onClick={() => setAdding(true)}>
                  添加 Issuer
                </Button>
              }
            >
              添加 GitLab 实例后，各站点即可配置免密钥的 CI 发布。
            </EmptyState>
          ) : (
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    <th>Issuer</th>
                    <th>JWKS URL</th>
                    <th>状态</th>
                    <th className="actions-col">
                      <span className="visually-hidden">操作</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {res.data.map((i) => (
                    <tr key={i.id} className={i.enabled ? undefined : 'row-muted'}>
                      <td className="mono">{i.url}</td>
                      <td className="mono small break">{i.jwks_url}</td>
                      <td>
                        {i.enabled ? (
                          <Badge tone="success" dot>
                            启用
                          </Badge>
                        ) : (
                          <Badge dot>已禁用</Badge>
                        )}
                      </td>
                      <td className="actions-col">
                        <Button
                          size="sm"
                          variant={i.enabled ? 'danger-ghost' : 'secondary'}
                          loading={busy === i.id}
                          onClick={() =>
                            run(
                              i.id,
                              async () => {
                                await api(`admin/issuers/${i.id}`, 'PATCH', { enabled: !i.enabled });
                                await res.reload();
                              },
                              i.enabled ? 'Issuer 已禁用，相关 CI 将无法发布' : 'Issuer 已启用',
                            )
                          }
                        >
                          {i.enabled ? '禁用' : '启用'}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      )}
      <AddIssuerDialog open={adding} onClose={() => setAdding(false)} onDone={res.reload} />
    </>
  );
}
