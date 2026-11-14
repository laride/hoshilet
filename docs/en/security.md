# Security

## Authentication & Authorization

### Users

Users are identified by their GitLab numeric user ID — username changes don't affect anything. GitLab is only used for identity; the server doesn't query groups or store GitLab access tokens.

Access control is managed locally: admins maintain a user allowlist in the database. Unapproved users can complete OAuth login but can only see their own status or log out. All API calls and deploy commits check the local user state — there's no cache or GitLab callback involved.

Initial admins (configured via `admin_ids`) cannot be disabled from the admin panel. Revoking a user immediately invalidates their session and all API keys, but does not automatically take down their team's CI trusts or public sites.

### Browser Sessions

- All write operations validate the `Origin` header.
- Cookies use `__Host-` prefix, `Secure`, `HttpOnly`, `SameSite=Lax`.
- Bearer tokens use a separate auth path — query-string credentials are never accepted.

### API Keys

- The plaintext key is shown exactly once at creation. The server stores only a SHA-256 hash.
- Keys have an expiration (default 90 days, max 365 days).
- A key never grants more permissions than the user's role.

### OIDC (CI Tokens)

- Only HTTPS JWKS endpoints from configured issuers are accepted. The server ignores `jku` or similar hints from the JWT itself.
- Algorithm is restricted to RS256. Issuer, audience, and expiration are all validated.
- JWKS is fetched on every token exchange, so key rotations take effect immediately. If the JWKS endpoint is down, tokens are rejected (no fallback).
- MR pipeline publishing is per-trust-rule (`allow_mr_pipelines`, default off). Source and execution repos must match — cross-fork MRs are always rejected.
- Each trust rule can require `ref_protected`. When enabled, only `true` (boolean) or `"true"` (string) are accepted; missing or other values are rejected.
- Token exchange produces a short-lived credential (up to 5 minutes). The temporary plaintext needed for idempotent exchange is stored in a restricted database table and cleaned up on expiry.

## ZIP Upload Validation

Uploaded ZIPs are validated and extracted in a **sandboxed subprocess** with strict resource limits.

### Rejected Content

The server rejects ZIPs containing:

- Absolute paths, `..` traversal, or paths that resolve outside the archive root
- Backslashes, Windows drive letters
- Duplicate or conflicting paths (e.g., same name as both file and directory)
- Invalid UTF-8, control characters
- Paths longer than 1024 bytes or deeper than 32 levels
- Symlinks, device files, or any non-regular entries

### Structural Checks

Before extraction, the server verifies the central directory, EOCD, local headers, filenames, compression methods, flags, offsets, and entry overlap ranges. Only single-volume ZIPs starting at offset 0 with no inter-entry gaps are accepted. No ZIP64.

Each file is read to EOF and verified against its CRC and declared length. Size limits are enforced on both metadata declarations and actual extracted output independently — the server doesn't trust ZIP-declared sizes alone.

An archive with only directories (no regular files) is rejected after validation. Zero-byte regular files are allowed.

### Resource Limits

| Limit | Default |
|-------|---------|
| Request body | 128 MiB |
| Uncompressed total | 100 MiB |
| Max entries | 10,000 |
| Max path length | 1,024 bytes |
| Max directory depth | 32 levels |
| Upload timeout | 120 seconds |
| Subprocess address space | 2 GiB |

The subprocess also has CPU, file descriptor, and single-file size limits. Go memory limit is set to half the address space quota.

Upload concurrency: 4 global / 1 per user (all keys and sessions from the same user share the quota). OIDC quota is per issuer + GitLab project ID — switching tokens or trust rules doesn't bypass it.

> When increasing upload size limits, also adjust request body limits, disk quotas, systemd unit memory, and worker limits accordingly.

### Failure Isolation

Malformed uploads only affect the private staging task — they never touch published content. Timeouts, cancellations, and failures clean up the staging directory. Orphan release files are reclaimed after the grace period.

Auth and quotas are checked at both the receive and commit stages.

## Static Content

Users can upload browser-executable JavaScript, but the server **never executes uploaded content**. There are no dynamic handlers — only static file serving.

Platform-generated symlinks only point into managed release directories. Archive files cannot generate symlinks.

### Shared Origin Warning

All content sites share a single origin. Don't rely on path-based isolation for security — avoid using management sessions, sensitive cross-site cookies, or tenant boundaries based on URL paths within the content origin.

The server makes no guarantees about the trustworthiness of user-uploaded content. Keep the static file server up to date.

### Management UI Isolation

The management UI is deployed to a separate static directory behind the reverse proxy. It's not bundled in the Go binary or backend container. The example reverse proxy configs set CSP, `X-Content-Type-Options: nosniff`, `Referrer-Policy`, and `must-revalidate` caching on admin responses.

## Backups

Backup files (permissions 0600) contain authorization data, key hashes, and short-lived OIDC credentials. Only restore from trusted sources.

The backup restore process uses a dedicated restricted tar reader — it does not reuse the ZIP upload validator.
