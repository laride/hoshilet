# Deployment & Operations

## Bare Metal (systemd)

1. Build the backend and install it:
   ```sh
   make build
   install -Dm755 bin/hoshilet-server /usr/local/bin/hoshilet-server
   ```

2. Create a dedicated system user `hoshilet` and write the config:
   ```sh
   install -Dm600 deploy/config.example.json /etc/hoshilet/config.json
   ```
   Edit the config with your domain, GitLab OAuth credentials, and admin IDs.

3. Install and start the systemd service:
   ```sh
   install -Dm644 deploy/systemd/hoshilet.service /etc/systemd/system/hoshilet.service
   systemctl daemon-reload
   systemctl enable --now hoshilet
   ```

4. Build and install the management UI:
   ```sh
   make web
   cp -r web/dist/* /srv/hoshilet/admin/
   ```
   Keep this directory separate from user sites. The static file server should have read-only access.

5. Set up the reverse proxy using `deploy/Caddyfile` or `deploy/nginx.conf` (replace the example domains).

### Reverse Proxy Rules

- `/api/*`, `/auth/*`, `/healthz` → forward to the Go backend.
- Everything else → serve from the admin static directory.
- Both share the same origin (no CORS).

The URL prefix `/~namespace` is handled purely by the static server's rewrite rule — on disk, paths don't contain `~`. Make sure directory redirects preserve the `~` prefix, disable directory listings, and don't enable SPA fallback. The example configs set `must-revalidate` caching so rollbacks take effect immediately.

### File Permissions

The server automatically sets: `public/`, `releases/` → 0755; static files → 0644; `database/`, `staging/` → 0700. The static file server needs read + directory traverse on `public` and `releases` — don't give it access to the database or config.

## User-Level systemd Service

If you don't want to create a system user, use the user-level service:

```sh
make build
install -Dm755 bin/hoshilet-server "$HOME/.local/bin/hoshilet-server"
install -Dm644 deploy/systemd/hoshilet-user.service \
  "${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/hoshilet-user.service"
install -Dm600 deploy/config.example.json \
  "${XDG_CONFIG_HOME:-$HOME/.config}/hoshilet/config.json"
```

Edit the config: set your domain, OAuth credentials, and admin IDs. Change `data_dir` from `/var/lib/hoshilet` to `"."` (or any writable absolute path). Don't use `~`, `$HOME`, or systemd's `%S` in the JSON — the program won't expand them.

The service uses `StateDirectory=hoshilet`, so the state directory is created automatically and used as the working directory. With `data_dir: "."`, the database ends up at `~/.local/state/hoshilet/database/hoshilet.db`.

```sh
systemctl --user daemon-reload
systemctl --user enable --now hoshilet-user.service
systemctl --user status hoshilet-user.service
journalctl --user -u hoshilet-user.service -f
```

To keep the service running after logout, ask a system admin to run `loginctl enable-linger USERNAME`.

> **Don't run both** a system service and a user service against the same data directory.

## Podman Quadlet (Container)

```sh
make image    # builds a static Go binary in a minimal container
```

The image contains only the backend — no management UI and no Node.js dependency. Build the UI separately with `make web` and mount it into the reverse proxy container.

Install `deploy/quadlet/hoshilet.container` to `/etc/containers/systemd/`. The example assumes a rootful system unit:

- Data directory `/var/lib/hoshilet` owned by UID/GID 10001.
- `config.json` owned by UID 10001, permissions 0600.
- Container listens on `0.0.0.0:8080`; the host publishes only `127.0.0.1:8080`.
- Data directory mounted at the same path inside the container (symlinks use relative targets).
- On SELinux hosts, use the `:z` shared label and configure read-only access policies for the static file server.
- If the static server is also containerized, mount `public` and `releases` read-only, preserving their relative layout. It doesn't need `database` or `staging`.

Rootless deployments need adjustments to host directories, UID mapping, and Quadlet install targets.

## Backup & Restore

### Backup

Backup uses the same exclusive lock as the running server, so **stop the service first**:

```sh
sudo systemctl stop hoshilet
sudo -u hoshilet hoshilet-server \
  -config /etc/hoshilet/config.json \
  -backup /path/writable-by-hoshilet/hoshilet.tar.gz
sudo systemctl start hoshilet
```

The backup includes a consistent SQLite snapshot and all release files. It does **not** include staging data or OAuth config secrets. Back up your config file separately.

> The backup file (permissions 0600) contains identity data, key hashes, and short-lived OIDC credentials. Keep it private.

### Upgrade

Back up first, then replace the binary or container image and restart. The current version uses schema 4. There's no migration from older prototype schemas — start fresh with a new data directory if needed.

### Restore

Restore only accepts trusted backups. The target `data_dir` **must not already exist** (to prevent overwriting a live site):

```sh
hoshilet-server -config restore-config.json -restore /path/hoshilet.tar.gz
```

The restore process validates tar paths, rejects symlinks and duplicates, verifies the database and public links in a temp directory, then atomically moves everything into place. If validation fails, the temp directory is cleaned up and nothing is left behind.

After restoring, verify ownership, paths, and site content before starting the service.

## Monitoring

| What | How |
|------|-----|
| Health check | `GET /healthz` |
| Metrics | `GET /api/v1/admin/metrics` (admin session) — disk, upload slots, task counts, deployment bytes |
| Admin UI | Pending reclaim bytes, pending recovery count, last 500 audit events |
| Logs | Structured JSON — upload validation failures, server errors, cleanup errors |

### Upload Quotas

Defaults: 4 global slots, 1 per user, 120s timeout. All keys from the same user share the per-user quota; OIDC quotas are per issuer + GitLab project ID. Adjust via `max_uploads`, `max_uploads_per_principal`, `upload_timeout_seconds`. The per-user limit must be less than the global limit. New uploads are rejected when disk space is low.

### Consistency Note

Atomic symlink swaps make individual page transitions instant, but multiple HTTP requests for different assets are **not** guaranteed to see the same version. Use content-hashed filenames to avoid stale references. Old version files are kept for a grace period (default 10 minutes) to help in-flight requests, but this is best-effort.

## Admin: Space Usage

Platform admins can view all namespaces and their disk usage in the "Space Usage" panel. The numbers include all retained deployments and files pending cleanup (including deleted projects not yet purged), but exclude already-cleaned versions, in-progress uploads, orphan files, and database/filesystem overhead. Values are based on recorded uncompressed file sizes — the current public symlink is not double-counted.
