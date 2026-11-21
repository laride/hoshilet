#!/usr/bin/env python3

"""Generate the checked-in OpenAPI contract without external tooling."""

import json
from pathlib import Path


def obj(properties, required=()):
    return {
        "type": "object",
        "properties": properties,
        "required": list(required),
        "additionalProperties": False,
    }


s = {"type": "string"}
i = {"type": "integer", "format": "int64"}
b = {"type": "boolean"}
ref = lambda name: {"$ref": "#/components/schemas/" + name}
schemas = {
    "Error": obj(
        {"error": obj({"code": s, "message": s}, ["code", "message"])}, ["error"]
    ),
    "ProjectInput": obj(
        {
            "name": s,
            "mode": {
                "type": "string",
                "enum": ["root", "projects"],
                "default": "projects",
            },
        }
    ),
    "KeyInput": obj(
        {
            "name": s,
            "scope": {"type": "string", "enum": ["account", "namespace", "project"]},
            "target": s,
            "days": {"type": "integer", "minimum": 1, "maximum": 365, "default": 90},
        },
        ["scope"],
    ),
    "TrustInput": obj(
        {
            "issuer_id": s,
            "repository_id": {"type": "string", "pattern": "^[0-9]+$"},
            "branches": {
                "type": "array",
                "items": s,
                "maxItems": 100,
                "description": "Branch allowlist. * matches zero or more characters, including /. Omitted or empty denies all branches. Other characters are literal.",
            },
            "tags": {
                "type": "array",
                "items": s,
                "maxItems": 100,
                "description": "Tag allowlist, e.g. v* or * for all tags. Same wildcard syntax as branches. Omitted or empty denies all tags.",
            },
            "allow_mr_pipelines": {
                "type": "boolean",
                "default": False,
                "description": "Allow same-repository MR pipelines for this trust only; fork MR is always rejected.",
            },
            "require_ref_protected": {
                "type": "boolean",
                "default": False,
                "description": "Require the signed ref_protected claim to be true (boolean or string). Missing/false claims fail closed.",
            },
        },
        ["issuer_id", "repository_id"],
    ),
    "SettingsInput": obj(
        {
            "max_versions": {"type": "integer", "minimum": 1, "maximum": 1000},
            "max_bytes": i,
        },
        ["max_versions", "max_bytes"],
    ),
    "ActivationInput": obj(
        {"deployment_id": s, "revision": i}, ["deployment_id", "revision"]
    ),
    "Upload": {
        "type": "object",
        "properties": {
            "id": s,
            "project_id": s,
            "deployment_id": s,
            "revision": i,
            "activate": i,
            "status": {
                "type": "string",
                "enum": ["receiving", "validating", "publishing", "complete", "failed"],
            },
            "error": s,
            "created_at": i,
        },
    },
    "Project": {
        "type": "object",
        "properties": {
            "id": s,
            "namespace_id": s,
            "name": s,
            "current_id": s,
            "revision": i,
            "disabled": i,
            "created_at": i,
        },
    },
    "Deployment": {
        "type": "object",
        "properties": {
            "id": s,
            "project_id": s,
            "uploaded_at": i,
            "size": i,
            "files": i,
            "last_activated_at": {"type": ["integer", "null"]},
            "status": s,
        },
    },
}
usage = {"retained_bytes": i, "retired_bytes": i, "used_bytes": i}
schemas["NamespaceUsage"] = {
    "type": "object",
    "properties": {
        "id": s,
        "name": s,
        "mode": s,
        "member_count": i,
        "project_count": i,
        **usage,
    },
    "description": "Logical deployment file bytes; includes ready and retired versions, excludes purged, staging, DB and filesystem allocation overhead.",
}
schemas["NamespaceUsageDetail"] = {
    "type": "object",
    "properties": {
        "namespace": {"type": "object"},
        "projects": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "id": s,
                    "name": s,
                    "disabled": i,
                    "deleted": i,
                    "current_id": s,
                    "retained_versions": i,
                    **usage,
                },
            },
        },
        "members": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "user_id": s,
                    "username": s,
                    "gitlab_id": i,
                    "role": s,
                    "status": s,
                },
            },
        },
    },
}
paths = {}


