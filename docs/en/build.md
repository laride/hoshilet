# Development & Building

## Prerequisites

| Tool | Version | Needed for |
|------|---------|------------|
| Go | 1.27.1 | Backend server |
| Node.js | 22+ | npm CLI, management web UI |
| pnpm | latest | npm CLI, web UI dependencies |
| uv | latest | Python CLI |

The backend only needs Go. The React management UI is built separately and served by Caddy or Nginx — it's not embedded in the Go binary.

## Building

```sh
pnpm --dir web install --frozen-lockfile
pnpm --dir cli/npm install --frozen-lockfile
uv sync --project cli/python

make build       # backend → bin/hoshilet-server
make web         # management UI → web/dist/
make test        # Go tests + both CLIs
make cli-test    # end-to-end: starts a real HTTP server, tests both CLIs
```

`web/dist/` is a build artifact and not committed to Git.

## Running Locally

```sh
cp deploy/config.example.json config.json
# Fill in your domain, GitLab OAuth credentials, and admin IDs.
# Don't commit this file — it contains secrets.

./bin/hoshilet-server -config config.json -check   # validate config
./bin/hoshilet-server -config config.json           # start the server
```

The server listens on `127.0.0.1:8080` by default. It only serves the API — no web pages. To use the management UI:

1. Build the UI: `make web`
2. Copy `web/dist/` contents to `/srv/hoshilet/admin/` (or wherever your reverse proxy serves static files).
3. Configure your reverse proxy to forward `/api/*`, `/auth/*`, and `/healthz` to the backend, and serve everything else from the admin static directory.
4. Both must be on the same origin — no CORS needed.

> **Note:** Web login requires HTTPS through the configured management domain (cookies are `Secure`). You can't complete OAuth login on the plain HTTP dev port.

Run `./bin/hoshilet-server -version` to check the version and Git build info (no config file needed).

The server binary is `hoshilet-server`; the CLI tool is `hoshilet` — they can coexist.

## npm CLI Development

Source is in `cli/npm/src/`, built with tsdown under TypeScript strict mode:

```sh
pnpm --dir cli/npm install --frozen-lockfile
pnpm --dir cli/npm typecheck
pnpm --dir cli/npm build     # → cli/npm/dist/hoshilet.mjs
pnpm --dir cli/npm test      # typecheck + build + tests
```

The published npm package only includes `dist/`. End users don't need TypeScript — just Node 22+. The build toolchain requires Node `^22.18.0 || ^24.11.0 || >=26.0.0`.

`pnpm pack` runs `prepack` which builds automatically. `make cli-test` also builds the npm CLI first, then runs real HTTP integration tests.

## Test Cache

Go test cache defaults to `/tmp/hoshilet-go-cache`. `make cli-test` spins up a real HTTP server on a random loopback port and exercises both CLIs end-to-end: pack, upload, query, activate, and staging.

## GitLab OAuth Setup

Create an OAuth Application on your GitLab instance:

- **Redirect URI:** `https://your-admin-domain/auth/callback`
- **Scopes:** `read_user`

The server uses PKCE, only reads basic identity info, doesn't query groups, and doesn't store GitLab access tokens.

### User Management

Set initial platform admins via `admin_ids` in the config (GitLab numeric user IDs). Other users land on a pending page after login — admins approve them in the "User Allowlist" panel (or pre-add their numeric IDs).

Users have three states: **pending**, **approved**, **disabled**.

- Only approved users can create namespaces and deploy.
- Revoking a user immediately invalidates their session and all API keys. Re-approving doesn't revive old keys.
- Public sites and CI OIDC trusts are not affected by revoking individual users.

### CI Issuers

The login GitLab is a single instance. **CI issuers** (configured in the admin panel) can come from multiple GitLab instances — they're independent of the login instance. The JWKS URL is typically `https://gitlab.example.com/oauth/discovery/keys`.

### Teams

Namespace admins can add logged-in users as members (default role: member). Members can deploy and roll back existing projects but cannot create projects, manage members, delete versions, or configure OIDC. Every namespace must keep at least one admin.
