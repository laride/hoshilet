import { createContext, useCallback, useContext, useEffect, useId, useRef, useState } from 'react';
import type { ButtonHTMLAttributes, FormEvent, InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from 'react';
import { AlertTriangle, Check, CheckCircle2, Copy, Info, Loader2, X, XCircle } from 'lucide-react';
import { errorMessage } from './api';
import { cx, formatDate, formatRelative } from './lib';

/* ---------- Primitives ---------- */

type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'danger-ghost';

export function Button({
  variant = 'secondary',
  size = 'md',
  loading,
  icon,
  children,
  className,
  disabled,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant;
  size?: 'sm' | 'md';
  loading?: boolean;
  icon?: ReactNode;
}) {
  return (
    <button
      type="button"
      className={cx('btn', `btn-${variant}`, size === 'sm' && 'btn-sm', !children && 'btn-icon', className)}
      disabled={disabled || loading}
      {...rest}
    >
      {loading ? <Loader2 className="spin" size={16} aria-hidden /> : icon}
      {children}
    </button>
  );
}

export type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'accent';

export function Badge({ tone = 'neutral', dot, children }: { tone?: Tone; dot?: boolean; children: ReactNode }) {
  return (
    <span className={cx('badge', `tone-${tone}`)}>
      {dot && <span className="badge-dot" aria-hidden />}
      {children}
    </span>
  );
}

export function Spinner({ label = '加载中…' }: { label?: string }) {
  return (
    <div className="spinner" role="status">
      <Loader2 className="spin" size={18} aria-hidden />
      <span>{label}</span>
    </div>
  );
}

export function Avatar({ name, size = 32 }: { name: string; size?: number }) {
  const text = (name || '?').slice(0, 2).toUpperCase();
  let hash = 0;
  for (const c of name) hash = (hash * 31 + c.charCodeAt(0)) % 360;
  return (
    <span className={cx('avatar', `avatar-h${Math.floor(hash / 45)}`, size <= 28 && 'avatar-sm')} aria-hidden>
      {text}
    </span>
  );
}

export function Time({ value, relative = true }: { value: number | null | undefined; relative?: boolean }) {
  if (!value) return <span className="muted">—</span>;
  return (
    <time dateTime={new Date(value * 1000).toISOString()} title={formatDate(value)}>
      {relative ? formatRelative(value) : formatDate(value)}
    </time>
  );
}

export function Callout({
  tone = 'info',
  title,
  children,
  action,
}: {
  tone?: 'info' | 'warning' | 'danger' | 'success';
  title?: ReactNode;
  children?: ReactNode;
  action?: ReactNode;
}) {
  const Icon =
    tone === 'danger' ? XCircle : tone === 'warning' ? AlertTriangle : tone === 'success' ? CheckCircle2 : Info;
  return (
    <div className={cx('callout', `callout-${tone}`)} role={tone === 'danger' ? 'alert' : undefined}>
      <Icon size={18} className="callout-icon" aria-hidden />
      <div className="callout-body">
        {title && <strong>{title}</strong>}
        {children && <div>{children}</div>}
      </div>
      {action}
    </div>
  );
}

export function EmptyState({
  icon,
  title,
  children,
  action,
}: {
  icon?: ReactNode;
  title: string;
  children?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="empty">
      {icon && <div className="empty-icon">{icon}</div>}
      <h3>{title}</h3>
      {children && <p>{children}</p>}
      {action && <div className="empty-action">{action}</div>}
    </div>
  );
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <Callout
      tone="danger"
      title="加载失败"
      action={
        onRetry && (
          <Button size="sm" onClick={onRetry}>
            重试
          </Button>
        )
      }
    >
      {message}
    </Callout>
  );
}

export function Card({
  title,
  description,
  actions,
  children,
  className,
  flush,
}: {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children?: ReactNode;
  className?: string;
  flush?: boolean;
}) {
  return (
    <section className={cx('card', flush && 'card-flush', className)}>
      {(title || actions) && (
        <header className="card-head">
          <div>
            {title && <h2>{title}</h2>}
            {description && <p>{description}</p>}
          </div>
          {actions && <div className="card-actions">{actions}</div>}
        </header>
      )}
      {children}
    </section>
  );
}

export function PageHeader({
  title,
  description,
  actions,
  crumbs,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  crumbs?: ReactNode;
}) {
  return (
    <header className="page-head">
      <div className="page-head-main">
        {crumbs && (
          <nav className="crumbs" aria-label="面包屑">
            {crumbs}
          </nav>
        )}
        <h1>{title}</h1>
        {description && <p>{description}</p>}
      </div>
      {actions && <div className="page-actions">{actions}</div>}
    </header>
  );
}

