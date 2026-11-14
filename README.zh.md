# Hoshilet

[English](README.md)

自托管的静态 Pages 平台。上传 ZIP，站点立即上线，访问路径为 `/~team/project/`。

- GitLab OAuth 登录，namespace admin/member 协作
- ZIP 上传部署和回滚
- API Key 和多 GitLab 实例 OIDC 发布
- 支持 systemd 裸机部署、用户级服务和 Podman Quadlet

URL `/~team/docs/` 在磁盘上映射到 `public/team/docs/`（本地路径不含 `~`）。站点的增删和版本切换不需要修改反代配置。管理服务停止后，公开站点仍然正常访问。

## 安装 CLI

npm 和 PyPI 的包名都是 `hoshilet`，提供相同的 `hoshilet` 命令，任选一种：

```sh
# Python
uv build --project cli/python --out-dir dist/python
uv tool install dist/python/hoshilet-0.1.0-py3-none-any.whl

# Node.js
pnpm --dir cli/npm pack --pack-destination ../../dist/npm
npm install -g ./dist/npm/hoshilet-0.1.0.tgz
```

## 快速开始

```sh
export HOSHILET_SERVER=https://hoshilet-admin.example.com
export HOSHILET_API_KEY=your-key

hoshilet deploy ./dist --namespace team --project docs
```

## 文档

- [CLI 使用指南](docs/zh/cli-usage.md) — 命令、选项、OIDC、限制
- [开发与构建](docs/zh/build.md) — 前置依赖、构建、本地开发、GitLab OAuth
- [部署与运维](docs/zh/operations.md) — systemd、Podman、备份恢复、监控
- [架构](docs/zh/architecture.md) — 内部实现、部署流程、崩溃恢复
- [安全](docs/zh/security.md) — 认证模型、ZIP 校验、隔离
- [OpenAPI](docs/openapi.json)
