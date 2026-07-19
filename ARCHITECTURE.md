# Architecture

## Shape

PPT 生成系统是单仓库单体部署形态：

```text
ppt-gen/
  cmd/server/           # Go 入口
  internal/
    httpapi/            # HTTP 路由、鉴权中间件、handlers
    store/              # JSON / SQLite / Postgres 持久化
    llm/                # OpenAI / Claude / Gemini 适配
    ppt/                # outline/SVG prompt 与解析
    pptx/               # 导出
    auth/               # 密码与 token 工具
    web/                # embed 静态前端 + 静态托管
  fronted/              # Next.js 前端（git submodule）
  build.sh              # 前端导出 + embed + go build
  config.yml            # 存储配置
  data/                 # 运行时数据（json/sqlite）
  bin/ppt-gen           # 构建产物
```

运行时一个二进制同时提供：

- `/api/*` 业务 API
- `/` 及前端静态页面（Next `output: "export"`）

## Deploy / Build Flow

```bash
./build.sh
# 1. fronted: lint + typecheck + next build (static export to out/)
# 2. copy fronted/out -> internal/web/dist
# 3. go test ./cmd/server ./internal/...
# 4. go build -o bin/ppt-gen ./cmd/server
```

运行：

```bash
ADDR=:8080 bin/ppt-gen
```

发布与部署：

```bash
# multi-arch release packages
./scripts/package-release.sh --version 1.0.0

# server first install
sudo ./scripts/deploy.sh --bin ./bin/ppt-gen --port 18080

# upgrade
sudo ./scripts/upgrade.sh --artifact ./bin/ppt-gen
```

详见 `deploy/README.md` 与 `.github/workflows/`。

可选：

- `-store json|sqlite|postgres`
- `-data` / `-dsn`
- `-storage-config config.yml`
- `-debug`

## Frontend Boundaries

- 公共页：`/`、`/login`、`/register`、`/signup`
- 受保护页：`app/(protected)/**` + `AppShell`
- 路由导航源：`fronted/lib/navigation.ts`
- API 客户端：`fronted/lib/api.ts`（同源 cookie 会话）

重要约束：

- 静态导出，无 Next middleware / SSR 鉴权
- 鉴权与 setup 检查在 client bootstrap（`AppShell` / auth forms）
- 刷新子路由依赖 Go `internal/web/handler.go` 正确解析 `path/index.html`

## Backend Boundaries

- `httpapi`：HTTP 适配层（decode / auth middleware / writeJSON）
- `service/generation`：大纲与 SVG 生成编排（prompt、LLM 调用、worker pool）
- `domain`：纯规则（并发优先级、requestJson 校验、prompt 默认值）
- `store`：持久化接口 + JSON/SQL 实现 + 额度规则 + snapshot 迁移
- `llm`：provider 请求与响应
- `ppt`：生成领域的 prompt/schema/parse
- `web`：静态资源托管与 cache/gzip

`StoreManager` 只管理存储生命周期（`Store/Config/Test/Configure/Switch`），不再实现完整 `store.Store`。  
handlers 通过 `Server.dataStore()` 访问当前数据平面。

## Bootstrap

前端鉴权入口优先走：

```http
GET /api/bootstrap
```

一次返回：

- `needsSetup`
- `storageConfigured`
- `user?`
- `quota?`

客户端共享封装：`fronted/lib/auth-bootstrap.ts` → `loadSessionSnapshot()`。

旧接口 `/api/setup/status`、`/api/auth/me`、`/api/me/quota` 仍保留兼容。

## Async Generation

长耗时生成已改为异步任务，避免用户同步等待：

```http
POST /api/jobs/outline
POST /api/jobs/svg
GET  /api/jobs/{id}
GET  /api/jobs
```

- 后端：`internal/jobs` 进程内任务队列 + worker
- SVG 任务仍先 `ReserveDailyQuota`，成功 `Commit`，失败 `Release`
- 前端：`create*Job` + `waitForJob` 轮询；提交后可切换页面
- 旧同步接口 `/api/architect`、`/api/generate-svg` 仍保留兼容

## Current Architecture Status

已完成：

1. 死代码/未用依赖清理
2. 基础回归测试（domain/llm/store/navigation）
3. 前端 Auth bootstrap 统一 + workspace 步骤拆分
4. 后端 domain/store rules + httpapi 响应拆分
5. `/api/bootstrap` 聚合接口
6. `service/generation` 抽出生成编排
7. `StoreManager` 收敛为纯生命周期
8. SQL `schema_version` 有序迁移
9. admin handlers 按域拆分（users/prompts/validate/settings）
10. auth bootstrap / generation concurrency / schema version 测试
11. 大纲/页面生成异步化，用户提交后可离开当前操作

可选后续增强：

1. 任务持久化（重启不丢）/ 多实例共享队列
2. WebSocket/SSE 推送替代轮询
3. mock LLM 全链路 e2e
4. OpenAPI/共享类型生成减少前后端漂移
