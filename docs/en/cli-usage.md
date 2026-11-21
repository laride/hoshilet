# CLI Usage

## Quick Start

Set two environment variables and you're ready to deploy:

```sh
export HOSHILET_SERVER=https://hoshilet-admin.example.com
export HOSHILET_API_KEY=your-key
```

Deploy a directory as a static site:

```sh
hoshilet deploy ./dist --namespace team --project docs
```

That's it — the site goes live immediately at `/~team/docs/`.

## Deploying

The basic deploy command zips your directory and uploads it:

```sh
# Auto-creates the project if your key belongs to a namespace admin.
hoshilet deploy ./dist --namespace team --project docs

# Use a project ID directly (project keys can't auto-create projects).
hoshilet deploy ./dist --project-id PROJECT_ID
```

By default, a deployment is activated (goes live) as soon as the upload succeeds. To upload without going live, add `--no-activate`:

```sh
hoshilet deploy ./dist --project-id PROJECT_ID --no-activate
```

### Concurrency Safety

- `--revision` prevents accidental overwrites — the server rejects the deploy if someone else published in the meantime.
- `--idempotency-key` lets you safely retry a failed upload without creating a duplicate deployment.

> **Tip:** If a deploy seems stuck or the network dropped, check existing deployments first (`hoshilet deployments list`) rather than immediately retrying with a new idempotency key.

## Managing Deployments

```sh
# List all deployments of a project.
hoshilet deployments list --project-id PROJECT_ID

# Activate (roll forward/back to) a specific deployment.
hoshilet activate DEPLOYMENT_ID --project-id PROJECT_ID
```

## JSON Output

Add `--json` to any command for machine-readable JSON output on stdout:

```sh
hoshilet --json deploy ./dist --namespace team --project docs
```

## Private Certificate Authorities

Use `--extra-ca-certs FILE` to append trusted CA certificates while retaining the runtime's default trust store:

```sh
hoshilet --extra-ca-certs company-ca.pem deploy ./dist --project-id PROJECT_ID

# Repeat the option for multiple files. Each file can contain multiple PEM certificates.
hoshilet deployments list --project-id PROJECT_ID \
  --extra-ca-certs company-ca.pem --extra-ca-certs team-ca.pem
```

The option works before or after subcommands and applies to every HTTPS request in that invocation, including OIDC token exchange. Certificate and hostname verification remain enabled. Missing or invalid certificate files cause the command to fail.

The Python CLI needs no additional dependencies or version changes (Python 3.10–3.15).
The npm CLI uses native TLS CA APIs available in Node.js 22.19 and Node.js 24.5 or later releases. Without this option, the existing Node.js 22 minimum still applies. On older Node.js versions, use the runtime's startup environment variable instead:

```sh
NODE_EXTRA_CA_CERTS=/path/to/company-ca-bundle.pem \
  hoshilet deploy ./dist --project-id PROJECT_ID
```

`NODE_EXTRA_CA_CERTS` also appends certificates, but is only read when Node.js starts. If it is already set, `--extra-ca-certs` retains those certificates as well.

## GitLab CI (OIDC)

Instead of static API keys, you can authenticate with a GitLab CI OIDC token:

```sh
export HOSHILET_ID_TOKEN=$CI_JOB_JWT_V2   # or use GitLab's id_tokens
hoshilet deploy ./dist --oidc --project-id PROJECT_ID
```

The `audience` must match the management origin. A namespace admin needs to configure OIDC trust rules in the web UI beforehand, binding the GitLab issuer, the numeric GitLab project ID, and separate branch and tag allowlists.

Branch and tag allowlist matching rules:

- The two allowlists are independent. An omitted or empty list denies all refs of that type.
- `*` matches zero or more characters, including `/`, e.g. branches `release/*` and tags `v*`. A standalone `*` allows every ref of that type.
- `all` is a literal branch name — it only matches a branch literally named `all`.
- Only `*` is a wildcard; other characters are literal. Matching is case-sensitive and covers the entire name.

Each trust rule can independently set:

- `require_ref_protected` (default: off) — only accepts tokens where the `ref_protected` claim is `true`.
- `allow_mr_pipelines` (default: off) — accepts merge-request pipelines from the same repo, matched by source branch. Cross-fork MRs are always rejected.

See [deploy/gitlab-ci.example.yml](../../deploy/gitlab-ci.example.yml) for a working CI config.

> OIDC deploys cannot auto-create projects.

## Limits

- **Namespace mode is sticky:** a namespace is either a single root site or a collection of sub-projects. Delete all projects to switch modes.
- **Versions are immutable.** Going live is an atomic symlink swap; the version directory itself never changes.
- **Empty ZIPs are rejected** (directories only, no regular files → `422 empty_deployment`). Zero-byte files are fine.
- **No symlinks in uploads.** Paths cannot escape the archive root. ZIPs are validated and extracted in a sandboxed subprocess.
- **Classic ZIP only:** Store or Deflate. No ZIP64, encryption, self-extracting, or multi-volume archives. Files inside the archive may themselves be compressed files — they won't be recursively unpacked.
- **Retention:** 10 versions per project by default, 100 MiB max per version (uncompressed). Old files are cleaned up after a 10-minute grace period.
- **No cross-request snapshot guarantee.** Use content-hashed filenames for assets. Set the correct `/~namespace/project/` base path at build time — the platform won't rewrite URLs.
- **Upload concurrency:** 4 global / 1 per user by default, 120s timeout. Multiple keys from the same user share the quota; OIDC quotas are per issuer + GitLab project ID. Tunable via `max_uploads`, `max_uploads_per_principal`, `upload_timeout_seconds`.
- **Shared content origin.** The management site must use a separate origin. The first version does not support private sites, custom domains, SSR, online builds, SPA fallback, or replicas.

## Server Messages

Instance administrators can set a broadcast message in the server config:

```json
{"server_message": "Welcome.\nMaintenance window: Sundays 02:00–03:00 UTC."}
```

The CLI displays these messages on stderr. Duplicate messages within a single command are shown only once. Malformed message headers produce a fixed warning and are never echoed raw.
