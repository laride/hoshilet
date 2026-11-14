import { parseArgs } from 'node:util';
import { mkdtemp, lstat, readdir, rm } from 'node:fs/promises';
import { openAsBlob, createWriteStream } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { pipeline } from 'node:stream/promises';
import { randomUUID } from 'node:crypto';
import yazl from 'yazl';
import { HEADER, serverMessages } from './messages.ts';

interface ArchiveEntry {
  file: string;
  relative: string;
  dir: boolean;
}

interface TokenResponse {
  access_token: string;
}

interface ProjectResponse {
  revision: number;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

export async function pack(source: string, target: string): Promise<string> {
  const st = await lstat(source);
  if (st.isSymbolicLink() || (!st.isDirectory() && !st.isFile())) {
    throw Error('Source must be a regular directory or ZIP file');
  }
  if (st.isFile()) return source;
  const zip = new yazl.ZipFile();
  // Validate the complete input tree before starting the output stream.
  const entries: ArchiveEntry[] = [];
  async function walk(dir: string): Promise<void> {
    for (const name of (await readdir(dir)).sort()) {
      const file = path.join(dir, name);
      const info = await lstat(file);
      const relative = path.relative(source, file).split(path.sep).join('/');
      if (info.isSymbolicLink() || (!info.isDirectory() && !info.isFile())) {
        throw Error(`Non-regular file: ${relative}`);
      }
      entries.push({ file, relative, dir: info.isDirectory() });
      if (info.isDirectory()) await walk(file);
    }
  }
  await walk(source);
  const finished = pipeline(zip.outputStream, createWriteStream(target, { flags: 'wx', mode: 0o600 }));
  for (const entry of entries) {
    if (entry.dir) zip.addEmptyDirectory(entry.relative);
    else zip.addFile(entry.file, entry.relative, { forceZip64Format: false });
  }
  zip.end({ forceZip64Format: false, comment: '' });
  await finished;
  return target;
}

export function makeClient(server: string, token?: string) {
  const base = new URL(server);
  if (
    !['https:', 'http:'].includes(base.protocol) ||
    base.username ||
    base.password ||
    base.search ||
    base.hash ||
    base.pathname !== '/'
  ) {
    throw Error('--server must be an HTTP(S) origin');
  }
  if (base.protocol === 'http:' && !['localhost', '127.0.0.1', '[::1]'].includes(base.hostname)) {
    throw Error('HTTPS is required except on loopback');
  }
  const showMessage = serverMessages();
  return {
    token,
    async request<T = unknown>(
      method: string,
      route: string,
      data?: unknown,
      extra: Record<string, string> = {},
    ): Promise<T> {
      const headers: Record<string, string> = { ...extra };
      if (this.token) headers.Authorization = 'Bearer ' + this.token;
      let body: Blob | string | undefined;
      if (data instanceof Blob) {
        body = data;
        headers['Content-Type'] = 'application/zip';
      } else if (data !== undefined) {
        body = JSON.stringify(data);
        headers['Content-Type'] = 'application/json';
      }
      const response = await fetch(new URL('/api/v1/' + route, base), {
        method,
        headers,
        body,
        redirect: 'manual',
        signal: AbortSignal.timeout(720000),
      });
      showMessage(response.headers.get(HEADER));
      if (!response.body) throw Error('Empty response body');
      let total = 0;
      const chunks: Uint8Array[] = [];
      for await (const chunk of response.body) {
        total += chunk.byteLength;
        if (total > 4 * 1024 * 1024) throw Error('Response too large');
        chunks.push(chunk);
      }
      const value: unknown = JSON.parse(Buffer.concat(chunks).toString('utf8'));
      if (!response.ok) {
        const error = isRecord(value) && isRecord(value.error) ? value.error : undefined;
        const code = typeof error?.code === 'string' ? error.code : 'request_failed';
        const message = typeof error?.message === 'string' ? error.message : 'Request failed';
        throw Error(`${response.status} ${code}: ${message}`);
      }
      return value as T;
    },
  };
}

export async function main(argv: string[] = process.argv.slice(2)): Promise<number> {
  const { values: v, positionals: p } = parseArgs({
    args: argv,
    allowPositionals: true,
    options: {
      server: { type: 'string' },
      json: { type: 'boolean' },
      namespace: { type: 'string' },
      project: { type: 'string' },
      'project-id': { type: 'string' },
      'no-activate': { type: 'boolean' },
      oidc: { type: 'boolean' },
      'idempotency-key': { type: 'string' },
      revision: { type: 'string' },
      help: { type: 'boolean' },
    },
  });
  if (v.help) {
    console.log(
      'hoshilet [--server ORIGIN] [--json] deploy DIRECTORY_OR_ZIP [--namespace NAME] [--project NAME] [--project-id ID] [--no-activate] [--oidc] [--revision N] [--idempotency-key KEY]\nhoshilet deployments list --project-id ID\nhoshilet activate DEPLOYMENT_ID --project-id ID [--revision N]\nEnvironment: HOSHILET_SERVER, HOSHILET_API_KEY, HOSHILET_ID_TOKEN',
    );
    return 0;
  }
  const server = v.server || process.env.HOSHILET_SERVER;
  if (!server) throw Error('Set HOSHILET_SERVER or --server');
  const client = makeClient(server, process.env.HOSHILET_API_KEY);
  let result: unknown;
  if (p[0] === 'deploy') {
    if (p.length !== 2) throw Error('deploy requires a directory or ZIP file');
    const idem = v['idempotency-key'] || randomUUID();
    if (v.oidc) {
      if (!process.env.HOSHILET_ID_TOKEN || !v['project-id'])
        throw Error('OIDC requires HOSHILET_ID_TOKEN and --project-id');
      const exchanged = await client.request<TokenResponse>(
        'POST',
        'oidc/exchange',
        {
          token: process.env.HOSHILET_ID_TOKEN,
          project_id: v['project-id'],
        },
        { 'Idempotency-Key': idem + '-oidc' },
      );
      client.token = exchanged.access_token;
    }
    if (!client.token) throw Error('Set HOSHILET_API_KEY or use --oidc');
    const qs = new URLSearchParams({ activate: v['no-activate'] ? 'false' : 'true' });
    for (const field of ['namespace', 'project', 'project-id'] as const) {
      const value = v[field];
      if (value !== undefined) qs.set(field.replace('-', '_'), value);
    }
    const headers: Record<string, string> = { 'Idempotency-Key': idem };
    if (v.revision !== undefined) {
      if (!/^\d+$/.test(v.revision)) throw Error('Invalid revision');
      headers['If-Match'] = v.revision;
    }
    const tmp = await mkdtemp(path.join(tmpdir(), 'hoshilet-'));
    try {
      const archive = await pack(path.resolve(p[1]), path.join(tmp, 'site.zip'));
      result = await client.request('POST', 'deploy?' + qs, await openAsBlob(archive), headers);
    } finally {
      await rm(tmp, { recursive: true, force: true });
    }
  } else {
    if (!v['project-id'] || !client.token) throw Error('--project-id and HOSHILET_API_KEY required');
    const pid = encodeURIComponent(v['project-id']);
    if (p.join(' ') === 'deployments list') {
      result = await client.request('GET', `projects/${pid}/deployments`);
    } else if (p[0] === 'activate' && p.length === 2) {
      const revision =
        v.revision === undefined
          ? (await client.request<ProjectResponse>('GET', `projects/${pid}`)).revision
          : Number(v.revision);
      if (!Number.isSafeInteger(revision) || revision < 0) throw Error('Invalid revision');
      result = await client.request('POST', `projects/${pid}/activate`, { deployment_id: p[1], revision });
    } else {
      throw Error('Unknown command; use --help');
    }
  }
  console.log(JSON.stringify(result, null, v.json ? undefined : 2));
  return isRecord(result) &&
    typeof result.status === 'string' &&
    ['failed', 'receiving', 'validating', 'publishing'].includes(result.status)
    ? 1
    : 0;
}
