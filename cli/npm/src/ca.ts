import { readFile } from 'node:fs/promises';
import { X509Certificate } from 'node:crypto';
import tls from 'node:tls';

export async function appendCACertificates(files: string[] = []): Promise<void> {
  if (files.length === 0) return;
  if (typeof tls.getCACertificates !== 'function' || typeof tls.setDefaultCACertificates !== 'function') {
    throw Error(
      '--extra-ca-certs requires Node.js 22.19+ or 24.5+ with TLS CA APIs; ' +
        'on older versions, set NODE_EXTRA_CA_CERTS=/path/to/ca-bundle.pem before starting hoshilet',
    );
  }
  const additional: string[] = [];
  for (const file of files) {
    try {
      const pem = await readFile(file, 'utf8');
      const certificates = pem.match(/-----BEGIN CERTIFICATE-----[\s\S]*?-----END CERTIFICATE-----/g);
      if (!certificates?.length) throw Error('No PEM certificates found');
      for (const certificate of certificates) {
        additional.push(new X509Certificate(certificate).toString());
      }
    } catch (error) {
      throw Error(`Cannot load --extra-ca-certs ${file}: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  tls.setDefaultCACertificates([...tls.getCACertificates('default'), ...additional]);
}
