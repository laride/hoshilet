# Architecture

## Overview

hoshilet is a single-process, single-machine application with three main components:

| Component | Location | Description |
|-----------|----------|-------------|
| Backend server | `cmd/hoshilet-server`, `internal/platform` | Go HTTP server: auth, API, uploads, deployments |
| ZIP validator | `internal/archive` | Sandboxed subprocess for safe extraction |
| Management UI | `web/` | React SPA, built separately, served by the reverse proxy |
| CLI tools | `cli/npm/`, `cli/python/` | Two interchangeable CLI clients using the same HTTP API |

The Go binary and web UI are built and deployed independently. The backend never serves frontend assets — the reverse proxy handles that.

## Data Layer

The backend uses SQLite in WAL mode. Every new connection sets `foreign_keys`, `busy_timeout`, and `FULL synchronous`. A process-level `flock` prevents multiple instances from accessing the same data directory.

Control-plane mutations (project settings, user management, etc.) are serialized with a mutex. Uploads and extraction run concurrently without holding this lock — they only acquire it briefly at commit time, when the server re-checks auth and compares the project revision before finalizing.

There is a single database connection. All queries inside a transaction must go through `tx.Query`/`tx.Exec`, not through the top-level helpers.

## Deployment Pipeline

When a user uploads a ZIP:

1. **Stage:** Files are extracted to a private staging directory, validated, and fsynced.
2. **Record:** A transaction records the Deployment, Upload, and a "publication intent."
3. **Link:** A relative symlink is created and atomically renamed into `public/namespace[/project]`, then the parent directory is fsynced.
4. **Commit:** A second transaction updates the current version, increments the revision, records an ActivationEvent, and removes the intent.
5. **Cleanup:** The retention policy runs — excess old versions are marked for deletion, and their files are removed after a grace period.

### Crash Recovery

On startup (and every minute), the server looks for incomplete publication intents and replays them. It won't re-generate activation events that were already committed. Recovery also reconciles public symlinks, finishes pending deletes, and cleans up staging leftovers.

While an unfinished intent exists for a project, new writes to that project are blocked — this prevents a stale intent from overwriting fresh data.

### Version Lifecycle

- **Retained versions** are available for rollback.
- **Retired versions** can no longer be activated; their files are deleted after a grace period.
- The current version and any version referenced by an in-flight deployment are never cleaned up.

Upload timestamps use second-precision UTC Unix time. Each activation (including rollbacks) appends an event.

## Namespace Model

The first project created in a namespace determines its mode:

- **Root site mode:** The namespace itself is the site (empty project name internally, but still has a project ID).
- **Sub-project mode:** Multiple named projects under one namespace.

These modes are mutually exclusive. Delete all projects to switch.

URLs use names (`/~team/docs/`), but permissions are bound to immutable IDs. Deleting and re-creating a project with the same name does not inherit the old project's API keys or OIDC trusts.

## OIDC Trust Rules

Each trust rule binds an issuer + GitLab project ID + branch and tag allowlists. All conditions in a rule must be satisfied together — you can't combine permissions across rules.

- Separate branch and tag allowlists: exact names or patterns with `*` (zero or more characters, including `/`). An empty list denies that ref type; `*` allows all. Other characters are literal.
- `require_ref_protected` (default: false): strict boolean check on the token's `ref_protected` claim.
- `allow_mr_pipelines` (default: false): same-repo MRs only, matched by source branch. Cross-fork MRs are always rejected.

Short-lived credentials are bound to the matched trust ID. Deleting a trust rule immediately revokes its publishing ability.

## Server Messages

Instance messages are injected via a response wrapper that preserves `ResponseController.Unwrap`. The `X-Hoshilet-Server-Message` header is only added on final 2xx responses. The CLI validates and displays the message before parsing the JSON body or checking the status code.

## Background Tasks

Maintenance tasks (recovery, cleanup) convert internal panics from database helpers into errors, log failures, and continue on the next cycle.
