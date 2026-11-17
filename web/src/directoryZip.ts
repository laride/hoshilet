export interface UploadEntry {
  path: string;
  file: File;
}

// A minimal classic ZIP writer: UTF-8 names, Store only, no ZIP64.
const crcTable = Uint32Array.from({ length: 256 }, (_, n) => {
  for (let i = 0; i < 8; i++) n = n & 1 ? 0xedb88320 ^ (n >>> 1) : n >>> 1;
  return n >>> 0;
});

export async function storeDirectory(entries: UploadEntry[]): Promise<File> {
  if (!entries.length) throw new Error('目录中没有文件');
  if (entries.length > 10000) throw new Error('目录中的文件数超过 10,000');
  const parts: BlobPart[] = [];
  const central: BlobPart[] = [];
  let offset = 0;
  let centralSize = 0;
  const seen = new Set<string>();
  for (const { path, file } of entries) {
    const name = new TextEncoder().encode(path);
    if (
      seen.has(path) ||
      name.length > 1024 ||
      /[\\:\x00-\x1f\x7f]/.test(path) ||
      path.split('/').some((p) => !p || p === '.' || p === '..' || new TextEncoder().encode(p).length > 255) ||
      path.split('/').length > 32
    )
      throw new Error(`目录中包含不支持的路径：${path}`);
    seen.add(path);
    if (file.size >= 0xffffffff || offset + file.size + name.length + 30 >= 0xffffffff)
      throw new Error('目录过大，无法打包为普通 ZIP');
    let crc = 0xffffffff;
    const reader = file.stream().getReader();
    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        for (const byte of value) crc = crcTable[(crc ^ byte) & 255] ^ (crc >>> 8);
      }
    } finally {
      reader.releaseLock();
    }
    crc = (crc ^ 0xffffffff) >>> 0;
    const header = new Uint8Array(30 + name.length);
    const h = new DataView(header.buffer);
    h.setUint32(0, 0x04034b50, true);
    h.setUint16(4, 20, true);
    h.setUint16(6, 0x800, true);
    h.setUint16(12, 33, true); // 1980-01-01
    h.setUint32(14, crc, true);
    h.setUint32(18, file.size, true);
    h.setUint32(22, file.size, true);
    h.setUint16(26, name.length, true);
    header.set(name, 30);
    const directory = new Uint8Array(46 + name.length);
    const d = new DataView(directory.buffer);
    d.setUint32(0, 0x02014b50, true);
    d.setUint16(4, 20, true);
    directory.set(header.subarray(4, 30), 6);
    d.setUint32(42, offset, true);
    directory.set(name, 46);
    parts.push(header, file);
    central.push(directory);
    offset += header.length + file.size;
    centralSize += directory.length;
  }
  if (offset + centralSize >= 0xffffffff) throw new Error('目录过大，无法打包为普通 ZIP');
  const end = new Uint8Array(22);
  const e = new DataView(end.buffer);
  e.setUint32(0, 0x06054b50, true);
  e.setUint16(8, entries.length, true);
  e.setUint16(10, entries.length, true);
  e.setUint32(12, centralSize, true);
  e.setUint32(16, offset, true);
  return new File([...parts, ...central, end], 'directory.zip', { type: 'application/zip' });
}

export async function readDirectory(root: FileSystemDirectoryEntry): Promise<UploadEntry[]> {
  const files: UploadEntry[] = [];
  async function walk(directory: FileSystemDirectoryEntry, prefix: string) {
    const reader = directory.createReader();
    while (true) {
      const batch = await new Promise<FileSystemEntry[]>((resolve, reject) => reader.readEntries(resolve, reject));
      if (!batch.length) break;
      for (const entry of batch) {
        const path = prefix + entry.name;
        if (entry.isDirectory) await walk(entry as FileSystemDirectoryEntry, path + '/');
        else if (entry.isFile) {
          const file = await new Promise<File>((resolve, reject) =>
            (entry as FileSystemFileEntry).file(resolve, reject),
          );
          files.push({ path, file });
          if (files.length > 10000) throw new Error('目录中的文件数超过 10,000');
        }
      }
    }
  }
  await walk(root, '');
  return files;
}
