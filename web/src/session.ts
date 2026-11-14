import { createContext, useContext } from 'react';
import type { Me, Namespace } from './types';

export interface Session {
  me: Me;
  adminMode: boolean;
  setAdminMode: (enabled: boolean) => Promise<void>;
  namespaces: Namespace[];
  refreshNamespaces: () => Promise<void>;
  logout: () => Promise<void>;
}

export const SessionContext = createContext<Session | null>(null);

export function useSession(): Session {
  const s = useContext(SessionContext);
  if (!s) throw new Error('SessionContext missing');
  return s;
}

export function canManage(session: Session, ns: Namespace | undefined): boolean {
  return session.adminMode || ns?.role === 'admin';
}