export function Tabs({ items, current }: { items: { id: string; label: ReactNode; href: string }[]; current: string }) {
  return (
    <nav className="tabs" aria-label="页面导航">
      {items.map((t) => (
        <a
          key={t.id}
          href={'#' + t.href}
          className={cx('tab', current === t.id && 'active')}
          aria-current={current === t.id ? 'page' : undefined}
        >
          {t.label}
        </a>
      ))}
    </nav>
  );
}

export function Segmented<T extends string>({
  value,
  onChange,
  options,
  label,
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: ReactNode }[];
  label: string;
}) {
  return (
    <div className="segmented" role="radiogroup" aria-label={label}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          className={cx(value === o.value && 'active')}
          onClick={() => onChange(o.value)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function CopyButton({ value, label = '复制', compact }: { value: string; label?: string; compact?: boolean }) {
  const [done, setDone] = useState(false);
  const toast = useToast();
  return (
    <Button
      size="sm"
      variant="ghost"
      aria-label={label}
      title={label}
      icon={done ? <Check size={14} /> : <Copy size={14} />}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(value);
          setDone(true);
          setTimeout(() => setDone(false), 1500);
        } catch {
          toast.error('复制失败，请手动选择文本');
        }
      }}
    >
      {compact ? undefined : done ? '已复制' : label}
    </Button>
  );
}

export function IdChip({ value, full }: { value: string; full?: boolean }) {
  return (
    <span className="id-chip">
      <code title={value}>{full ? value : value.slice(0, 8)}</code>
      <CopyButton value={value} label="复制 ID" compact />
    </span>
  );
}

/* ---------- Form controls ---------- */

export function Field({
  label,
  hint,
  error,
  children,
  optional,
}: {
  label: ReactNode;
  hint?: ReactNode;
  error?: string;
  children: ReactNode;
  optional?: boolean;
}) {
  return (
    <label className={cx('field', error && 'field-invalid')}>
      <span className="field-label">
        {label}
        {optional && <span className="muted">（可选）</span>}
      </span>
      {children}
      {error ? <span className="field-error">{error}</span> : hint && <span className="field-hint">{hint}</span>}
    </label>
  );
}

export function Input({ prefix, ...props }: InputHTMLAttributes<HTMLInputElement> & { prefix?: string }) {
  if (!prefix) return <input className="input" autoComplete="off" {...props} />;
  return (
    <span className="input input-group">
      <span className="input-prefix">{prefix}</span>
      <input autoComplete="off" {...props} />
    </span>
  );
}

export function Select({ children, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select className="input select" {...props}>
      {children}
    </select>
  );
}

export function Switch({
  checked,
  onChange,
  label,
  description,
  disabled,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: ReactNode;
  description?: ReactNode;
  disabled?: boolean;
}) {
  const id = useId();
  return (
    <div className="switch-row">
      <div>
        <label htmlFor={id} className="switch-label">
          {label}
        </label>
        {description && <p className="field-hint">{description}</p>}
      </div>
      <input
        id={id}
        type="checkbox"
        role="switch"
        className="switch"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
    </div>
  );
}

export function RadioCards<T extends string>({
  name,
  value,
  onChange,
  options,
}: {
  name: string;
  value: T;
  onChange: (v: T) => void;
  options: { value: T; title: ReactNode; description?: ReactNode; disabled?: boolean }[];
}) {
  return (
    <div className="radio-cards">
      {options.map((o) => (
        <label key={o.value} className={cx('radio-card', value === o.value && 'checked', o.disabled && 'disabled')}>
          <input
            type="radio"
            name={name}
            value={o.value}
            checked={value === o.value}
            disabled={o.disabled}
            onChange={() => onChange(o.value)}
          />
          <span>
            <strong>{o.title}</strong>
            {o.description && <small>{o.description}</small>}
          </span>
        </label>
      ))}
    </div>
  );
}

/* ---------- Dialog ---------- */

export function Dialog({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  size = 'md',
}: {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  size?: 'sm' | 'md' | 'lg';
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (open && !el.open) el.showModal();
    if (!open && el.open) el.close();
  }, [open]);
  return (
    <dialog
      ref={ref}
      className={cx('dialog', `dialog-${size}`)}
      aria-labelledby={titleId}
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      {open && (
        <div className="dialog-inner">
          <header className="dialog-head">
            <div>
              <h2 id={titleId}>{title}</h2>
              {description && <p>{description}</p>}
            </div>
            <Button variant="ghost" size="sm" aria-label="关闭" icon={<X size={16} />} onClick={onClose} />
          </header>
          <div className="dialog-body">{children}</div>
          {footer && <footer className="dialog-foot">{footer}</footer>}
        </div>
      )}
    </dialog>
  );
}

