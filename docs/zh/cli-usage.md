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

## 私有 CA

使用 `--extra-ca-certs FILE` 追加信任的 CA 证书，同时保留运行时默认信任库：

```sh
hoshilet --extra-ca-certs company-ca.pem deploy ./dist --project-id PROJECT_ID

# 可以重复传入多个文件，每个文件也可以包含多张 PEM 证书。
hoshilet deployments list --project-id PROJECT_ID \
  --extra-ca-certs company-ca.pem --extra-ca-certs team-ca.pem
```

参数可放在子命令前后，对本次命令中的所有 HTTPS 请求生效，包括 OIDC token 交换。证书及主机名校验仍然启用；证书文件不存在或格式无效时，命令会报错退出。

Python CLI 不增加依赖，也不改变版本要求（Python 3.10–3.15）。
npm CLI 使用的原生 TLS CA API 在 Node.js 22.19 和 Node.js 24.5 及后续版本中可用。不使用此参数时，仍保持现有的 Node.js 22 最低要求。较旧的 Node.js 可以改用运行时启动环境变量：

```sh
NODE_EXTRA_CA_CERTS=/path/to/company-ca-bundle.pem \
  hoshilet deploy ./dist --project-id PROJECT_ID
```

`NODE_EXTRA_CA_CERTS` 同样采用追加语义，但只在 Node.js 启动时读取。如果已经设置了该变量，`--extra-ca-certs` 也会保留其中的证书。

## GitLab CI（OIDC）

除了静态 API Key，还可以用 GitLab CI OIDC token 认证：

```sh
export HOSHILET_ID_TOKEN=$CI_JOB_JWT_V2   # 或者使用 GitLab 的 id_tokens
hoshilet deploy ./dist --oidc --project-id PROJECT_ID
```

`audience` 必须等于管理站点的 origin。namespace admin 需要预先在 Web 管理界面配置 OIDC 信任规则，绑定 GitLab issuer、GitLab 数字项目 ID 以及独立的分支和 tag 白名单。

分支和 tag 白名单匹配规则：

- 两份白名单独立匹配；不填或空列表表示不允许该类型发布。
- `*` 匹配零个或多个字符（包含 `/`），例如分支 `release/*`、tag `v*`。单独的 `*` 允许该类型的所有引用。
- `all` 是字面量——只匹配名为 `all` 的分支。
- 仅 `*` 是通配符，其他字符按字面匹配；匹配区分大小写且覆盖完整名称。

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
