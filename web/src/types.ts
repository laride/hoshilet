export type UserStatus = 'pending' | 'approved' | 'disabled';
export type Role = 'admin' | 'member';

export interface User {
  id: string;
  gitlab_id: number;
  username: string;
  personal_ns: string;
  status: UserStatus;
}

export interface Me {
  user: User;
  admin: boolean;
  content_url: string;
}

export interface Namespace {
  id: string;
  name: string;
  mode: '' | 'root' | 'projects';
  created_at: number;
  role: Role;
}

export interface Project {
  id: string;
  namespace_id: string;
  name: string;
  current_id: string;
  revision: number;
  disabled: number;
  created_at: number;
  namespace_name?: string;
  mode?: string;
}

export interface Deployment {
  id: string;
  project_id: string;
  uploaded_at: number;
  size: number;
  files: number;
  status: string;
  last_activated_at: number | null;
}

export interface Activation {
  id: string;
  project_id: string;
  deployment_id: string;
  previous_id: string;
  actor: string;
  activated_at: number;
}

export interface Member {
  namespace_id: string;
  user_id: string;
  role: Role;
  username: string;
  gitlab_id: number;
}

export interface ApiKey {
  id: string;
  name: string;
  scope: 'account' | 'namespace' | 'project';
  target: string;
  expires_at: number;
  revoked: number;
  last_used_at: number;
  created_at: number;
}

export interface NamespaceRequest {
  id: string;
  user_id: string;
  name: string;
  status: 'pending' | 'approved' | 'rejected';
  reason: string;
  created_at: number;
  username?: string;
}

export interface Issuer {
  id: string;
  url: string;
  jwks_url?: string;
  enabled: number;
}

export interface Trust {
  allow_mr_pipelines: number;
  id: string;
  project_id: string;
  issuer_id: string;
  issuer_url: string;
  repository_id: string;
  branches: string;
  tags: string;
  require_ref_protected: number;
}

export interface Settings {
  max_versions: number;
  max_bytes: number;
  retired_bytes: number;
  recovery_pending: number;
}

export interface Metrics {
  disk_free_bytes: number;
  upload_slots_in_use: number;
  uploads: { status: string; count: number }[];
  deployments: { status: string; count: number; bytes: number }[];
  pending_publications: number;
}

export interface AuditEvent {
  id: string;
  actor: string;
  action: string;
  target: string;
  created_at: number;
}

export interface NamespaceUsage {
  id: string;
  name: string;
  mode: string;
  project_count: number;
  member_count: number;
  retained_bytes: number;
  retired_bytes: number;
  used_bytes: number;
}
export interface NamespaceUsageDetail {
  namespace: { id: string; name: string; mode: string };
  projects: {
    id: string;
    name: string;
    disabled: number;
    deleted: number;
    current_id: string;
    retained_versions: number;
    retained_bytes: number;
    retired_bytes: number;
    used_bytes: number;
  }[];
  members: { user_id: string; username: string; gitlab_id: number; role: Role; status: UserStatus }[];
}
