# 部署与运维

## 裸机部署（systemd）

1. 构建后端并安装：
   ```sh
   make build
   install -Dm755 bin/hoshilet-server /usr/local/bin/hoshilet-server
   ```

2. 创建专用系统用户 `hoshilet`，写入配置文件：
   ```sh
   install -Dm600 deploy/config.example.json /etc/hoshilet/config.json
   ```
   编辑配置：填入域名、GitLab OAuth 凭据和管理员 ID。

3. 安装并启动 systemd 服务：
   ```sh
   install -Dm644 deploy/systemd/hoshilet.service /etc/systemd/system/hoshilet.service
   systemctl daemon-reload
   systemctl enable --now hoshilet
   ```

4. 构建并安装管理界面：
   ```sh
   make web
   cp -r web/dist/* /srv/hoshilet/admin/
   ```
   这个目录要和用户站点分开存放。静态服务器对它只需要读权限。

5. 配置反代，使用 `deploy/Caddyfile` 或 `deploy/nginx.conf`（把示例域名替换成你自己的）。

### 反代规则

- `/api/*`、`/auth/*`、`/healthz` → 转发到 Go 后端。
- 其他请求 → 读取管理界面的静态目录。
- 前后端同 origin（不需要 CORS）。

URL 前缀 `/~namespace` 的转换完全在静态服务器的 rewrite 规则里完成——磁盘上的路径不包含 `~`。要确保目录跳转时保留 `~` 前缀，禁用目录列表，不要开启 SPA fallback。示例配置设置了 `must-revalidate` 缓存，确保回滚后立即生效。

### 文件权限

服务端会自动设置：`public/`、`releases/` → 0755；静态文件 → 0644；`database/`、`staging/` → 0700。静态服务器需要对 `public` 和 `releases` 的读取和目录遍历权限——不要给它数据库或配置目录的权限。

## 用户级 systemd 服务

如果不想创建系统用户，可以用用户级服务：

```sh
make build
install -Dm755 bin/hoshilet-server "$HOME/.local/bin/hoshilet-server"
install -Dm644 deploy/systemd/hoshilet-user.service \
  "${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/hoshilet-user.service"
install -Dm600 deploy/config.example.json \
  "${XDG_CONFIG_HOME:-$HOME/.config}/hoshilet/config.json"
```

编辑配置：填入域名、OAuth 凭据和管理员 ID。把 `data_dir` 从示例的 `/var/lib/hoshilet` 改成 `"."`（或者任意可写的绝对路径）。不要在 JSON 里写 `~`、`$HOME` 或 systemd 的 `%S`——程序不会展开它们。

服务使用 `StateDirectory=hoshilet`，状态目录会自动创建并作为工作目录。配置 `data_dir: "."` 时，数据库会在 `~/.local/state/hoshilet/database/hoshilet.db`。

```sh
systemctl --user daemon-reload
systemctl --user enable --now hoshilet-user.service
systemctl --user status hoshilet-user.service
journalctl --user -u hoshilet-user.service -f
```

如果需要退出登录后继续运行，让系统管理员执行 `loginctl enable-linger USERNAME`。

> **不要同时运行**指向同一数据目录的系统服务和用户服务。

## Podman Quadlet（容器部署）

```sh
make image    # 构建包含静态 Go 二进制的最小容器镜像
```

镜像里只有后端——没有管理界面，也不依赖 Node.js。单独用 `make web` 构建界面，挂载到反代容器里。

把 `deploy/quadlet/hoshilet.container` 安装到 `/etc/containers/systemd/`。示例按 rootful system unit 设计：

- 数据目录 `/var/lib/hoshilet`，owner 为 UID/GID 10001。
- `config.json` 的 owner 为 UID 10001，权限 0600。
- 容器内监听 `0.0.0.0:8080`，宿主机只发布 `127.0.0.1:8080`。
- 数据目录在容器内外保持同一路径（符号链接用相对目标）。
- SELinux 主机使用 `:z` 共享标签，给静态服务器配置只读访问策略。
- 静态服务器也容器化的话，只读挂载 `public` 和 `releases`，保持相对布局。不需要挂载 `database` 和 `staging`。

rootless 部署需要自行调整宿主目录、UID 映射和 Quadlet install target。

## 备份与恢复

### 备份

备份和运行中的服务使用同一个排他锁，所以**必须先停止服务**：

```sh
sudo systemctl stop hoshilet
sudo -u hoshilet hoshilet-server \
  -config /etc/hoshilet/config.json \
  -backup /path/writable-by-hoshilet/hoshilet.tar.gz
sudo systemctl start hoshilet
```

备份包含 SQLite 一致性快照和所有 release 文件，**不包含** staging 数据和 OAuth 配置密钥。配置文件需要单独备份。

> 备份文件（权限 0600）包含身份数据、Key 哈希和短期 OIDC 凭据，不要公开。

### 升级

先备份，再替换二进制或容器镜像并重启。当前版本使用 schema 5，会自动升级 schema 4，保留原有分支规则，并将 tag 白名单初始化为空（不允许 tag）。更早的原型 schema 仍需用新数据目录重新初始化。

### 恢复

恢复只接受可信的备份。目标 `data_dir` **必须不存在**（防止覆盖正在运行的站点）：

```sh
hoshilet-server -config restore-config.json -restore /path/hoshilet.tar.gz
```

恢复过程会校验 tar 路径、拒绝符号链接和重复条目，在临时目录中验证数据库和公开链接，全部通过后才原子地移到目标位置。验证失败会清理临时目录，不会留下半成品。

恢复完成后，检查目录所有权、配置路径和站点内容，再启动服务。

## 监控

| 内容 | 方式 |
|------|------|
| 健康检查 | `GET /healthz` |
| 指标 | `GET /api/v1/admin/metrics`（管理员会话）——磁盘、上传槽、任务计数、部署字节数 |
| 管理后台 | 待回收字节、待恢复数量、最近 500 条审计事件 |
| 日志 | 结构化 JSON——上传校验失败、服务错误、清理错误 |

### 上传配额

默认值：全局 4 个槽位、每用户 1 个、超时 120 秒。同一用户的所有 Key 共享额度；OIDC 配额按 issuer + GitLab 项目 ID 分配。通过 `max_uploads`、`max_uploads_per_principal`、`upload_timeout_seconds` 调整。每用户额度必须小于全局额度。磁盘空间不足时拒绝新上传。

### 一致性说明

原子符号链接切换能保证单个页面的切换是瞬时的，但多个 HTTP 请求获取不同资源时**不保证**看到同一个版本。建议使用内容哈希文件名。旧版本文件会保留一个宽限期（默认 10 分钟），帮助进行中的请求，但这只是尽力而为。

## 管理员空间用量

管理员可以在「空间用量」面板查看所有 namespace 及其磁盘占用。统计包含所有保留的部署和待清理文件（包括已删除但未清理的项目），不包含已清理的版本、上传临时文件、孤立文件和数据库开销。数值基于记录的解压文件大小，当前公开链接不会重复计算。
