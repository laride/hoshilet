# 开发与构建

## 前置依赖

| 工具 | 版本 | 用途 |
|------|------|------|
| Go | 1.27.1 | 后端服务 |
| Node.js | 22+ | npm CLI、管理界面 |
| pnpm | 最新 | npm CLI、管理界面依赖管理 |
| uv | 最新 | Python CLI |

后端只需要 Go。React 管理界面是独立构建的，由 Caddy 或 Nginx 提供服务，不会嵌入到 Go 二进制里。

## 构建

```sh
pnpm --dir web install --frozen-lockfile
pnpm --dir cli/npm install --frozen-lockfile
uv sync --project cli/python

make build       # 后端 → bin/hoshilet-server
make web         # 管理界面 → web/dist/
make test        # Go 测试 + 两套 CLI
make cli-test    # 端到端测试：启动真实 HTTP 服务器，测试两套 CLI
```

`web/dist/` 是构建产物，不提交到 Git。

## 本地运行

```sh
cp deploy/config.example.json config.json
# 填入域名、GitLab OAuth 凭据和管理员 ID。
# 不要提交这个文件——里面有密钥。

./bin/hoshilet-server -config config.json -check   # 检查配置
./bin/hoshilet-server -config config.json           # 启动服务
```

服务默认监听 `127.0.0.1:8080`，只提供 API，不提供页面。要使用管理界面：

1. 构建界面：`make web`
2. 把 `web/dist/` 的内容复制到 `/srv/hoshilet/admin/`（或者反代静态目录的其他位置）。
3. 配置反代：把 `/api/*`、`/auth/*`、`/healthz` 转发到后端，其他请求读取管理界面的静态目录。
4. 前后端必须在同一个 origin 下——不需要 CORS。

> **注意：** 网页登录需要通过配置好的 HTTPS 管理域名访问（Cookie 设了 `Secure`）。直接用 HTTP 开发端口无法完成 OAuth 登录。

运行 `./bin/hoshilet-server -version` 可以查看版本和 Git 构建信息（不需要配置文件）。

服务端二进制叫 `hoshilet-server`，CLI 工具叫 `hoshilet`，两者可以同时安装。

## npm CLI 开发

源码在 `cli/npm/src/`，使用 TypeScript 严格模式，用 tsdown 构建：

```sh
pnpm --dir cli/npm install --frozen-lockfile
pnpm --dir cli/npm typecheck
pnpm --dir cli/npm build     # → cli/npm/dist/hoshilet.mjs
pnpm --dir cli/npm test      # 类型检查 + 构建 + 测试
```

发布的 npm 包只包含 `dist/`。用户运行 CLI 不需要 TypeScript，只要 Node 22+。构建工具要求 Node `^22.18.0 || ^24.11.0 || >=26.0.0`。

`pnpm pack` 会通过 `prepack` 自动构建。`make cli-test` 也会先构建 npm CLI，再通过真实 HTTP 集成测试验证。

## 测试缓存

Go 测试缓存默认在 `/tmp/hoshilet-go-cache`。`make cli-test` 会在本地随机端口启动真实 HTTP 服务器，端到端测试两套 CLI 的打包、上传、查询、激活和暂存功能。

## GitLab OAuth 配置

在登录用的 GitLab 实例上创建 OAuth Application：

- **Redirect URI:** `https://你的管理域名/auth/callback`
- **Scopes:** `read_user`

服务端使用 PKCE，只读取基本身份信息，不查询群组，也不保存 GitLab access token。

### 用户管理

在配置文件的 `admin_ids` 中设置初始管理员（GitLab 数字用户 ID）。其他用户登录后会进入待审核页面——管理员在「用户白名单」面板批准，也可以预先添加数字 ID。

用户有三种状态：**pending**（待审核）、**approved**（已批准）、**disabled**（已禁用）。

- 只有 approved 用户可以创建空间和部署。
- 撤销用户会立即使其会话和所有 API Key 失效。重新批准不会恢复已撤销的 Key。
- 公开站点和 CI OIDC 信任不受个人用户撤销的影响。

### CI Issuers

登录用的 GitLab 只有一个实例。**CI issuers**（在管理后台配置）可以来自多个不同的 GitLab 实例，与登录实例无关。JWKS URL 通常是 `https://gitlab.example.com/oauth/discovery/keys`。

### 团队协作

namespace admin 可以添加已登录过的用户作为成员（默认角色：member）。member 可以部署和回滚已有项目，但不能创建项目、管理成员、删除版本或配置 OIDC。每个 namespace 至少保留一个 admin。
