export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}

const codeMessages: Record<string, string> = {
  unauthenticated: '登录已失效，请重新登录',
  invalid_token: '登录已失效，请重新登录',
  csrf: '请求来源校验失败，请通过管理域名访问',
  not_admitted: '账户尚未获得授权',
  blocked: '账户已被停用',
  forbidden: '没有权限执行此操作',
  scope_denied: '凭据权限不足',
  session_required: '该操作需要浏览器登录会话',
  invalid_name: '名称需为 1–63 位小写字母、数字、点、下划线或连字符，并以字母或数字开头',
  name_taken: '该名称已被占用',
  pending: '同名申请已在审核中',
  invalid_scope: '无效的 Key 范围',
  invalid_expiry: '有效期必须在 1–365 天之间',
  invalid_role: '无效的角色',
  last_admin: '空间至少需要保留一名管理员',
  invalid_mode: '无效的站点模式',
  mode_conflict: '该空间已使用另一种站点模式，删除全部站点后才能切换',
  current_version: '不能删除当前上线的版本',
  invalid_state: '该版本已被清理',
  invalid_trust: '请选择可用的 Issuer，并填写数字仓库 ID 和至少一个分支',
  invalid_branch: '分支名无效：只支持精确分支名或单独的 *',
  missing_fields: '请填写全部设置项',
  invalid_limits: '保留版本数须在 1–1000 之间，大小上限须在 1 字节到 4 GiB 之间',
  not_pending: '该申请已被处理',
  invalid_user: '请输入正整数 GitLab 用户 ID',
  user_exists: '该用户已存在，请在列表中直接修改其状态',
  invalid_status: '无效的用户状态',
  bootstrap_admin: '配置文件中的初始管理员不能被停用',
  invalid_issuer: 'Issuer 必须是 HTTPS origin，JWKS URL 必须位于该 origin 之下',
  duplicate_issuer: '该 Issuer 已存在',
  recovering: '站点正在恢复上一次发布，请稍后重试',
  disabled: '站点已被平台管理员停用',
  revision_conflict: '站点已被其他操作更新，请刷新后重试',
  retention_conflict: '保留版本数为 1 时，无法在保留当前版本的同时暂存新版本',
  idempotency_conflict: '幂等键冲突，请刷新页面后重试',
  disk_full: '服务器磁盘空间不足',
  upload_timeout: '上传超时，请检查部署历史后重试',
  upload_limit: '上传不完整，或压缩包超过大小限制',
  size_limit: '解压后的大小超过平台限制',
  empty_deployment: '压缩包中没有任何文件',
  unsupported_type: '只支持 ZIP 文件',
  internal: '服务器内部错误，请查看服务端日志',
};

const notFoundMessages: Record<string, string> = {
  'User must sign in first': '该用户需要先登录一次 hoshilet',
  'Project not found': '站点不存在或已被删除',
  'Namespace not found': '空间不存在',
};

function toError(status: number, payload: unknown, fallback: string): ApiError {
  const err = (payload as { error?: { code?: string; message?: string } } | null)?.error;
  const code = err?.code ?? 'http_' + status;
  const raw = err?.message ?? fallback;
  let message = codeMessages[code] ?? notFoundMessages[raw] ?? raw;
  if (code === 'invalid_archive') message = '压缩包校验失败：' + raw.replace(/^Archive rejected:\s*/, '');
  if (code === 'not_found' && !notFoundMessages[raw]) message = '资源不存在';
  return new ApiError(status, code, message);
}

export async function api<T = unknown>(path: string, method = 'GET', data?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch('/api/v1/' + path, {
      method,
      headers: data === undefined ? {} : { 'Content-Type': 'application/json' },
      body: data === undefined ? undefined : JSON.stringify(data),
    });
  } catch {
    throw new ApiError(0, 'network', '无法连接服务器，请检查网络');
  }
  let payload: unknown = null;
  try {
    payload = await res.json();
  } catch {
    if (res.ok) throw new ApiError(res.status, 'bad_response', '服务器响应格式异常');
  }
  if (!res.ok) throw toError(res.status, payload, `请求失败（HTTP ${res.status}）`);
  return payload as T;
}

export interface UploadOptions {
  projectId: string;
  revision: number;
  activate: boolean;
  file: File;
  onProgress: (percent: number) => void;
}

export function uploadDeployment({ projectId, revision, activate, file, onProgress }: UploadOptions): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', `/api/v1/deploy?project_id=${encodeURIComponent(projectId)}&activate=${activate}`);
    xhr.setRequestHeader('Content-Type', 'application/zip');
    xhr.setRequestHeader('Idempotency-Key', crypto.randomUUID());
    xhr.setRequestHeader('If-Match', String(revision));
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100));
    };
    xhr.onerror = () => reject(new ApiError(0, 'network', '网络中断，请先检查部署历史再决定是否重试'));
    xhr.onload = () => {
      let payload: unknown = null;
      try {
        payload = JSON.parse(xhr.responseText);
      } catch {
        /* handled below */
      }
      if (xhr.status >= 400) return reject(toError(xhr.status, payload, '上传失败'));
      if (!payload) return reject(new ApiError(xhr.status, 'bad_response', '服务器响应格式异常'));
      resolve();
    };
    xhr.send(file);
  });
}

export function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
