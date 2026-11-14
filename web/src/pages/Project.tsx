import { useRef, useState } from 'react';
import type { DragEvent } from 'react';
import {
  ArrowUpRight,
  CircleDot,
  FileArchive,
  GitBranch,
  History,
  Plus,
  RotateCcw,
  Rocket,
  ShieldCheck,
  Trash2,
  UploadCloud,
} from 'lucide-react';
import { ApiError, api, errorMessage, uploadDeployment } from '../api';
import { cx, formatBytes, navigate, parseBranches, shortId, sitePath, siteUrl, useResource } from '../lib';
import { canManage, useSession } from '../session';
import type { Activation, Deployment, Issuer, Namespace, Project, Trust } from '../types';
import {
  Badge,
  Button,
  Callout,
  Card,
  CopyButton,
  EmptyState,
  ErrorState,
  Field,
  FormDialog,
  IdChip,
  Input,
  PageHeader,
  Select,
  Spinner,
  Switch,
  Tabs,
  Time,
  useAction,
  useConfirm,
  useToast,
} from '../ui';
import { siteStatus } from './Sites';

interface ProjectData {
  project: Project;
  deployments: Deployment[];
  activations: Activation[];
}

export function ProjectPage({ ns, projectId, tab }: { ns: Namespace; projectId: string; tab: string }) {
  const session = useSession();
  const manage = canManage(session, ns);
  const res = useResource<ProjectData>(async () => {
    const [project, deployments, activations] = await Promise.all([
      api<Project>(`projects/${projectId}`),
      api<Deployment[]>(`projects/${projectId}/deployments`),
      api<Activation[]>(`projects/${projectId}/activations`),
    ]);
    return { project, deployments, activations };
  }, [projectId]);
  const base = `/n/${ns.name}/sites/${projectId}`;
  const sites = <a href={`#/n/${ns.name}/sites`}>站点</a>;

  if (res.error && !res.data) {
    return (
      <>
        <PageHeader crumbs={sites} title="站点" />
        <ErrorState message={res.error} onRetry={res.reload} />
      </>
    );
  }
  if (!res.data) return <Spinner />;
  const { project } = res.data;
  const url = siteUrl(session.me.content_url, ns.name, project.name);
  const tabs = [
    { id: 'deployments', label: '部署', href: base },
    ...(manage ? [{ id: 'ci', label: 'CI 发布', href: `${base}/ci` }] : []),
    { id: 'settings', label: '设置', href: `${base}/settings` },
  ];

  return (
    <>
      <PageHeader
        crumbs={
          <>
            {sites}
            <span aria-hidden>/</span>
            <span>{project.name || '根站点'}</span>
          </>
        }
        title={
          <span className="title-with-badge">
            {project.name || '根站点'}
            {siteStatus(project)}
          </span>
        }
        description={
          <span className="url-line">
            <a href={url} target="_blank" rel="noreferrer" className="mono">
              {url}
            </a>
            <CopyButton value={url} label="复制链接" compact />
          </span>
        }
        actions={
          <a
            className={cx('btn btn-secondary', (!project.current_id || !!project.disabled) && 'btn-disabled')}
            href={url}
            target="_blank"
            rel="noreferrer"
            aria-disabled={!project.current_id || !!project.disabled}
          >
            访问站点 <ArrowUpRight size={16} aria-hidden />
          </a>
        }
      />
      <Tabs items={tabs} current={tab === 'ci' && manage ? 'ci' : tab === 'settings' ? 'settings' : 'deployments'} />
      {tab === 'ci' && manage ? (
        <CiTab project={project} />
      ) : tab === 'settings' ? (
        <SettingsTab ns={ns} project={project} manage={manage} reload={res.reload} />
      ) : (
        <DeploymentsTab ns={ns} data={res.data} manage={manage} reload={res.reload} />
      )}
    </>
  );
}

/* ---------- Deployments ---------- */

