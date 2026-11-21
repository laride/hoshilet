"""Dependency-free client; protocol parity with the npm hoshilet package."""

import argparse
import http.client
import json
import os
from pathlib import Path
import ssl
import stat
import sys
import tempfile
import urllib.parse
import uuid
import zipfile
from .messages import HEADER, ServerMessages


class Client:
    def __init__(self, base, token=None, extra_ca_certs=()):
        parsed = urllib.parse.urlsplit(base)
        if (
            parsed.scheme not in ('https', 'http')
            or not parsed.hostname
            or parsed.username
            or parsed.query
            or parsed.fragment
            or parsed.path not in ('', '/')
        ):
            raise ValueError('--server must be an HTTP(S) origin')
        if parsed.scheme == 'http' and parsed.hostname not in ('localhost', '127.0.0.1', '::1'):
            raise ValueError('HTTPS is required except on loopback')
        self.base, self.token = parsed, token
        self.messages = ServerMessages()
        self.ssl_context = None
        if extra_ca_certs:
            self.ssl_context = ssl.create_default_context()
            for cafile in extra_ca_certs:
                try:
                    self.ssl_context.load_verify_locations(cafile=cafile)
                except (OSError, ValueError) as exc:
                    raise ValueError(f'Cannot load --extra-ca-certs {cafile}: {exc}') from exc

    def request(self, method, route, data=None, archive=None, headers=None):
        if self.base.scheme == 'https':
            conn = http.client.HTTPSConnection(
                self.base.hostname, self.base.port, timeout=720, context=self.ssl_context
            )
        else:
            conn = http.client.HTTPConnection(self.base.hostname, self.base.port, timeout=720)
        h = dict(headers or {})
        if self.token:
            h['Authorization'] = 'Bearer ' + self.token
        stream = None
        try:
            payload = None
            if archive is not None:
                stream = open(archive, 'rb')
                payload = stream
                h.update(
                    {
                        'Content-Type': 'application/zip',
                        'Content-Length': str(os.fstat(stream.fileno()).st_size),
                    }
                )
            elif data is not None:
                payload = json.dumps(data).encode()
                h['Content-Type'] = 'application/json'
            conn.request(method, '/api/v1/' + route, body=payload, headers=h)
            response = conn.getresponse()
            self.messages.show(response.getheader(HEADER))
            raw = response.read(4 * 1024 * 1024 + 1)
            if len(raw) > 4 * 1024 * 1024:
                raise RuntimeError('Response too large')
            result = json.loads(raw)
            if not 200 <= response.status < 300:
                error = result.get('error', {})
                raise RuntimeError(
                    f'{response.status} {error.get("code", "request_failed")}: {error.get("message", "Request failed")}'
                )
            return result
        finally:
            if stream:
                stream.close()
            conn.close()


def pack(source, target):
    source = Path(source)
    if source.is_symlink():
        raise ValueError('Symbolic links are not accepted')
    if not source.is_dir():
        if not source.is_file():
            raise ValueError('Source must be a directory or ZIP file')
        return source
    with zipfile.ZipFile(target, 'w', zipfile.ZIP_DEFLATED, allowZip64=False) as z:
        for root, dirs, files in os.walk(source, followlinks=False):
            dirs.sort()
            files.sort()
            for name in dirs + files:
                p = Path(root) / name
                mode = p.lstat().st_mode
                if not stat.S_ISREG(mode) and not stat.S_ISDIR(mode):
                    raise ValueError(f'Non-regular file: {p.relative_to(source)}')
                z.write(p, p.relative_to(source).as_posix())
    return Path(target)


