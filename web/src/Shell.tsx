import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import {
  Check,
  ChevronsUpDown,
  DatabaseCog,
  FileClock,
  Gauge,
  Globe,
  KeyRound,
  LogOut,
  Menu,
  Plus,
  ScrollText,
  Search,
  ShieldCheck,
  UserCheck,
  Users,
  X,
} from 'lucide-react';
import { api } from './api';
import { Brand } from './pages/Auth';
import { AdminAudit } from './pages/admin/Audit';
import { AdminIssuers } from './pages/admin/Issuers';
import { AdminOverview } from './pages/admin/Overview';
import { AdminRequests } from './pages/admin/Requests';
import { AdminNamespaces } from './pages/admin/Namespaces';
import { AdminUsers } from './pages/admin/Users';
import { Keys } from './pages/Keys';
import { Members } from './pages/Members';
import { ProjectPage } from './pages/Project';
import { Requests } from './pages/Requests';
import { Sites } from './pages/Sites';
import { canManage, useSession } from './session';
import type { Namespace, NamespaceRequest, User } from './types';
import { Avatar, Badge, Button, EmptyState, useAction } from './ui';
import { cx, navigate, useHashPath } from './lib';

function NavLink({
  href,
  icon,
  children,
  active,
  count,
}: {
  href: string;
  icon: ReactNode;
  children: ReactNode;
  active: boolean;
  count?: number;
}) {
  return (
    <a href={'#' + href} className={cx('nav-link', active && 'active')} aria-current={active ? 'page' : undefined}>
      {icon}
      <span>{children}</span>
      {!!count && (
        <span className="nav-count" aria-label={`${count} 项待处理`}>
          {count}
        </span>
      )}
    </a>
  );
}

function NamespaceSwitcher({ current, onPick }: { current?: Namespace; onPick: (ns: Namespace) => void }) {
  const { namespaces, me } = useSession();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    document.addEventListener('mousedown', close);
    document.addEventListener('keydown', esc);
    return () => {
      document.removeEventListener('mousedown', close);
      document.removeEventListener('keydown', esc);
    };
  }, [open]);
  const list = namespaces.filter((n) => n.name.toLowerCase().includes(query.trim().toLowerCase().replace(/^~/, '')));
  return (
    <div className="ns-switcher" ref={ref}>
      <button
        type="button"
        className="ns-trigger"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label="切换空间"
        onClick={() => setOpen((o) => !o)}
      >
        <Avatar name={current?.name ?? '?'} size={28} />
        <span className="ns-trigger-text">
          <small>当前空间</small>
          <strong>{current ? '~' + current.name : '选择空间'}</strong>
        </span>
        <ChevronsUpDown size={16} aria-hidden />
      </button>
      {open && (
        <div className="ns-menu">
          <div className="ns-search">
            <Search size={14} aria-hidden />
            <input
              autoFocus
              placeholder="搜索空间"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              aria-label="搜索空间"
            />
          </div>
          <ul role="listbox" aria-label="空间">
            {list.map((n) => (
              <li key={n.id} role="option" aria-selected={n.id === current?.id}>
                <button
                  type="button"
                  onClick={() => {
                    onPick(n);
                    setOpen(false);
                    setQuery('');
                  }}
                >
                  <Avatar name={n.name} size={24} />
                  <span className="ns-option-name">
                    ~{n.name}
                    {n.id === me.user.personal_ns && <small>个人</small>}
                  </span>
                  {n.id === current?.id ? (
                    <Check size={14} aria-hidden />
                  ) : (
                    <small className="muted">{n.role === 'admin' ? '管理员' : '成员'}</small>
                  )}
                </button>
              </li>
            ))}
            {!list.length && <li className="ns-empty">没有匹配的空间</li>}
          </ul>
          <a className="ns-menu-foot" href="#/requests" onClick={() => setOpen(false)}>
            <Plus size={14} aria-hidden />
            申请新空间
          </a>
        </div>
      )}
    </div>
  );
}