function UploadZone({ ns, project, onDone }: { ns: Namespace; project: Project; onDone: () => Promise<void> }) {
  const toast = useToast();
  const input = useRef<HTMLInputElement>(null);
  const [activate, setActivate] = useState(true);
  const [progress, setProgress] = useState<number | null>(null);
  const [fileName, setFileName] = useState('');
  const [dragging, setDragging] = useState(false);
  const [error, setError] = useState('');
  const disabled = progress !== null || !!project.disabled;

  async function start(file: File) {
    if (!/\.zip$/i.test(file.name) && file.type !== 'application/zip') {
      setError('请选择 .zip 文件');
      return;
    }
    setError('');
    setFileName(file.name);
    setProgress(0);
    try {
      await uploadDeployment({
        projectId: project.id,
        revision: project.revision,
        activate,
        file,
        onProgress: setProgress,
      });
      toast.success(activate ? '新版本已上线' : '版本已保存，尚未上线');
    } catch (e) {
      setError(errorMessage(e));
      if (e instanceof ApiError && e.code === 'revision_conflict') toast.info('已刷新站点状态，请重新上传');
    } finally {
      setProgress(null);
      await onDone();
    }
  }

  function onDrop(e: DragEvent) {
    e.preventDefault();
    setDragging(false);
    const f = e.dataTransfer.files[0];
    if (f && !disabled) void start(f);
  }

  return (
    <Card
      title="上传新版本"
      description={
        <>
          ZIP 包的顶层即网站根目录。构建时请把资源 base path 设为 <code>{sitePath(ns.name, project.name)}</code>。
        </>
      }
    >
      <div
        className={cx('dropzone', dragging && 'dragging', disabled && 'disabled')}
        onDragOver={(e) => {
          e.preventDefault();
          if (!disabled) setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
        onClick={() => !disabled && input.current?.click()}
      >
        <input
          ref={input}
          type="file"
          accept=".zip,application/zip"
          aria-label="选择 ZIP 部署文件"
          className="visually-hidden"
          disabled={disabled}
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) void start(f);
            e.target.value = '';
          }}
        />
        {progress !== null ? (
          <div className="upload-progress" role="status">
            <FileArchive size={28} aria-hidden />
            <strong>{fileName}</strong>
            <progress max={100} value={progress < 100 ? progress : undefined} aria-label="上传进度" />
            <span className="muted">
              {progress < 100 ? `上传中 ${progress}%` : activate ? '正在校验并发布…' : '正在校验…'}
            </span>
          </div>
        ) : (
          <>
            <UploadCloud size={32} aria-hidden className="dropzone-icon" />
            <strong>{project.disabled ? '站点已停用，无法上传' : '拖拽 ZIP 文件到这里'}</strong>
            {!project.disabled && (
              <span className="muted">
                或 <span className="link">点击选择文件</span>
              </span>
            )}
          </>
        )}
      </div>
      <div className="upload-options">
        <Switch
          checked={activate}
          onChange={setActivate}
          disabled={progress !== null}
          label="上传后立即上线"
          description={activate ? '校验通过后立即替换当前版本。' : '仅保存为历史版本，之后可在下方手动上线。'}
        />
      </div>
      {error && (
        <Callout tone="danger" title="上传失败">
          {error}
        </Callout>
      )}
    </Card>
  );
}

