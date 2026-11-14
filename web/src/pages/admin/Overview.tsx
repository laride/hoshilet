import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { ArrowRight, Save } from 'lucide-react';
import { api, errorMessage } from '../../api';
import { formatBytes, useResource } from '../../lib';
import type { Metrics, Settings } from '../../types';
import { Button, Callout, Card, ErrorState, Field, Input, PageHeader, Spinner, useToast } from '../../ui';

const MiB = 1048576;

function SettingsForm({ settings, onSaved }: { settings: Settings; onSaved: () => Promise<void> }) {
  const toast = useToast();
  const [versions, setVersions] = useState(String(settings.max_versions));
  const [mib, setMib] = useState(String(+(settings.max_bytes / MiB).toFixed(2)));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    setVersions(String(settings.max_versions));
    setMib(String(+(settings.max_bytes / MiB).toFixed(2)));
  }, [settings]);

  const v = Number(versions);
  const bytes = Math.round(Number(mib) * MiB);
  const versionsError = !Number.isInteger(v) || v < 1 || v > 1000 ? '须为 1–1000 的整数' : '';
  const bytesError = !(bytes >= 1 && bytes <= 4 * 1024 * MiB) ? '须在 1 字节到 4096 MiB 之间' : '';
  const dirty = v !== settings.max_versions || bytes !== settings.max_bytes;

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await api('admin/settings', 'PUT', { max_versions: v, max_bytes: bytes });
      toast.success('平台策略已保存');
      await onSaved();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="form-stack" onSubmit={submit} noValidate>
      <div className="form-row">
        <Field
          label="每个站点保留版本数"
          error={versionsError}
          hint="包含当前版本和暂存版本。降低后会立即清理多余的旧版本。"
        >
          <Input type="number" min={1} max={1000} value={versions} onChange={(e) => setVersions(e.target.value)} />
        </Field>
        <Field
          label="单版本解压大小上限（MiB）"
          error={bytesError}
          hint={!bytesError ? `= ${bytes.toLocaleString()} 字节` : undefined}
        >
          <Input type="number" min={0} step="any" value={mib} onChange={(e) => setMib(e.target.value)} />
        </Field>
      </div>
      {error && <Callout tone="danger">{error}</Callout>}
      <div className="form-actions">
        <Button
          type="submit"
          variant="primary"
          icon={<Save size={16} />}
          loading={busy}
          disabled={!dirty || !!versionsError || !!bytesError}
        >
          保存策略
        </Button>
      </div>
    </form>
  );
}

export function AdminOverview({ counts }: { counts: { users: number; requests: number } }) {
  const res = useResource(async () => {
    const [settings, metrics] = await Promise.all([api<Settings>('admin/settings'), api<Metrics>('admin/metrics')]);
    return { settings, metrics };
  }, []);

  return (
    <>
      <PageHeader title="概览与策略" description="平台运行状态与全局发布策略。" />
      {res.error && <ErrorState message={res.error} onRetry={res.reload} />}
      {!res.data && res.loading && <Spinner />}
      {res.data &&
        (() => {
          const { settings, metrics } = res.data;
          const ready = metrics.deployments.find((d) => d.status === 'ready');
          const failed = metrics.uploads.find((u) => u.status === 'failed')?.count ?? 0;
          return (
            <div className="stack">
              {(counts.users > 0 || counts.requests > 0) && (
                <div className="todo-row">
                  {counts.users > 0 && (
                    <a className="todo" href="#/admin/users">
                      <span>
                        <strong>{counts.users}</strong> 位用户等待授权
                      </span>
                      <ArrowRight size={16} aria-hidden />
                    </a>
                  )}
                  {counts.requests > 0 && (
                    <a className="todo" href="#/admin/requests">
                      <span>
                        <strong>{counts.requests}</strong> 个空间申请待审批
                      </span>
                      <ArrowRight size={16} aria-hidden />
                    </a>
                  )}
                </div>
              )}
              {metrics.pending_publications > 0 && (
                <Callout tone="warning" title={`${metrics.pending_publications} 个发布等待恢复`}>
                  服务重启后会自动恢复未完成的发布；如果持续存在，请查看服务端日志。
                </Callout>
              )}
              <div className="stats">
                <div className="stat">
                  <span className="stat-label">磁盘可用</span>
                  <span className="stat-value">{formatBytes(metrics.disk_free_bytes)}</span>
                  <span className="stat-sub">数据目录所在分区</span>
                </div>
                <div className="stat">
                  <span className="stat-label">已保存版本</span>
                  <span className="stat-value">{ready?.count ?? 0}</span>
                  <span className="stat-sub">占用 {formatBytes(ready?.bytes)}</span>
                </div>
                <div className="stat">
                  <span className="stat-label">待回收空间</span>
                  <span className="stat-value">{formatBytes(settings.retired_bytes)}</span>
                  <span className="stat-sub">已删除版本，延迟清理</span>
                </div>
                <div className="stat">
                  <span className="stat-label">进行中的上传</span>
                  <span className="stat-value">{metrics.upload_slots_in_use}</span>
                  <span className="stat-sub">历史失败上传 {failed} 次</span>
                </div>
              </div>
              <Card title="平台策略" description="对所有空间和站点生效。">
                <SettingsForm settings={settings} onSaved={res.reload} />
              </Card>
            </div>
          );
        })()}
    </>
  );
}
