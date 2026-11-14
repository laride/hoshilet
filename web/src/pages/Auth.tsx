import { useState } from 'react';
import {
  ArrowRight,
  Clock3,
  GitBranch,
  History,
  LogOut,
  RefreshCw,
  ShieldCheck,
  ShieldOff,
  UploadCloud,
} from 'lucide-react';
import { api } from '../api';
import type { Me } from '../types';
import { Button, Callout } from '../ui';

export function Brand() {
  return (
    <div className="brand">
      <span className="brand-mark" aria-hidden>
        H
      </span>
      <span className="brand-name">
        hoshi<b>let</b>
      </span>
    </div>
  );
}

export function LoginScreen() {
  return (
    <div className="auth">
      <section className="auth-hero">
        <Brand />
        <div className="auth-hero-body">
          <h1>静态站点，发布到你自己的空间。</h1>
          <p>上传构建产物即可上线，随时回滚到任意历史版本。支持 API Key 与 GitLab CI OIDC 自动发布。</p>
          <ul className="auth-features">
            <li>
              <UploadCloud size={18} aria-hidden />
              ZIP 上传，原子切换上线
            </li>
            <li>
              <History size={18} aria-hidden />
              完整版本历史，一键回滚
            </li>
            <li>
              <GitBranch size={18} aria-hidden />
              GitLab CI 免密钥发布
            </li>
          </ul>
        </div>
        <small className="auth-foot">hoshilet · 单机静态托管平台</small>
      </section>
      <section className="auth-panel">
        <div className="auth-card">
          <h2>登录管理后台</h2>
          <p>使用 GitLab 账户验证身份。首次登录后需由平台管理员授权。</p>
          <a className="btn btn-primary btn-block btn-lg" href="/auth/login">
            使用 GitLab 登录
            <ArrowRight size={16} aria-hidden />
          </a>
          <small className="muted">仅读取 GitLab 用户 ID 与用户名，不保存 access token。</small>
        </div>
      </section>
    </div>
  );
}

export function AccountGate({ me, onRefresh }: { me: Me; onRefresh: () => Promise<void> }) {
  const pending = me.user.status === 'pending';
  const [busy, setBusy] = useState<'refresh' | 'logout' | null>(null);
  return (
    <div className="center-screen">
      <div className="gate-card">
        <Brand />
        <div className={pending ? 'gate-icon tone-warning' : 'gate-icon tone-danger'}>
          {pending ? <Clock3 size={28} /> : <ShieldOff size={28} />}
        </div>
        <h1>{pending ? '等待管理员授权' : '账户已停用'}</h1>
        <p>
          {pending
            ? '你的账户已登记。平台管理员批准后，系统会自动为你创建个人空间。'
            : '该账户的发布权限已被撤销，已有 API Key 也已失效。如有疑问请联系平台管理员。'}
        </p>
        <dl className="gate-meta">
          <div>
            <dt>GitLab 用户</dt>
            <dd>{me.user.username}</dd>
          </div>
          <div>
            <dt>GitLab ID</dt>
            <dd>
              <code>{me.user.gitlab_id}</code>
            </dd>
          </div>
        </dl>
        {pending && <Callout tone="info">把你的 GitLab ID 告诉管理员，可以加快审批。</Callout>}
        <div className="gate-actions">
          <Button
            variant="primary"
            icon={<RefreshCw size={16} />}
            loading={busy === 'refresh'}
            onClick={async () => {
              setBusy('refresh');
              await onRefresh();
              setBusy(null);
            }}
          >
            刷新授权状态
          </Button>
          <Button
            variant="ghost"
            icon={<LogOut size={16} />}
            loading={busy === 'logout'}
            onClick={async () => {
              setBusy('logout');
              try {
                await api('logout', 'POST');
              } finally {
                await onRefresh();
              }
            }}
          >
            退出登录
          </Button>
        </div>
      </div>
    </div>
  );
}

export function StartupError({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="center-screen">
      <div className="gate-card">
        <Brand />
        <div className="gate-icon tone-danger">
          <ShieldCheck size={28} />
        </div>
        <h1>无法连接管理服务</h1>
        <p>{message}</p>
        <div className="gate-actions">
          <Button variant="primary" icon={<RefreshCw size={16} />} onClick={onRetry}>
            重试
          </Button>
        </div>
      </div>
    </div>
  );
}
