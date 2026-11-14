import test from 'node:test';
import assert from 'node:assert/strict';
import { decodeMessage, formatMessage, serverMessages } from './src/messages.ts';
import { makeClient } from './src/hoshilet.ts';
const encoded = (s: string) => Buffer.from(s).toString('base64');
test('message validation and marker widths', () => {
  assert.equal(decodeMessage(encoded('hello\n世界')), 'hello\n世界');
  assert.equal(decodeMessage(encoded('x'.repeat(512))).length, 512);
  for (const s of [
    'bad-base64',
    encoded('x'.repeat(513)),
    encoded('\x1b[2J'),
    encoded('\x9b31m'),
    encoded('a\rb'),
    encoded('a\tb'),
    '/w==',
  ])
    assert.throws(() => decodeMessage(s));
  for (const width of [1, 3, 20, 80, 100, 150]) {
    const lines = formatMessage('first\nsecond', width).trimEnd().split('\n');
    assert.equal(lines[0].length, Math.min(width, 100));
    assert.equal(lines.at(-1)?.length, Math.min(width, 100));
    assert.deepEqual(lines.slice(1, 3), ['first', 'second']);
  }
});
test('messages print on success, error, redirects and non-JSON; same message deduplicated', async () => {
  const oldFetch = globalThis.fetch,
    oldWrite = process.stderr.write;
  let output = '';
  process.stderr.write = (s) => {
    output += s;
    return true;
  };
  try {
    for (const status of [200, 400, 500, 302, 204]) {
      output = '';
      globalThis.fetch = async () =>
        new Response(
          status === 204
            ? null
            : status === 500
              ? 'not JSON'
              : status === 200
                ? '[]'
                : '{"error":{"message":"denied"}}',
          { status, headers: { 'X-Hoshilet-Server-Message': encoded('server\nnotice') } },
        );
      const c = makeClient('http://127.0.0.1:1234', 'key');
      if (status === 200) assert.deepEqual(await c.request('GET', 'projects'), []);
      else await assert.rejects(() => c.request('GET', 'projects'));
      assert.match(output, /server\nnotice/);
    }
    output = '';
    const show = serverMessages();
    show(encoded('hello'));
    show(encoded('hello'));
    show(encoded('\x1b[2J'));
    assert.equal(output.split('hello').length - 1, 1);
    assert.ok(!output.includes('\x1b'));
  } finally {
    globalThis.fetch = oldFetch;
    process.stderr.write = oldWrite;
  }
});