def add(
    path,
    method,
    summary,
    body=None,
    response=None,
    status="200",
    session=False,
    public=False,
):
    operation = {
        "summary": summary,
        "operationId": method
        + "_"
        + path.strip("/").replace("/", "_").replace("{", "").replace("}", ""),
        "security": []
        if public
        else ([{"session": []}] if session else [{"bearer": []}, {"session": []}]),
        "responses": {
            status: {
                "description": "Success",
                "content": {
                    "application/json": {
                        "schema": response or {"type": ["object", "array"]}
                    }
                },
            },
            "default": {
                "description": "Error; 401 invalid credentials, 403 permission denied, 409 conflict, 413 size limit, 408 upload deadline, 422 archive rejected or empty deployment, 429 global/principal capacity reached, 507 insufficient disk",
                "content": {"application/json": {"schema": ref("Error")}},
            },
        },
    }
    if body:
        operation["requestBody"] = {
            "required": True,
            "content": {"application/json": {"schema": body}},
        }
    params = []
    for part in path.split("/"):
        if part.startswith("{"):
            params.append(
                {"name": part[1:-1], "in": "path", "required": True, "schema": s}
            )
    if params:
        operation["parameters"] = params
    paths.setdefault("/api/v1" + path, {})[method] = operation
    return operation


add("/me", "get", "Current identity and management metadata")
add("/logout", "post", "Revoke current credential")
op = add(
    "/namespaces",
    "get",
    "List actual namespace memberships by default; platform administrators may explicitly request all",
)
op["parameters"] = [
    {
        "name": "scope",
        "in": "query",
        "schema": {"type": "string", "enum": ["member", "all"], "default": "member"},
    }
]
add("/requests", "get", "List own namespace requests", session=True)
add(
    "/requests",
    "post",
    "Request an additional namespace",
    obj({"name": s}, ["name"]),
    status="201",
    session=True,
)
add("/keys", "get", "List own API keys without secrets", session=True)
add(
    "/keys",
    "post",
    "Create an API key; returned token is displayed once",
    ref("KeyInput"),
    status="201",
    session=True,
)
add(
    "/keys/{key_id}",
    "delete",
    "Revoke own key; missing=404, already revoked=200",
    session=True,
)
add("/namespaces/{namespace_id}/projects", "get", "List projects")
add(
    "/namespaces/{namespace_id}/projects",
    "post",
    "Create root site or child project (namespace admin)",
    ref("ProjectInput"),
    ref("Project"),
    "201",
    True,
)
add(
    "/namespaces/{namespace_id}/members",
    "get",
    "List namespace members (namespace admin)",
    session=True,
)
add(
    "/namespaces/{namespace_id}/members",
    "put",
    "Add or change a registered member (namespace admin)",
    obj(
        {"gitlab_id": i, "role": {"type": "string", "enum": ["admin", "member"]}},
        ["gitlab_id"],
    ),
    session=True,
)
add(
    "/namespaces/{namespace_id}/members/{user_id}",
    "delete",
    "Remove member; missing=404, last admin is protected",
    session=True,
)
add(
    "/projects/{project_id}", "get", "Get project and revision", response=ref("Project")
)
add(
    "/projects/{project_id}",
    "delete",
    "Delete project and unpublish immediately",
    session=True,
)
add(
    "/projects/{project_id}/deployments",
    "get",
    "List retained versions",
    response={"type": "array", "items": ref("Deployment")},
)
add(
    "/projects/{project_id}/deployments/{deployment_id}",
    "delete",
    "Retire a ready non-current version; missing=404, retired/purged=409 (namespace admin)",
    session=True,
)
add(
    "/projects/{project_id}/activate",
    "post",
    "Activate or roll back using optimistic revision",
    ref("ActivationInput"),
    ref("Project"),
)
add("/projects/{project_id}/activations", "get", "List activation events")
add(
    "/projects/{project_id}/trusts",
    "get",
    "List trusted publishers (namespace admin)",
    session=True,
)
add(
    "/projects/{project_id}/trusts",
    "post",
    "Add trusted publisher (namespace admin)",
    ref("TrustInput"),
    status="201",
    session=True,
)
add(
    "/projects/{project_id}/trusts/{trust_id}",
    "delete",
    "Revoke publisher trust; missing=404",
    session=True,
)
add("/issuers", "get", "List configured issuer IDs and origins", session=True)
op = add(
    "/oidc/exchange",
    "post",
    "Exchange GitLab ID token for short-lived project credential",
    obj({"token": s, "project_id": s}, ["token", "project_id"]),
    public=True,
)
op["parameters"] = [
    {
        "name": "Idempotency-Key",
        "in": "header",
        "required": True,
        "schema": {"type": "string", "minLength": 8, "maxLength": 128},
    }
]
op = add(
    "/deploy",
    "post",
    "Upload ZIP; auto-create child project only for an authorized account/namespace API key",
    response=ref("Upload"),
    status="201",
)
op["responses"]["200"] = {
    "description": "Idempotent replay returns original upload state",
    "content": {"application/json": {"schema": ref("Upload")}},
}
op["requestBody"] = {
    "required": True,
    "content": {"application/zip": {"schema": {"type": "string", "format": "binary"}}},
}
op["parameters"] = [
    {"name": k, "in": "query", "schema": s}
    for k in ["namespace", "project", "project_id"]
] + [
    {"name": "activate", "in": "query", "schema": {"type": "boolean", "default": True}},
    {
        "name": "Idempotency-Key",
        "in": "header",
        "required": True,
        "schema": {"type": "string", "minLength": 8, "maxLength": 128},
    },
    {
        "name": "If-Match",
        "in": "header",
        "description": "Project revision",
        "schema": s,
    },
]
add("/uploads/{upload_id}", "get", "Get persisted upload state", response=ref("Upload"))
for name in ["settings", "requests", "users", "issuers", "audit", "metrics"]:
    add("/admin/" + name, "get", "Platform admin: " + name, session=True)
