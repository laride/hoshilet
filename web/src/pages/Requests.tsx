import { useState } from 'react';
import type { FormEvent } from 'react';
import { FileClock, Send } from 'lucide-react';
import { api, errorMessage } from '../api';
import { useResource, validSlug } from '../lib';
import { useSession } from '../session';
import type { NamespaceRequest } from '../types';
import {
  Badge,
  Button,
  Callout,
  Card,
  EmptyState,
  ErrorState,
  Field,
  Input,
  PageHeader,
  Spinner,
  Time,
  useToast,
} from '../ui';
import type { Tone } from '../ui';

export const requestStatus: Record<NamespaceRequest['status'], [string, Tone]> = {
  pending: ['审核中', 'warning'],
  approved: ['已批准', 'success'],
  rejected: ['已拒绝', 'danger'],
};

export function Requests() {
  const toast = useToast();
  const session = useSession();
  const res = useResource(async () => {
    const list = await api<NamespaceRequest[]>('requests');
    if (list.some((r) => r.status === 'approved' && !session.namespaces.some((n) => n.name === r.name)))
      await session.refreshNamespaces();
    return list;
  }, []);
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const invalid = !!name && !validSlug(name);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await api('requests', 'POST', { name });
      setName('');
      toast.success('申请已提交，等待平台管理员审核');
      await res.reload();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <PageHeader
        title="空间申请"
        description="除个人空间外，可以为团队或项目申请额外的空间。批准后你将成为该空间的管理员。"
      />
      <div className="stack">
        <Card title="申请新空间">
          <form className="inline-form" onSubmit={submit} noValidate>
            <Field
              label="空间名称"
              error={invalid ? '只能包含小写字母、数字、点、下划线、连字符，并以字母或数字开头' : ''}
              hint={`站点将发布在 /~${name || 'name'}/ 下，名称批准后不可修改。`}
            >
              <Input
                value={name}
                onChange={(e) => setName(e.target.value.trim())}
                prefix="/~"
                placeholder="team"
                aria-label="空间名称"
                maxLength={63}
              />
            </Field>
            <Button
              type="submit"
              variant="primary"
              icon={<Send size={16} />}
              loading={busy}
              disabled={!name || invalid}
            >
              提交申请
            </Button>
          </form>
          {error && <Callout tone="danger">{error}</Callout>}
        </Card>
        <Card title="我的申请" flush>
          {res.error && <ErrorState message={res.error} onRetry={res.reload} />}
          {!res.data && res.loading && <Spinner />}
          {res.data && res.data.length === 0 && <EmptyState icon={<FileClock size={24} />} title="暂无申请记录" />}
          {res.data && res.data.length > 0 && (
            <ul className="list">
              {res.data.map((r) => (
                <li key={r.id} className="list-item">
                  <div>
                    <strong className="mono">~{r.name}</strong>
                    <div className="muted small">
                      提交于 <Time value={r.created_at} />
                      {r.reason && <> · {r.reason}</>}
                    </div>
                  </div>
                  <Badge tone={requestStatus[r.status][1]} dot>
                    {requestStatus[r.status][0]}
                  </Badge>
                </li>
              ))}
            </ul>
          )}
        </Card>
      </div>
    </>
  );
}
