import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, symlink, rm, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { makeClient, pack } from './src/hoshilet.ts';
test('origin validation', () => {
  for (const s of ['http://remote.example', 'https://user:pass@example.com', 'https://example.com/api'])
    assert.throws(() => makeClient(s));
  makeClient('https://example.com');
  makeClient('http://127.0.0.1:1234');
});
test('package tree and reject links', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'hoshilet-test-'));
  try {
    const source = path.join(root, 'source');
    await mkdir(source);
    await writeFile(path.join(source, 'index.html'), 'hello');
    const target = path.join(root, 'site.zip');
    await pack(source, target);
    assert.equal((await readFile(target)).subarray(0, 2).toString(), 'PK');
    await symlink('/etc/passwd', path.join(source, 'link'));
    await assert.rejects(() => pack(source, path.join(root, 'bad.zip')));
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