add(
    "/admin/namespaces",
    "get",
    "All namespace usage (platform admin only)",
    response={"type": "array", "items": ref("NamespaceUsage")},
    session=True,
)
add(
    "/admin/namespaces/{namespace_id}",
    "get",
    "Namespace projects and members, including deleted projects awaiting GC",
    response=ref("NamespaceUsageDetail"),
    session=True,
)
add(
    "/admin/settings",
    "put",
    "Replace all settings; missing or null fields are rejected with 400",
    ref("SettingsInput"),
    session=True,
)
add(
    "/admin/requests/{request_id}",
    "post",
    "Approve or reject namespace request",
    obj({"approve": b, "reason": s}, ["approve"]),
    session=True,
)
add(
    "/admin/users",
    "post",
    "Preapprove a GitLab numeric user ID; username is fetched on login",
    obj({"gitlab_id": i}, ["gitlab_id"]),
    status="201",
    session=True,
)
add(
    "/admin/users/{user_id}",
    "patch",
    "Approve or disable user; disabled users cannot publish and their API keys are revoked",
    obj({"status": {"type": "string", "enum": ["approved", "disabled"]}}, ["status"]),
    session=True,
)
add(
    "/admin/projects/{project_id}",
    "patch",
    "Disable or re-enable public site",
    obj({"disabled": b}, ["disabled"]),
    session=True,
)
add(
    "/admin/issuers",
    "post",
    "Register trusted HTTPS GitLab issuer",
    obj({"url": s, "jwks_url": s}, ["url", "jwks_url"]),
    status="201",
    session=True,
)
add(
    "/admin/issuers/{issuer_id}",
    "patch",
    "Enable or disable CI issuer",
    obj({"enabled": b}, ["enabled"]),
    session=True,
)
root = {
    "openapi": "3.1.0",
    "info": {
        "title": "hoshilet API",
        "version": "0.1.0",
        "description": "Namespace and project IDs are immutable opaque strings. Browser mutations require Origin matching the configured management origin. Times are UTC Unix seconds. Integer DB flags are 0/1.",
    },
    "paths": paths,
    "components": {
        "securitySchemes": {
            "bearer": {"type": "http", "scheme": "bearer"},
            "session": {
                "type": "apiKey",
                "in": "cookie",
                "name": "__Host-hoshilet-session",
            },
        },
        "schemas": schemas,
    },
}
for path in paths.values():
    for operation in path.values():
        for status, response in operation["responses"].items():
            if status.startswith("2"):
                response["headers"] = {
                    "X-Hoshilet-Server-Message": {
                        "description": "Optional standard Base64 of UTF-8 plain text, at most 512 decoded bytes (684 header bytes). LF allowed; other control characters forbidden. Current server emits on 2xx only; clients display on any status, on stderr.",
                        "schema": {"type": "string", "maxLength": 684},
                    }
                }
Path("docs/openapi.json").write_text(json.dumps(root, indent=2) + "\n")