function useAdminCounts(enabled: boolean, path: string) {
  const [counts, setCounts] = useState({ users: 0, requests: 0 });
  useEffect(() => {
    if (!enabled) return;
    let alive = true;
    Promise.all([api<User[]>('admin/users'), api<NamespaceRequest[]>('admin/requests')])
      .then(
        ([u, r]) =>
          alive &&
          setCounts({
            users: u.filter((x) => x.status === 'pending').length,
            requests: r.filter((x) => x.status === 'pending').length,
          }),
      )
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [enabled, path]);
  return counts;
}

const LAST_NS_KEY = 'hoshilet:last-namespace';

export function Shell() {
  const session = useSession();
  const { me, namespaces } = session;
  const path = useHashPath();
  const seg = path.split('/').filter(Boolean);
  const [drawer, setDrawer] = useState(false);
  const { busy, run } = useAction();
  const counts = useAdminCounts(session.adminMode, path);

  const routeNs = seg[0] === 'n' ? namespaces.find((n) => n.name === seg[1]) : undefined;
  const remembered = namespaces.find((n) => n.name === localStorage.getItem(LAST_NS_KEY));
  const fallbackNs = remembered ?? namespaces.find((n) => n.id === me.user.personal_ns) ?? namespaces[0];
  const currentNs = routeNs ?? fallbackNs;

  useEffect(() => {
    if (routeNs) localStorage.setItem(LAST_NS_KEY, routeNs.name);
  }, [routeNs]);

  useEffect(() => {
    if (seg.length === 0) navigate(fallbackNs ? `/n/${fallbackNs.name}/sites` : '/requests', true);
    setDrawer(false);
    window.scrollTo(0, 0);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path]);

  useEffect(() => {
    if (!session.adminMode && (seg[0] === 'admin' || (seg[0] === 'n' && !routeNs))) {
      navigate(fallbackNs ? `/n/${fallbackNs.name}/sites` : '/requests', true);
    }
  }, [session.adminMode, path, routeNs, fallbackNs]);

  function pickNamespace(ns: Namespace) {
    const section = seg[0] === 'n' && seg[2] === 'members' ? 'members' : 'sites';
    navigate(`/n/${ns.name}/${section}`);
  }

  const nsBase = currentNs ? `/n/${currentNs.name}` : '';
  const isNs = seg[0] === 'n';

  let page: ReactNode;
  if (seg[0] === 'n') {
    if (!routeNs)
      page = (
        <EmptyState
          icon={<Globe size={28} />}
          title="空间不存在或无权访问"
          action={<Button onClick={() => navigate('/')}>返回首页</Button>}
        >
          ~{seg[1]} 不在你可访问的空间列表中。
        </EmptyState>
      );
    else if (seg[2] === 'members') page = <Members ns={routeNs} />;
    else if (seg[2] === 'sites' && seg[3])
      page = <ProjectPage key={seg[3]} ns={routeNs} projectId={seg[3]} tab={seg[4] ?? 'deployments'} />;
    else page = <Sites ns={routeNs} />;
  } else if (seg[0] === 'keys') page = <Keys />;
  else if (seg[0] === 'requests') page = <Requests />;
  else if (seg[0] === 'admin' && session.adminMode) {
    if (seg[1] === 'namespaces') page = <AdminNamespaces />;
    else if (seg[1] === 'users') page = <AdminUsers />;
    else if (seg[1] === 'requests') page = <AdminRequests />;
    else if (seg[1] === 'issuers') page = <AdminIssuers />;
    else if (seg[1] === 'audit') page = <AdminAudit />;
    else page = <AdminOverview counts={counts} />;
  } else if (seg.length)
    page = <EmptyState title="页面不存在" action={<Button onClick={() => navigate('/')}>返回首页</Button>} />;

  return (
    <div className={cx('shell', drawer && 'drawer-open')}>
      <header className="topbar">
        <Button variant="ghost" aria-label="打开菜单" icon={<Menu size={20} />} onClick={() => setDrawer(true)} />
        <Brand />
      </header>
      <div className="scrim" onClick={() => setDrawer(false)} aria-hidden />
      <aside className="sidebar" aria-label="主导航">
        <div className="sidebar-head">
          <Brand />
          <Button
            className="drawer-close"
            variant="ghost"
            size="sm"
            aria-label="关闭菜单"
            icon={<X size={18} />}
            onClick={() => setDrawer(false)}
          />
        </div>
        <NamespaceSwitcher key={String(session.adminMode)} current={currentNs} onPick={pickNamespace} />
        <nav className="nav">
          {currentNs && (
            <div className="nav-group">
              <NavLink href={`${nsBase}/sites`} icon={<Globe size={18} />} active={isNs && seg[2] !== 'members'}>
                站点
              </NavLink>
              {canManage(session, currentNs) && (
                <NavLink href={`${nsBase}/members`} icon={<Users size={18} />} active={isNs && seg[2] === 'members'}>
                  空间成员
                </NavLink>
              )}
            </div>
          )}
          <div className="nav-group">
            <span className="nav-title">个人</span>
            <NavLink href="/keys" icon={<KeyRound size={18} />} active={seg[0] === 'keys'}>
              API Keys
            </NavLink>
            <NavLink href="/requests" icon={<FileClock size={18} />} active={seg[0] === 'requests'}>
              空间申请
            </NavLink>
          </div>
          {session.adminMode && (
            <div className="nav-group">
              <span className="nav-title">平台管理</span>
              <NavLink href="/admin" icon={<Gauge size={18} />} active={seg[0] === 'admin' && !seg[1]}>
                概览与策略
              </NavLink>
              <NavLink href="/admin/namespaces" icon={<Globe size={18} />} active={seg[1] === 'namespaces'}>
                空间用量
              </NavLink>
              <NavLink
                href="/admin/users"
                icon={<UserCheck size={18} />}
                active={seg[1] === 'users'}
                count={counts.users}
              >
                用户授权
              </NavLink>
              <NavLink
                href="/admin/requests"
                icon={<FileClock size={18} />}
                active={seg[0] === 'admin' && seg[1] === 'requests'}
                count={counts.requests}
              >
                空间审批
              </NavLink>
              <NavLink href="/admin/issuers" icon={<ShieldCheck size={18} />} active={seg[1] === 'issuers'}>
                CI Issuers
              </NavLink>
              <NavLink href="/admin/audit" icon={<ScrollText size={18} />} active={seg[1] === 'audit'}>
                审计日志
              </NavLink>
            </div>
          )}
        </nav>
        <footer className="sidebar-foot">
          <Avatar name={me.user.username} />
          <div className="user-meta">
            <strong>{me.user.username}</strong>
            <small>{me.admin ? <Badge tone="accent">平台管理员</Badge> : `GitLab #${me.user.gitlab_id}`}</small>
          </div>
          {me.admin && (
            <Button
              variant={session.adminMode ? 'primary' : 'ghost'}
              size="sm"
              role="switch"
              aria-label="管理员功能"
              aria-checked={session.adminMode}
              title={session.adminMode ? '关闭管理员功能' : '启用管理员功能'}
              loading={busy === 'admin-mode'}
              icon={<DatabaseCog size={16} aria-hidden />}
              onClick={() => run('admin-mode', () => session.setAdminMode(!session.adminMode))}
            />
          )}
          <Button
            variant="ghost"
            size="sm"
            aria-label="退出登录"
            title="退出登录"
            loading={busy === 'logout'}
            icon={<LogOut size={16} />}
            onClick={() => run('logout', session.logout)}
          />
        </footer>
      </aside>
      <main className="content">{page}</main>
    </div>
  );
}
