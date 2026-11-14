const TITLE = 'Messages from the Current Server Instance';
export const HEADER = 'X-Hoshilet-Server-Message';

export function decodeMessage(value: unknown): string {
  if (
    typeof value !== 'string' ||
    !value ||
    value.length > 684 ||
    !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(value)
  ) {
    throw Error('Invalid server message');
  }
  const raw = Buffer.from(value, 'base64');
  if (raw.length > 512 || raw.toString('base64') !== value) throw Error('Invalid server message');
  const text = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(raw);
  // eslint-disable-next-line no-control-regex -- Control characters are intentionally rejected.
  if (/[\u0000-\u0009\u000b-\u001f\u007f-\u009f]/u.test(text)) throw Error('Invalid server message');
  return text;
}

export function terminalWidth(): number {
  const n = Number(process.stderr.columns || process.env.COLUMNS || 80);
  return Math.min(Number.isSafeInteger(n) && n > 0 ? n : 80, 100);
}

export function formatMessage(text: string, width: number): string {
  width = Math.max(1, Math.min(Math.trunc(width), 100));
  const title = width >= 4 ? ' ' + TITLE.slice(0, Math.max(0, width - 4)) + ' ' : '';
  const remaining = width - title.length;
  return (
    '='.repeat(Math.floor(remaining / 2)) +
    title +
    '='.repeat(remaining - Math.floor(remaining / 2)) +
    '\n' +
    text +
    (text.endsWith('\n') ? '' : '\n') +
    '='.repeat(width) +
    '\n'
  );
}

export function serverMessages(): (value: string | null) => void {
  const seen = new Set<string>();
  let warned = false;
  return (value) => {
    if (!value) return;
    let text: string;
    try {
      text = decodeMessage(value);
    } catch {
      if (!warned) {
        process.stderr.write('Ignored invalid server message header.\n');
        warned = true;
      }
      return;
    }
    if (text && !seen.has(text)) {
      seen.add(text);
      process.stderr.write(formatMessage(text, terminalWidth()));
    }
  };
}