def parser():
    p = argparse.ArgumentParser(description='Deploy static sites to hoshilet')
    p.add_argument(
        '--server',
        default=os.getenv('HOSHILET_SERVER'),
        help='Management origin (or HOSHILET_SERVER)',
    )
    p.add_argument('--json', action='store_true', help='Machine-readable result')
    ca_help = 'Append trusted CA certificates from a PEM file (repeatable)'
    p.add_argument('--extra-ca-certs', action='append', default=[], metavar='FILE', help=ca_help)
    sub = p.add_subparsers(dest='command', required=True)

    def common(command, ca_dest='command_extra_ca_certs'):
        command.add_argument('--server', default=argparse.SUPPRESS)
        command.add_argument('--json', action='store_true', default=argparse.SUPPRESS)
        # Subparsers use separate namespaces; keep each level's CA files when merging.
        command.add_argument(
            '--extra-ca-certs',
            dest=ca_dest,
            action='append',
            default=[],
            metavar='FILE',
            help=ca_help,
        )

    d = sub.add_parser('deploy')
    common(d)
    d.add_argument('source')
    d.add_argument('--namespace')
    d.add_argument('--project')
    d.add_argument('--project-id')
    d.add_argument('--no-activate', action='store_true')
    d.add_argument(
        '--oidc', action='store_true', help='Use HOSHILET_ID_TOKEN from GitLab id_tokens'
    )
    d.add_argument('--idempotency-key', default=None)
    d.add_argument('--revision', type=int)
    deployment_parser = sub.add_parser('deployments')
    common(deployment_parser)
    listings = deployment_parser.add_subparsers(dest='subcommand', required=True)
    listing = listings.add_parser('list')
    common(listing, 'listing_extra_ca_certs')
    listing.add_argument('--project-id', required=True)
    a = sub.add_parser('activate')
    common(a)
    a.add_argument('deployment_id')
    a.add_argument('--project-id', required=True)
    a.add_argument('--revision', type=int)
    return p


def execute(args):
    if not args.server:
        raise ValueError('Set HOSHILET_SERVER or --server')
    ca_files = (
        args.extra_ca_certs
        + args.command_extra_ca_certs
        + getattr(args, 'listing_extra_ca_certs', [])
    )
    client = Client(args.server, os.getenv('HOSHILET_API_KEY'), ca_files)
    if args.command == 'deploy':
        idem = args.idempotency_key or str(uuid.uuid4())
        if args.oidc:
            token = os.getenv('HOSHILET_ID_TOKEN')
            if not token or not args.project_id:
                raise ValueError('OIDC requires HOSHILET_ID_TOKEN and --project-id')
            exchanged = client.request(
                'POST',
                'oidc/exchange',
                {'token': token, 'project_id': args.project_id},
                headers={'Idempotency-Key': idem + '-oidc'},
            )
            client.token = exchanged['access_token']
        if not client.token:
            raise ValueError('Set HOSHILET_API_KEY or use --oidc')
        query = {'activate': 'false' if args.no_activate else 'true'}
        for field in ('namespace', 'project', 'project_id'):
            value = getattr(args, field)
            if value is not None:
                query[field] = value
        headers = {'Idempotency-Key': idem}
        if args.revision is not None:
            headers['If-Match'] = str(args.revision)
        with tempfile.TemporaryDirectory(prefix='hoshilet-') as tmp:
            path = pack(args.source, Path(tmp) / 'site.zip')
            return client.request(
                'POST', 'deploy?' + urllib.parse.urlencode(query), archive=path, headers=headers
            )
    if not client.token:
        raise ValueError('Set HOSHILET_API_KEY')
    pid = urllib.parse.quote(args.project_id, safe='')
    if args.command == 'deployments':
        return client.request('GET', f'projects/{pid}/deployments')
    revision = args.revision
    if revision is None:
        revision = client.request('GET', f'projects/{pid}')['revision']
    return client.request(
        'POST',
        f'projects/{pid}/activate',
        {'deployment_id': args.deployment_id, 'revision': revision},
    )


def main():
    args = parser().parse_args()
    try:
        result = execute(args)
        print(json.dumps(result, ensure_ascii=False, indent=None if args.json else 2))
        if isinstance(result, dict) and result.get('status') in (
            'failed',
            'receiving',
            'validating',
            'publishing',
        ):
            return 1
        return 0
    except (
        ValueError,
        RuntimeError,
        OSError,
        http.client.HTTPException,
        zipfile.BadZipFile,
    ) as exc:
        print(
            json.dumps({'error': {'message': str(exc)}}) if args.json else str(exc), file=sys.stderr
        )
        return 1


if __name__ == '__main__':
    sys.exit(main())
