import { StrictMode, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { ApiError, api, errorMessage } from './api';
import { AccountGate, LoginScreen, StartupError } from './pages/Auth';
import { Shell } from './Shell';
import { SessionContext } from './session';
import type { Session } from './session';
import type { Me, Namespace } from './types';
import { FeedbackProvider, Spinner } from './ui';
import './style.css';

type Boot =
  | { state: 'loading' }
  | { state: 'anonymous' }
  | { state: 'error'; message: string }
  | { state: 'ready'; me: Me; namespaces: Namespace[]; adminMode: boolean };

const ADMIN_MODE_KEY = 'hoshilet:admin-mode-user';

function App() {
  const [boot, setBoot] = useState<Boot>({ state: 'loading' });
  const modeEpoch = useRef(0);

  const load = useCallback(async () => {
    const epoch = ++modeEpoch.current;
    setBoot({ state: 'loading' });
    try {
      const me = await api<Me>('me');
      if (epoch !== modeEpoch.current) return;
      const adminMode =
        me.admin && me.user?.status === 'approved' && sessionStorage.getItem(ADMIN_MODE_KEY) === me.user.id;
      if (!adminMode) sessionStorage.removeItem(ADMIN_MODE_KEY);
      const namespaces =
        me.user?.status === 'approved'
          ? await api<Namespace[]>(`namespaces?scope=${adminMode ? 'all' : 'member'}`)
          : [];
      if (epoch !== modeEpoch.current) return;
      setBoot({ state: 'ready', me, namespaces, adminMode });
    } catch (e) {
      if (epoch !== modeEpoch.current) return;
      if (e instanceof ApiError && e.status === 401) {
        sessionStorage.removeItem(ADMIN_MODE_KEY);
        setBoot({ state: 'anonymous' });
      } else setBoot({ state: 'error', message: errorMessage(e) });
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const session = useMemo<Session | null>(() => {
    if (boot.state !== 'ready') return null;
    return {
      me: boot.me,
      adminMode: boot.adminMode,
      async setAdminMode(enabled) {
        if (enabled && !boot.me.admin) return;
        const epoch = ++modeEpoch.current;
        const namespaces = await api<Namespace[]>(`namespaces?scope=${enabled ? 'all' : 'member'}`);
        if (epoch !== modeEpoch.current) return;
        if (enabled) sessionStorage.setItem(ADMIN_MODE_KEY, boot.me.user.id);
        else sessionStorage.removeItem(ADMIN_MODE_KEY);
        setBoot((b) =>
          b.state === 'ready' && b.me.user.id === boot.me.user.id ? { ...b, adminMode: enabled, namespaces } : b,
        );
      },
      namespaces: boot.namespaces,
      async refreshNamespaces() {
        const namespaces = await api<Namespace[]>(`namespaces?scope=${boot.adminMode ? 'all' : 'member'}`);
        setBoot((b) =>
          b.state === 'ready' && b.me.user.id === boot.me.user.id && b.adminMode === boot.adminMode
            ? { ...b, namespaces }
            : b,
        );
      },
      async logout() {
        ++modeEpoch.current;
        await api('logout', 'POST');
        sessionStorage.removeItem(ADMIN_MODE_KEY);
        location.hash = '';
        setBoot({ state: 'anonymous' });
      },
    };
  }, [boot]);

  if (boot.state === 'loading')
    return (
      <div className="boot">
        <Spinner label="正在连接 hoshilet…" />
      </div>
    );
  if (boot.state === 'anonymous') return <LoginScreen />;
  if (boot.state === 'error') return <StartupError message={boot.message} onRetry={load} />;
  if (boot.me.user.status !== 'approved') return <AccountGate me={boot.me} onRefresh={load} />;
  return (
    <SessionContext.Provider value={session}>
      <Shell />
    </SessionContext.Provider>
  );
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <FeedbackProvider>
      <App />
    </FeedbackProvider>
  </StrictMode>,
);
