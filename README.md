# Hoshilet

[中文](README.zh.md)

A self-hosted static Pages platform. Deploy static sites under `/~team/project/` paths — upload a ZIP, get a live site.

- GitLab OAuth login with namespace admin/member collaboration
- Deploy and roll back via ZIP upload
- API key and multi-instance GitLab OIDC publishing
- systemd bare metal, user-level service, and Podman Quadlet deployment

The URL `/~team/docs/` maps to `public/team/docs/` on disk (no `~` in local paths). Adding, removing, and switching site versions never changes the reverse proxy config. Public sites stay online even if the management server is stopped.

## Install the CLI

The npm and PyPI packages are both called `hoshilet` and provide the same `hoshilet` command. Pick whichever you prefer:

```sh
# Python
uv build --project cli/python --out-dir dist/python
uv tool install dist/python/hoshilet-0.1.0-py3-none-any.whl

# Node.js
pnpm --dir cli/npm pack --pack-destination ../../dist/npm
npm install -g ./dist/npm/hoshilet-0.1.0.tgz
```

## Quick Start

```sh
export HOSHILET_SERVER=https://hoshilet-admin.example.com
export HOSHILET_API_KEY=your-key

hoshilet deploy ./dist --namespace team --project docs
```

## Documentation

- [CLI Usage](docs/en/cli-usage.md) — commands, options, OIDC, limits
- [Development & Building](docs/en/build.md) — prerequisites, building, local dev, GitLab OAuth
- [Deployment & Operations](docs/en/operations.md) — systemd, Podman, backup/restore, monitoring
- [Architecture](docs/en/architecture.md) — internals, deployment pipeline, crash recovery
- [Security](docs/en/security.md) — auth model, ZIP validation, isolation
- [OpenAPI](docs/openapi.json)
