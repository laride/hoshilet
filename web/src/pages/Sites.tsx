import { useState } from 'react';
import { ArrowUpRight, Globe, Plus } from 'lucide-react';
import { api } from '../api';
import { navigate, sitePath, siteUrl, useResource, validSlug } from '../lib';
import { canManage, useSession } from '../session';
import type { Namespace, Project } from '../types';
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  Field,
  FormDialog,
  Input,
  PageHeader,
  RadioCards,
  Spinner,
  Time,
} from '../ui';

export function siteStatus(p: Pick<Project, 'disabled' | 'current_id'>) {
  if (p.disabled)
    return (
      <Badge tone="danger" dot>
        已停用
      </Badge>
    );
  if (p.current_id)
    return (
      <Badge tone="success" dot>
        已上线
      </Badge>
    );
  return (
    <Badge tone="neutral" dot>
      未发布
    </Badge>
  );
}

function CreateSiteDialog({
  ns,
  open,
  onClose,
  hasProjects,
}: {
  ns: Namespace;
  open: boolean;
  onClose: () => void;
  hasProjects: boolean;
}) {
  const session = useSession();
  const lockedMode = hasProjects && ns.mode ? ns.mode : null;
  const [mode, setMode] = useState<'projects' | 'root'>('projects');
  const [name, setName] = useState('');
  const effectiveMode = lockedMode ?? mode;
  const nameError =
    effectiveMode === 'projects' && name && !validSlug(name)
      ? '只能包含小写字母、数字、点、下划线、连字符，并以字母或数字开头'
      : '';
  return (
    <FormDialog
      open={open}
      onClose={onClose}
      title="新建站点"
      description={`站点将发布在 ~${ns.name} 空间下。`}
      submitLabel="创建站点"
      canSubmit={effectiveMode === 'root' || (!!name && !nameError)}
      onSubmit={async () => {
        const p = await api<Project>(`namespaces/${ns.id}/projects`, 'POST', {
          mode: effectiveMode,
          name: effectiveMode === 'root' ? '' : name,
        });
        await session.refreshNamespaces();
        setName('');
        onClose();
        navigate(`/n/${ns.name}/sites/${p.id}`);
      }}
    >
      {lockedMode ? (
        <p className="muted small">
          该空间已使用{lockedMode === 'root' ? '根站点' : '子项目'}模式。删除全部站点后才能切换模式。
        </p>
      ) : (
        <RadioCards
          name="mode"
          value={mode}
          onChange={setMode}
          options={[
            { value: 'projects', title: '子项目模式', description: `可创建多个站点，如 /~${ns.name}/docs/` },
            { value: 'root', title: '根站点模式', description: `整个空间只有一个站点 /~${ns.name}/` },
          ]}
        />
      )}
      {effectiveMode === 'projects' && (
        <Field label="站点名称" error={nameError} hint={`访问路径：${sitePath(ns.name, name || 'name')}`}>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value.trim())}
            placeholder="docs"
            autoFocus
            prefix={`/~${ns.name}/`}
            aria-label="站点名称"
          />
        </Field>
      )}
    </FormDialog>
  );
}

export function Sites({ ns }: { ns: Namespace }) {
  const session = useSession();
  const manage = canManage(session, ns);
  const projects = useResource(() => api<Project[]>(`namespaces/${ns.id}/projects`), [ns.id]);
  const [creating, setCreating] = useState(false);
  const list = projects.data ?? [];
  const rootMode = ns.mode === 'root' && list.length > 0;
  const canCreate = manage && !rootMode;

  return (
    <>
      <PageHeader
        title="站点"
        description={
          <>
            <span className="mono">~{ns.name}</span> 下的静态站点
            {list.length > 0 && ns.mode && <Badge>{ns.mode === 'root' ? '根站点模式' : '子项目模式'}</Badge>}
          </>
        }
        actions={
          canCreate && (
            <Button variant="primary" icon={<Plus size={16} />} onClick={() => setCreating(true)}>
              新建站点
            </Button>
          )
        }
      />
      {projects.error && <ErrorState message={projects.error} onRetry={projects.reload} />}
      {projects.loading && !projects.data && <Spinner />}
      {projects.data && list.length === 0 && (
        <EmptyState
          icon={<Globe size={28} />}
          title="还没有站点"
          action={
            manage && (
              <Button variant="primary" icon={<Plus size={16} />} onClick={() => setCreating(true)}>
                新建站点
              </Button>
            )
          }
        >
          {manage
            ? '创建一个站点，然后上传构建好的 ZIP 包即可上线。'
            : '请联系空间管理员创建站点。成员可以发布和回滚已有站点。'}
        </EmptyState>
      )}
      {list.length > 0 && (
        <div className="site-grid">
          {list.map((p) => (
            <div key={p.id} className="site-card">
              <div className="site-card-top">
                <span className="site-icon">
                  <Globe size={18} aria-hidden />
                </span>
                {siteStatus(p)}
              </div>
              <h3>
                <a href={`#/n/${ns.name}/sites/${p.id}`} className="stretched">
                  {p.name || '根站点'}
                </a>
              </h3>
              <code className="site-path">{sitePath(ns.name, p.name)}</code>
              <div className="site-card-foot">
                <span>
                  创建于 <Time value={p.created_at} />
                </span>
                {p.current_id && !p.disabled && (
                  <a
                    className="site-visit"
                    href={siteUrl(session.me.content_url, ns.name, p.name)}
                    target="_blank"
                    rel="noreferrer"
                    title="在新标签页访问站点"
                  >
                    访问 <ArrowUpRight size={14} aria-hidden />
                  </a>
                )}
              </div>
            </div>
          ))}
          {canCreate && (
            <button type="button" className="site-card site-card-new" onClick={() => setCreating(true)}>
              <Plus size={20} aria-hidden />
              <span>新建站点</span>
            </button>
          )}
        </div>
      )}
      <CreateSiteDialog
        ns={ns}
        open={creating}
        hasProjects={list.length > 0}
        onClose={() => {
          setCreating(false);
          void projects.reload();
        }}
      />
    </>
  );
}
