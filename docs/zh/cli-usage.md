# CLI 使用指南

## 快速开始

设置两个环境变量就可以开始部署：

```sh
export HOSHILET_SERVER=https://hoshilet-admin.example.com
export HOSHILET_API_KEY=your-key
```

把一个目录部署为静态站点：

```sh
hoshilet deploy ./dist --namespace team --project docs
```

站点会立即上线，访问地址是 `/~team/docs/`。

## 部署

基本的部署命令会把目录打包成 ZIP 并上传：

```sh
# 如果你的 Key 属于 namespace admin，项目会自动创建
hoshilet deploy ./dist --namespace team --project docs

# 也可以直接用项目 ID（project Key 不能自动创建项目）
hoshilet deploy ./dist --project-id PROJECT_ID
```

默认情况下，上传成功后就会自动上线。如果只想上传、不想立即上线，加 `--no-activate`：

```sh
hoshilet deploy ./dist --project-id PROJECT_ID --no-activate
```

### 并发安全

- `--revision` 防止误覆盖——如果别人在你之前发布了新版本，服务端会拒绝你的部署。
- `--idempotency-key` 用于安全重试失败的上传，不会产生重复的部署记录。

> **提示：** 如果部署似乎卡住了或者网络断了，先用 `hoshilet deployments list` 检查现有部署，不要急着用新的幂等键重试。

## 管理部署

```sh
# 列出项目的所有部署
hoshilet deployments list --project-id PROJECT_ID

# 激活（上线或回滚到）指定的部署版本
hoshilet activate DEPLOYMENT_ID --project-id PROJECT_ID
```

## JSON 输出

给任何命令加上 `--json`，可以在 stdout 输出机器可读的 JSON：

```sh
hoshilet --json deploy ./dist --namespace team --project docs
```

## GitLab CI（OIDC）

除了静态 API Key，还可以用 GitLab CI OIDC token 认证：

```sh
export HOSHILET_ID_TOKEN=$CI_JOB_JWT_V2   # 或者使用 GitLab 的 id_tokens
hoshilet deploy ./dist --oidc --project-id PROJECT_ID
```

`audience` 必须等于管理站点的 origin。namespace admin 需要预先在 Web 管理界面配置 OIDC 信任规则，绑定 GitLab issuer、GitLab 数字项目 ID 和允许的分支。

分支匹配规则：

- `*` 表示所有分支（不含 tag）。
- `all` 是字面量——只匹配名为 `all` 的分支。
- 不支持 `release/*` 之类的 glob。

每条信任规则可以独立设置：

- `require_ref_protected`（默认关闭）——只接受 `ref_protected` claim 为 `true` 的 token。
- `allow_mr_pipelines`（默认关闭）——允许同仓库的 MR 流水线发布，按来源分支匹配。跨 fork 的 MR 始终拒绝。

参考 [deploy/gitlab-ci.example.yml](../../deploy/gitlab-ci.example.yml) 中的 CI 配置示例。

> OIDC 部署不能自动创建项目。

## 限制

- **Namespace 模式固定：** 一个 namespace 要么是单个根站点，要么是多个子项目。只有删除所有项目后才能切换。
- **版本不可变。** 上线是原子符号链接切换，版本目录本身不会被修改。
- **空 ZIP 会被拒绝**（只有目录没有文件 → `422 empty_deployment`）。零字节文件是允许的。
- **上传中不能包含符号链接。** 路径不能越出归档根目录。ZIP 会在沙箱子进程中校验和解压。
- **只接受经典 ZIP 格式：** Store 或 Deflate。不支持 ZIP64、加密、自解压或多卷归档。归档里的文件本身可以是压缩文件，不会被递归解包。
- **版本保留：** 每项目默认保留 10 个版本，每版解压后最大 100 MiB。旧文件在 10 分钟宽限期后清理。
- **没有跨请求快照保证。** 建议资源名包含内容哈希。构建时请配置正确的 `/~namespace/project/` base path，平台不会改写 URL。
- **上传并发：** 全局默认 4 个、每用户 1 个、超时 120 秒。同一用户的多个 Key 共享配额；OIDC 配额按 issuer + GitLab 项目 ID 分配。可通过 `max_uploads`、`max_uploads_per_principal`、`upload_timeout_seconds` 调整。
- **公开站点共享 origin。** 管理站点必须使用独立 origin。当前版本不支持私有站点、自定义域名、SSR、在线构建、SPA fallback 或多副本。

## 服务端消息

实例管理员可以在服务端配置文件中设置广播消息：

```json
{"server_message": "欢迎使用本实例。\n维护时间：每周日 02:00–03:00。"}
```

CLI 会在 stderr 显示这些消息。同一命令内相同的消息只显示一次。格式异常的消息头只会输出固定的警告，绝不会原样回显。