export function FormDialog({
  open,
  onClose,
  title,
  description,
  submitLabel = '保存',
  onSubmit,
  children,
  danger,
  canSubmit = true,
  size,
}: {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  submitLabel?: string;
  onSubmit: () => Promise<void>;
  children: ReactNode;
  danger?: boolean;
  canSubmit?: boolean;
  size?: 'sm' | 'md' | 'lg';
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const formId = useId();
  useEffect(() => {
    if (open) setError('');
  }, [open]);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await onSubmit();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog
      open={open}
      onClose={() => !busy && onClose()}
      title={title}
      description={description}
      size={size}
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>
            取消
          </Button>
          <Button
            type="submit"
            form={formId}
            variant={danger ? 'danger' : 'primary'}
            loading={busy}
            disabled={!canSubmit}
          >
            {submitLabel}
          </Button>
        </>
      }
    >
      <form id={formId} className="form-stack" onSubmit={submit} noValidate>
        {children}
        {error && <Callout tone="danger">{error}</Callout>}
      </form>
    </Dialog>
  );
}

/* ---------- Confirm ---------- */

interface ConfirmOptions {
  title: string;
  description?: ReactNode;
  confirmLabel?: string;
  danger?: boolean;
  typeToConfirm?: string;
  action: () => Promise<unknown>;
}

const ConfirmContext = createContext<(o: ConfirmOptions) => void>(() => {});
export const useConfirm = () => useContext(ConfirmContext);

function ConfirmHost({ options, onClose }: { options: ConfirmOptions | null; onClose: () => void }) {
  const [typed, setTyped] = useState('');
  useEffect(() => setTyped(''), [options]);
  return (
    <FormDialog
      open={!!options}
      onClose={onClose}
      title={options?.title}
      size="sm"
      danger={options?.danger}
      submitLabel={options?.confirmLabel ?? '确认'}
      canSubmit={!options?.typeToConfirm || typed === options.typeToConfirm}
      onSubmit={async () => {
        await options?.action();
        onClose();
      }}
    >
      {options?.description && <div className="confirm-text">{options.description}</div>}
      {options?.typeToConfirm && (
        <Field
          label={
            <>
              请输入 <code>{options.typeToConfirm}</code> 以确认
            </>
          }
        >
          <Input value={typed} onChange={(e) => setTyped(e.target.value)} autoFocus />
        </Field>
      )}
    </FormDialog>
  );
}

/* ---------- Toast ---------- */

interface ToastItem {
  id: number;
  tone: 'success' | 'error' | 'info';
  message: string;
}
interface ToastApi {
  success: (m: string) => void;
  error: (m: string) => void;
  info: (m: string) => void;
}

const ToastContext = createContext<ToastApi>({ success() {}, error() {}, info() {} });
export const useToast = () => useContext(ToastContext);

export function FeedbackProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const [confirm, setConfirm] = useState<ConfirmOptions | null>(null);
  const counter = useRef(0);
  const dismiss = useCallback((id: number) => setToasts((list) => list.filter((t) => t.id !== id)), []);
  const push = useCallback(
    (tone: ToastItem['tone'], message: string) => {
      const id = ++counter.current;
      setToasts((list) => [...list.slice(-3), { id, tone, message }]);
      setTimeout(() => dismiss(id), tone === 'error' ? 8000 : 4000);
    },
    [dismiss],
  );
  const [api] = useState<ToastApi>(() => ({
    success: (m) => push('success', m),
    error: (m) => push('error', m),
    info: (m) => push('info', m),
  }));
  return (
    <ToastContext.Provider value={api}>
      <ConfirmContext.Provider value={setConfirm}>
        {children}
        <ConfirmHost options={confirm} onClose={() => setConfirm(null)} />
        <div className="toasts">
          {toasts.map((t) => (
            <div key={t.id} className={cx('toast', `toast-${t.tone}`)} role={t.tone === 'error' ? 'alert' : 'status'}>
              {t.tone === 'success' ? (
                <CheckCircle2 size={18} aria-hidden />
              ) : t.tone === 'error' ? (
                <XCircle size={18} aria-hidden />
              ) : (
                <Info size={18} aria-hidden />
              )}
              <span>{t.message}</span>
              <button type="button" className="toast-close" aria-label="关闭通知" onClick={() => dismiss(t.id)}>
                <X size={14} />
              </button>
            </div>
          ))}
        </div>
      </ConfirmContext.Provider>
    </ToastContext.Provider>
  );
}

/** Runs a mutation, reporting success or failure through toasts. */
export function useAction() {
  const toast = useToast();
  const [busy, setBusy] = useState<string | null>(null);
  const run = useCallback(
    async (key: string, fn: () => Promise<unknown>, success?: string) => {
      setBusy(key);
      try {
        await fn();
        if (success) toast.success(success);
        return true;
      } catch (e) {
        toast.error(errorMessage(e));
        return false;
      } finally {
        setBusy(null);
      }
    },
    [toast],
  );
  return { busy, run };
}