function DeploymentsTab({
  ns,
  data,
  manage,
  reload,
}: {
  ns: Namespace;
  data: ProjectData;
  manage: boolean;
  reload: () => Promise<void>;
}) {
  const { project, deployments, activations } = data;
  const confirm = useConfirm();
  const toast = useToast();
  const current = deployments.find((d) => d.id === project.current_id);
  const totalSize = deployments.reduce((s, d) => s + d.size, 0);
  const isRollback = (d: Deployment) => !!d.last_activated_at;

  function activate(d: Deployment) {
    const rollback = isRollback(d);
    confirm({
      title: rollback ? '回滚到此版本？' : '上线此版本？',
      description: (
        <>
          站点 <code>{sitePath(ns.name, project.name)}</code> 将立即切换到版本 <code>{shortId(d.id)}</code>
          {current && (
            <>
              （当前为 <code>{shortId(current.id)}</code>）
            </>
          )}
          。切换是原子的，之后可以随时再切回。
        </>
      ),
      confirmLabel: rollback ? '确认回滚' : '确认上线',
      action: async () => {
        try {
          await api(`projects/${project.id}/activate`, 'POST', { deployment_id: d.id, revision: project.revision });
        } catch (e) {
          if (e instanceof ApiError && e.code === 'revision_conflict') await reload();
          throw e;
        }
        await reload();
        toast.success(`版本 ${shortId(d.id)} 已上线`);
      },
    });
  }

  function remove(d: Deployment) {
    confirm({
      title: '删除此版本？',
      description: (
        <>
          版本 <code>{shortId(d.id)}</code> 将被标记删除，文件会在稍后清理，删除后无法再回滚到该版本。
        </>
      ),
      confirmLabel: '删除版本',
      danger: true,
      action: async () => {
        await api(`projects/${project.id}/deployments/${d.id}`, 'DELETE');
        await reload();
        toast.success('版本已删除');
      },
    });
  }

  return (
    <div className="stack">
      {!!project.disabled && (
        <Callout tone="warning" title="站点已被平台管理员停用">
          公开访问已下线，期间无法上传新版本。
        </Callout>
      )}
      <div className="stats">
        <div className="stat">
          <span className="stat-label">当前版本</span>
          <span className="stat-value mono">{current ? shortId(current.id) : '—'}</span>
          <span className="stat-sub">
            {current ? (
              <>
                上传于 <Time value={current.uploaded_at} />
              </>
            ) : (
              '尚未上线任何版本'
            )}
          </span>
        </div>
        <div className="stat">
          <span className="stat-label">当前大小</span>
          <span className="stat-value">{current ? formatBytes(current.size) : '—'}</span>
          <span className="stat-sub">{current ? `${current.files} 个文件` : '\u00a0'}</span>
        </div>
        <div className="stat">
          <span className="stat-label">历史版本</span>
          <span className="stat-value">{deployments.length}</span>
          <span className="stat-sub">共占用 {formatBytes(totalSize)}</span>
        </div>
        <div className="stat">
          <span className="stat-label">最近上线</span>
          <span className="stat-value stat-value-sm">
            {activations[0] ? <Time value={activations[0].activated_at} /> : '—'}
          </span>
          <span className="stat-sub">共 {activations.length} 次上线</span>
        </div>
      </div>

      <UploadZone ns={ns} project={project} onDone={reload} />

      <Card title="版本历史" description="已保存的部署版本，超出平台保留数量的旧版本会被自动清理。" flush>
        {deployments.length === 0 ? (
          <EmptyState icon={<Rocket size={24} />} title="还没有部署">
            上传第一个 ZIP 包后，版本会出现在这里。
          </EmptyState>
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>版本</th>
                  <th>上传时间</th>
                  <th>最后上线</th>
                  <th className="num">文件</th>
                  <th className="num">大小</th>
                  <th className="actions-col">
                    <span className="visually-hidden">操作</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {deployments.map((d) => {
                  const isCurrent = d.id === project.current_id;
                  const rollback = isRollback(d);
                  return (
                    <tr key={d.id} className={cx(isCurrent && 'row-current')} data-deployment={d.id}>
                      <td>
                        <span className="cell-inline">
                          <IdChip value={d.id} />
                          {isCurrent && (
                            <Badge tone="success" dot>
                              当前
                            </Badge>
                          )}
                        </span>
                      </td>
                      <td>
                        <Time value={d.uploaded_at} />
                      </td>
                      <td>
                        <Time value={d.last_activated_at} />
                      </td>
                      <td className="num">{d.files}</td>
                      <td className="num">{formatBytes(d.size)}</td>
                      <td className="actions-col">
                        {!isCurrent && (
                          <span className="row-actions">
                            <Button
                              size="sm"
                              icon={rollback ? <RotateCcw size={14} /> : <Rocket size={14} />}
                              onClick={() => activate(d)}
                              disabled={!!project.disabled}
                            >
                              {rollback ? '回滚' : '上线'}
                            </Button>
                            {manage && (
                              <Button
                                size="sm"
                                variant="danger-ghost"
                                aria-label={`删除版本 ${shortId(d.id)}`}
                                title="删除版本"
                                icon={<Trash2 size={14} />}
                                onClick={() => remove(d)}
                              />
                            )}
                          </span>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <Card
        title={
          <span className="title-with-badge">
            <History size={18} aria-hidden />
            上线记录<Badge>{activations.length}</Badge>
          </span>
        }
      >
        {activations.length === 0 ? (
          <p className="muted">暂无上线记录。</p>
        ) : (
          <ol className="timeline" aria-label="上线记录">
            {activations.slice(0, 50).map((a, i) => (
              <li key={a.id}>
                <span className={cx('timeline-dot', i === 0 && 'active')} aria-hidden>
                  <CircleDot size={12} />
                </span>
                <div>
                  <div>
                    上线版本 <code>{shortId(a.deployment_id)}</code>
                    {a.previous_id && (
                      <span className="muted">
                        （替换 <code>{shortId(a.previous_id)}</code>）
                      </span>
                    )}
                  </div>
                  <small className="muted">
                    <Time value={a.activated_at} relative={false} /> · 凭据{' '}
                    <code title={a.actor}>{shortId(a.actor)}</code>
                  </small>
                </div>
              </li>
            ))}
          </ol>
        )}
      </Card>
    </div>
  );
}

/* ---------- CI / OIDC ---------- */

function AddTrustDialog({
  project,
  issuers,
  open,
  onClose,
  onDone,
}: {
  project: Project;
  issuers: Issuer[];
  open: boolean;
  onClose: () => void;
  onDone: () => Promise<void>;
}) {
  const enabled = issuers.filter((i) => i.enabled);
  const [issuer, setIssuer] = useState('');
  const [repo, setRepo] = useState('');
  const [branches, setBranches] = useState('main');
  const [protectedRef, setProtectedRef] = useState(false);
  const [allowMR, setAllowMR] = useState(false);
  const list = branches
    .split(/[,\s]+/)
    .map((s) => s.trim())
    .filter(Boolean);
  const branchError = list.some((b) => b.includes('*') && b !== '*')
    ? '不支持通配符模式，只能填写精确分支名或单独的 *'
    : '';
  const repoError = repo && !/^\d+$/.test(repo) ? '请填写 GitLab 项目的数字 ID' : '';
  return (
    <FormDialog
      open={open}
      onClose={onClose}
      title="添加发布信任"
      description="允许指定 GitLab 仓库的 CI 作业通过 OIDC 发布此站点。"
      submitLabel="添加信任"
      canSubmit={!!repo && !repoError && list.length > 0 && !branchError}
      onSubmit={async () => {
        await api(`projects/${project.id}/trusts`, 'POST', {
          issuer_id: issuer || enabled[0]?.id,
          repository_id: repo,
          branches: list,
          require_ref_protected: protectedRef,
          allow_mr_pipelines: allowMR,
        });
        setRepo('');
        setAllowMR(false);
        await onDone();
        onClose();
      }}
    >
      <Field label="GitLab 实例（Issuer）">
        <Select value={issuer || enabled[0]?.id} onChange={(e) => setIssuer(e.target.value)}>
          {enabled.map((i) => (
            <option key={i.id} value={i.id}>
              {i.url}
            </option>
          ))}
        </Select>
      </Field>
      <Field label="GitLab 项目 ID" error={repoError} hint="在 GitLab 项目主页标题下方可以找到数字 ID。">
        <Input value={repo} onChange={(e) => setRepo(e.target.value.trim())} inputMode="numeric" placeholder="12345" />
      </Field>
      <Field
        label="允许的分支"
        error={branchError}
        hint={
          <>
            多个分支用逗号分隔；<code>*</code> 匹配所有分支（不含 tag）。
          </>
        }
      >
        <Input value={branches} onChange={(e) => setBranches(e.target.value)} placeholder="main, release" />
      </Field>
      <Switch
        checked={allowMR}
        onChange={setAllowMR}
        label="允许同仓库 MR 流水线发布"
        description="仅对此条信任生效，默认关闭；fork MR 始终拒绝，仍需匹配分支和保护要求。"
      />
      <Switch
        checked={protectedRef}
        onChange={setProtectedRef}
        label="仅限受保护分支"
        description="开启后，要求 CI token 中 ref_protected 为 true。"
      />
    </FormDialog>
  );
}

function CiTab({ project }: { project: Project }) {
  const session = useSession();
  const confirm = useConfirm();
  const toast = useToast();
  const [adding, setAdding] = useState(false);
  const res = useResource(async () => {
    const [trusts, issuers] = await Promise.all([
      api<Trust[]>(`projects/${project.id}/trusts`),
      api<Issuer[]>('issuers'),
    ]);
    return { trusts, issuers };
  }, [project.id]);
  const origin = location.origin;
  const snippet = `publish_hoshilet:
  id_tokens:
    HOSHILET_ID_TOKEN:
      aud: ${origin}
  variables:
    HOSHILET_SERVER: ${origin}
  script:
    - hoshilet --json deploy dist --oidc --project-id ${project.id}`;

  if (res.error) return <ErrorState message={res.error} onRetry={res.reload} />;
  if (!res.data) return <Spinner />;
  const { trusts, issuers } = res.data;
  const hasIssuer = issuers.some((i) => i.enabled);

  return (
    <div className="stack">
      <Card
        title="发布信任"
        description="匹配下列规则的 GitLab CI 作业可以使用 OIDC 身份令牌发布此站点，无需保存任何密钥。"
        actions={
          hasIssuer && (
            <Button variant="primary" size="sm" icon={<Plus size={14} />} onClick={() => setAdding(true)}>
              添加信任
            </Button>
          )
        }
        flush
      >
        {!hasIssuer ? (
          <EmptyState
            icon={<ShieldCheck size={24} />}
            title="尚未配置 CI Issuer"
            action={session.adminMode && <Button onClick={() => navigate('/admin/issuers')}>前往配置</Button>}
          >
            需要平台管理员先添加允许签发 OIDC 令牌的 GitLab 实例。
          </EmptyState>
        ) : trusts.length === 0 ? (
          <EmptyState icon={<GitBranch size={24} />} title="还没有发布信任">
            添加一条信任规则，让指定仓库的 CI 自动发布此站点。
          </EmptyState>
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>GitLab 实例</th>
                  <th>项目 ID</th>
                  <th>分支</th>
                  <th>受保护分支</th>
                  <th>MR 流水线</th>
                  <th className="actions-col">
                    <span className="visually-hidden">操作</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {trusts.map((t) => (
                  <tr key={t.id}>
                    <td className="mono">{t.issuer_url}</td>
                    <td className="mono">{t.repository_id}</td>
                    <td>
                      <span className="chips">
                        {parseBranches(t.branches).map((b) => (
                          <Badge key={b} tone={b === '*' ? 'warning' : 'neutral'}>
                            {b === '*' ? '所有分支' : b}
                          </Badge>
                        ))}
                      </span>
                    </td>
                    <td>
                      {t.require_ref_protected ? (
                        <Badge tone="success">必须</Badge>
                      ) : (
                        <span className="muted">不要求</span>
                      )}
                    </td>
                    <td>
                      {t.allow_mr_pipelines ? (
                        <Badge tone="success">允许同仓库 MR</Badge>
                      ) : (
                        <span className="muted">不允许</span>
                      )}
                    </td>
                    <td className="actions-col">
                      <Button
                        size="sm"
                        variant="danger-ghost"
                        onClick={() =>
                          confirm({
                            title: '移除此信任？',
                            description: (
                              <>
                                仓库 <code>{t.repository_id}</code> 的 CI 将无法再发布此站点。
                              </>
                            ),
                            confirmLabel: '移除',
                            danger: true,
                            action: async () => {
                              await api(`projects/${project.id}/trusts/${t.id}`, 'DELETE');
                              await res.reload();
                              toast.success('信任已移除');
                            },
                          })
                        }
                      >
                        移除
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
      <Card
        title="GitLab CI 配置示例"
        description="在 .gitlab-ci.yml 中添加发布作业，CLI 会用 OIDC 令牌换取一次性发布凭据。"
        actions={<CopyButton value={snippet} />}
      >
        <pre className="code-block">{snippet}</pre>
        <p className="muted small">合并请求（MR）流水线是否可以发布，由匹配的发布信任规则决定，默认不允许。</p>
      </Card>
      <AddTrustDialog
        project={project}
        issuers={issuers}
        open={adding}
        onClose={() => setAdding(false)}
        onDone={res.reload}
      />
    </div>
  );
}

/* ---------- Settings ---------- */

function SettingsTab({
  ns,
  project,
  manage,
  reload,
}: {
  ns: Namespace;
  project: Project;
  manage: boolean;
  reload: () => Promise<void>;
}) {
  const session = useSession();
  const confirm = useConfirm();
  const toast = useToast();
  const { busy, run } = useAction();
  const label = project.name || '根站点';
  return (
    <div className="stack">
      <Card title="站点信息">
        <dl className="details">
          <div>
            <dt>Project ID</dt>
            <dd>
              <IdChip value={project.id} full />
            </dd>
          </div>
          <div>
            <dt>访问路径</dt>
            <dd className="mono">{sitePath(ns.name, project.name)}</dd>
          </div>
          <div>
            <dt>修订号</dt>
            <dd>{project.revision}</dd>
          </div>
          <div>
            <dt>创建时间</dt>
            <dd>
              <Time value={project.created_at} relative={false} />
            </dd>
          </div>
        </dl>
        <p className="muted small">
          使用 CLI 发布时，通过 <code>--project-id</code> 指定上述 Project ID。
        </p>
      </Card>
      {session.adminMode && (
        <Card title="平台管控" description="停用后公开访问立即下线，且无法上传新版本；恢复后重新指向当前版本。">
          <div className="setting-row">
            <div>
              <strong>{project.disabled ? '站点当前已停用' : '站点当前正常提供服务'}</strong>
            </div>
            <Button
              variant={project.disabled ? 'primary' : 'secondary'}
              loading={busy === 'toggle'}
              onClick={() =>
                run(
                  'toggle',
                  async () => {
                    await api(`admin/projects/${project.id}`, 'PATCH', { disabled: !project.disabled });
                    await reload();
                  },
                  project.disabled ? '站点已恢复' : '站点已停用',
                )
              }
            >
              {project.disabled ? '恢复站点' : '停用站点'}
            </Button>
          </div>
        </Card>
      )}
      {manage && (
        <Card title="危险操作" className="card-danger">
          <div className="setting-row">
            <div>
              <strong>删除站点</strong>
              <p className="muted small">删除站点及其全部版本，公开链接立即失效，且无法恢复。</p>
            </div>
            <Button
              variant="danger"
              icon={<Trash2 size={16} />}
              onClick={() =>
                confirm({
                  title: `删除站点 ${label}？`,
                  description: (
                    <>
                      将删除 <code>{sitePath(ns.name, project.name)}</code> 及全部历史版本，此操作无法撤销。
                    </>
                  ),
                  confirmLabel: '永久删除',
                  danger: true,
                  typeToConfirm: project.name || ns.name,
                  action: async () => {
                    await api(`projects/${project.id}`, 'DELETE');
                    await session.refreshNamespaces();
                    toast.success('站点已删除');
                    navigate(`/n/${ns.name}/sites`);
                  },
                })
              }
            >
              删除站点
            </Button>
          </div>
        </Card>
      )}
    </div>
  );
}
