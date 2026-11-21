import test from 'node:test';
import assert from 'node:assert/strict';
import tls from 'node:tls';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { appendCACertificates } from './src/ca.ts';

test('older runtimes work without the option and report the startup alternative when requested', async () => {
  for (const api of ['getCACertificates', 'setDefaultCACertificates'] as const) {
    const original = tls[api];
    try {
      Reflect.set(tls, api, undefined);
      await appendCACertificates();
      await assert.rejects(
        () => appendCACertificates(['company.pem']),
        /--extra-ca-certs requires Node.js .*NODE_EXTRA_CA_CERTS=/,
      );
    } finally {
      Reflect.set(tls, api, original);
    }
  }
});

test('invalid CA bundles fail before changing default trust', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'hoshilet-ca-test-'));
  const defaults = tls.getCACertificates('default');
  try {
    const valid = path.join(root, 'valid.pem');
    const invalid = path.join(root, 'invalid.pem');
    await writeFile(valid, tls.rootCertificates[0]);
    for (const pem of ['', 'not PEM', '-----BEGIN CERTIFICATE-----\nbad\n-----END CERTIFICATE-----']) {
      await writeFile(invalid, pem);
      await assert.rejects(() => appendCACertificates([valid, invalid]), /Cannot load --extra-ca-certs .*invalid.pem/);
      assert.deepEqual(tls.getCACertificates('default'), defaults);
    }
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
