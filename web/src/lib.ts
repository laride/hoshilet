import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { errorMessage } from './api';

const dateFormat = new Intl.DateTimeFormat('zh-CN', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
});
const relativeFormat = new Intl.RelativeTimeFormat('zh-CN', { numeric: 'auto' });

export function formatDate(sec: number | null | undefined): string {
  return sec ? dateFormat.format(new Date(sec * 1000)) : '—';
}

export function formatRelative(sec: number | null | undefined): string {
  if (!sec) return '—';
  const diff = sec - Date.now() / 1000;
  const abs = Math.abs(diff);
  const units: [number, Intl.RelativeTimeFormatUnit][] = [
    [60, 'second'],
    [3600, 'minute'],
    [86400, 'hour'],
    [86400 * 30, 'day'],
    [86400 * 365, 'month'],
  ];
  if (abs < 45) return diff < 0 ? '刚刚' : '即将';
  let divisor = 1;
  for (const [limit, unit] of units) {
    if (abs < limit) return relativeFormat.format(Math.round(diff / divisor), unit);
    divisor = limit;
  }
  return relativeFormat.format(Math.round(diff / (86400 * 365)), 'year');
}

export function formatBytes(bytes: number | null | undefined): string {
  if (!bytes) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let i = 0;
  let n = bytes;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n >= 100 || i === 0 ? Math.round(n) : n.toFixed(1)} ${units[i]}`;
}

export const shortId = (id: string | null | undefined) => (id ? id.slice(0, 8) : '—');

export const SLUG_RE = /^[a-z0-9][a-z0-9._-]{0,62}$/;
export const validSlug = (s: string) => SLUG_RE.test(s) && s !== '.' && s !== '..';

export function siteUrl(contentUrl: string, ns: string, project: string): string {
  return `${contentUrl.replace(/\/$/, '')}/~${ns}/${project ? project + '/' : ''}`;
}

export function sitePath(ns: string, project: string): string {
  return `/~${ns}/${project ? project + '/' : ''}`;
}

export function parseBranches(raw: string): string[] {
  try {
    const v = JSON.parse(raw);
    return Array.isArray(v) ? v.map(String) : [raw];
  } catch {
    return [raw];
  }
}

export function cx(...names: (string | false | null | undefined)[]): string {
  return names.filter(Boolean).join(' ');
}

/* ---------- Hash router ---------- */

function subscribeHash(cb: () => void) {
  window.addEventListener('hashchange', cb);
  return () => window.removeEventListener('hashchange', cb);
}

export function useHashPath(): string {
  return useSyncExternalStore(subscribeHash, () => location.hash.replace(/^#/, '') || '/');
}

export function navigate(path: string, replace = false) {
  if (replace) history.replaceState(null, '', '#' + path);
  else location.hash = path;
  if (replace) window.dispatchEvent(new HashChangeEvent('hashchange'));
}

/* ---------- Data loading ---------- */

export interface Resource<T> {
  data: T | undefined;
  error: string;
  loading: boolean;
  reload: () => Promise<void>;
}

export function useResource<T>(load: (() => Promise<T>) | null, deps: unknown[]): Resource<T> {
  const [data, setData] = useState<T>();
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(!!load);
  const loadRef = useRef(load);
  loadRef.current = load;
  const seq = useRef(0);

  const reload = useCallback(async () => {
    const fn = loadRef.current;
    if (!fn) return;
    const n = ++seq.current;
    setLoading(true);
    try {
      const v = await fn();
      if (n === seq.current) {
        setData(v);
        setError('');
      }
    } catch (e) {
      if (n === seq.current) setError(errorMessage(e));
    } finally {
      if (n === seq.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    setData(undefined);
    setError('');
    void reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  return { data, error, loading, reload };
}
