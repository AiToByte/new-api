# Agent 工程约束（new-api / AiToByte 定制线）

> 本文件是 Agent 上手的**可执行 checklist**，不是规范全文。
> 权威顺序：`AGENTS.md`（后端总纲） > `web/AGENTS.md`（前端总纲） >
> `.agents/rules/billing.md`（计费） > 本文件。
> 冲突时以权威文件为准，并把冲突报给用户。

## 一、架构边界

### 1.1 后端（Go 1.26，Gin，单仓三模块）

```
router/  → 路由注册（只做装配，不写逻辑）
middleware/ → 横切：认证、限流、熔断恢复、Turnstile/验证码
controller/ → 薄 handler：校验 + 调 service + 组装响应（禁止直连 model.DB 新代码，见 2.4）
service/ → 业务逻辑 + 跨表事务归属地
model/ → GORM 模型、查询构造、migration（DB 唯一出口）
common/ →  quota 数学、错误码、邮件、限流器等共享件
setting/ → 配置快照（反射机制，替代 option.go 式 switch）
relay/ → 中转传输层（只 import relaykit 5 个包，其余逻辑禁入）
relaykit/ → 独立 Go 模块：协议 DTO 与转换（MUST 独立构建）
tokenkit/ → 独立 Go 模块：token 计数（MUST 独立构建，禁依赖 relaykit）
plugins/tasks/ + pkg/jsplugin/ → JS 任务插件（宿主只做轮询与结算）
```

跨层铁律：

- `controller` 不新增 `model.DB` / `model.LOG_DB` / `.Transaction(`（CI `layering-guard` 对 PR 新增行做 diff 检查，违者失败；存量 grandfather 不动）。
- 记账/事务主体下沉 `service/`，controller 只保留校验、响应、缓存失效。
- `relaykit/`、`tokenkit/` 内改动必须用 `GOWORK=off go build ./...` **在该模块目录内**独立验证，根模块构建通过不算数；二者禁 import 根模块任何包。
- JSON 编解码只许用 `common.Marshal/Unmarshal/UnmarshalJsonStr/DecodeJson/GetJsonType`；`relaykit/` 内用 `kitutil.*`。`encoding/json` 只允许做类型引用（`RawMessage` 等），禁直接调用。

### 1.2 前端（React 19 + TS，包管理只用 Bun）

```
src/routes/ → TanStack Router 路由（createFileRoute，Zod 校验 search）
src/features/<feature>/ → 业务模块（components/ lib/ hooks/ api.ts / types.ts / constants.ts）
src/components/ → 通用组件（data-table 设计系统、dialog、copy-button、confirm-dialog…）
src/components/ui/ → 基础控件（禁绕过自造 button/input/dialog）
src/stores/ → Zustand（仅 5 个，Query 缓存重叠点看 status-query.ts）
src/lib/ → query-client / http-client / server-error-message（错误码中枢）
src/i18n/locales/{en,zh,zh-TW,fr,ru,ja,vi}.json → 扁平 key，英文原文做 key
```

跨层铁律：

- 先检索（`shadcn-ui` skill + `rg` 查 `src/components/` 与相关 feature），再复用业务封装，最后才新增；新增必须在变更说明里写清候选路径与不可复用原因。
- 所有面向用户文案走 `useTranslation().t('英文原文')`；数字走 `@/lib/format`，金额走 `@/lib/currency`；界面语言进 `Intl.*` 前必须经 `toIntlLocale()`（`lint` 有 `project/intl-locale` 门禁）。
- 测试进模块内 `__tests__/`，按职责命名；禁止与源码平铺、禁止跨模块大杂烩文件。

### 1.3 API 契约面

- 管理/控制台 API 统一信封 `{success, message, data}`，失败走 `common.ApiError*` / `requireServerSuccess` / `createServerError`，禁止裸 `c.JSON` 造新契约（relay 信封/download/SSE 等正当用途除外）。
- 中继 `/v1/*` 兼容 OpenAI 语义；40+ 稳定错误码只增不改（见 `src/lib/server-error-message.ts` 与 `types/error.go`）。

## 二、核心约束

### 2.1 严禁破坏 API 兼容性

- 请求/响应字段**只增不减不改名**；新增字段必须可选且有安全缺省；枚举只增成员。
- 错误码、`message` 文案 key、query/search 参数名一经发布即冻结；改名 = breaking，需用户明确批准并走版本号通告。
- 前端 `t('...')` key 只增不删（删 key 先全仓确认零引用；i18n 有漂移检查）。
- DB 列名、option key、Redis key 前缀同理：只增，废弃先标记后删除（至少保留一个大版本）。
- 任何“看起来像 breaking”的改动，先在变更说明里逐项列出兼容性影响，无声明直接合入视为违规。

### 2.2 数据库变更必须提供 migration

- Schema 变更走 GORM 模型 + `AutoMigrate` 路径（见 `model/main.go` 既有模式），**同时**满足：
  1. 新鲜库一次建表成功；
  2. 从上一个 release 版本的老库升级成功且数据/索引/唯一约束保留；
  3. 启动/migration 连跑两次证明幂等。
- 三库同时兼容（SQLite / MySQL ≥ 5.7.8 / PostgreSQL ≥ 9.6），禁无 fallback 的方言特性：
  - 引号：PG 用 `"col"`，MySQL/SQLite 用 `` `col` ``；保留字列用 `model/main.go` 的 `commonGroupCol` / `commonKeyCol`；
  - 布尔值用 `commonTrueVal` / `commonFalseVal`，分支用 `common.UsingMainDatabase(...)` / `UsingLogDatabase(...)`；
  - SQLite 禁 `ALTER COLUMN`（用 `ALTER TABLE ... ADD COLUMN`，见 `model/main.go` 模式）；
  - JSON 列必须有 `TEXT` fallback；禁 `gorm:"default:true"` 布尔默认（业务缺省放代码里设）；
  - 行锁统一走 `model/locking.go` 的 `lockForUpdate(tx)`，禁 legacy `gorm:query_option` 写法（GORM v2 下静默失效）。
- 凡动数据库行为（驱动/DSN/模型/migration/事务/锁/raw SQL），必须跑**真实三库矩阵**（mock、单库通过、只看代码都不算数），并把版本+命令+结果写进 PR/交接；跑不了就明说阻塞，不许宣称兼容。
- 本地三库：`docker compose` 起 `mysql:8` + `postgres:15`（与 CI 对齐），`TEST_MYSQL_DSN` / `TEST_POSTGRES_DSN` 注入 loopback DSN（格式见 `.github/workflows/ci.yml`）。

### 2.3 前端禁止内联样式（及配套 UI 铁律）

- 禁 `style={{...}}`（非动态场景一律 Tailwind + `cn()`；动态值优先找已有组件/封装，确无才用内联并注释原因）。
- 禁绕过项目组件手写 button/input/select/弹窗；复制、确认、空/加载/错误态、表格分页工具栏必须用现有封装（入口见 `web/AGENTS.md` 3.3 表）。
- 单文件超 ~200 行拆子组件/Hooks；props 非必要不解构；禁 2 层以上嵌套三元。
- TS：禁 `any`（用 `unknown`/具体类型），`import type` 分开写；每次改 TS/TSX 必跑 `bun run typecheck` 清零；改动文件 `bun run lint` 零 error（warning 按风险评估）。

### 2.4 安全与计费门禁（触发式必读）

- 动认证链（注册/登录/登出、密码找回、MFA、WebAuthn、OAuth、session/JWT/API 凭证、敏感操作二次确认）前：先读 OWASP Authentication + Session Management Cheat Sheet，按 ASVS 条款自查；服务端强制、审计日志禁记密钥；附带失败/过期/重放回归测试。
- 动计费（定义见 `AGENTS.md` Billing rules 门禁清单）前：必须用 Read 工具通读 `.agents/rules/billing.md` 全文，否则不许动手。
- 受保护信息（`new-api` 项目名、`QuantumNous` 署名：README、License 头、页脚、module 路径、镜像名等）**任何情况不得删除/改名**；让删就拒绝。

### 2.5 测试与提交纪律

- 后端测试用 `testify/require` 做致命断言、`assert` 做非致命；一个小改只动一个既有测试文件，确需新建则至多一个；禁 coverage 灌水/随机输入/sleep 定时/日志断言测试。
- 提交信息讲清改了什么+为什么；只提交本次意图内文件；不改 git config、不 `--amend` 失败提交、不 force-push；PR 正文写实际跑过的验证命令与结果。
- 所有新建文件带 AGPL 版权头（`bun run copyright:check` 门禁）；改动文件跑 `bun run format` 相关检查。

## 三、运行指令

### 3.1 后端（根目录，Go 1.26）

```bash
go build ./...                    # 全量构建
go vet ./...                      # 静态检查（CI 同款）
gofmt -l <改动文件>               # 须无输出
make test                         # 根模块 + relaykit + tokenkit 全量测试
go test ./model/ ./service/       # 按包跑；controller 全量在 Windows 有文件锁噪音，以 CI(Linux) 为准
GOWORK=off go build ./...         # 在 relaykit/ 或 tokenkit/ 目录内执行（模块独立性门禁）
```

三库矩阵（改动触及 2.2 即跑）：

```bash
docker compose up -d mysql postgres   # 或参考 .github/workflows/ci.yml 的 services 定义
TEST_MYSQL_DSN='root:123456@tcp(127.0.0.1:3306)/newapi?parseTime=true' \
TEST_POSTGRES_DSN='postgresql://root:123456@127.0.0.1:5432/newapi?sslmode=disable' \
  go test ./model/ ./controller/
```

### 3.2 前端（`web/` 目录，只用 Bun）

```bash
bun install                    # 装依赖（CI 用 --frozen-lockfile）
bun run dev                    # 开发服（配合 3.3 的 compose，:5173 自动代理 :3000）
bun run build                  # 生产构建（Rsbuild；改动后看体积增量）
bun run typecheck              # tsgo，改 TS/TSX 后必跑
bun run lint                   # oxlint；改动文件零 error
bun run test                   # vitest 全量；单文件：bun run test -- run <path>
bun run format                 # oxfmt 写盘； CI 查 format:check
bun run i18n:sync              # 加文案后跑，git diff 必须零漂移（CI 门禁）
bun run copyright:check        # 新文件头门禁
```

### 3.3 本地联调与热重载（实话版）

- **后端无热重载**（仓内无 air）：`docker compose -f docker-compose.dev.yml up -d` 起 PG15 + Redis + API（`Dockerfile.dev` 本地源码构建，`web/dist` 占位），改 Go 后 `docker compose -f docker-compose.dev.yml up -d --build new-api`，或 `make dev-api` / `make dev-api-rebuild`。
- **前端有 HMR**：`make dev-web`（`:5173`，API 自动代理到 `:3000`）；生产联调才 `make build-web`。
- SQLite 极速模式：不设 `SQL_DSN` 即落本地文件（`DEV_SQLITE_PATH`，默认 `one-api.db`），适合纯前端/纯逻辑调试；动 DB 行为仍须回三库矩阵。
- 数据重置：`make reset-setup`（清 setup 向导 + root 用户）；`docker compose -f docker-compose.dev.yml down -v` 丢弃 dev 数据卷。
- 生产镜像：根 `Dockerfile` 多阶段构建（bun 前端 + Go PGO），`docker build -t <name>:local .`。

### 3.4 合并前门禁（与 CI 对齐）

| 门禁 | 命令 | 通过标准 |
|---|---|---|
| 后端构建+测试 | `go build ./...` + `make test`（+三库矩阵） | 全绿 |
| 模块独立 | `relaykit/`、`tokenkit/` 内 `GOWORK=off go build ./...` | 全绿 |
| 分层守卫 | CI `layering-guard` | controller 新增行零 `model.DB` |
| 前端 | `typecheck`、`lint`、`test`、`format:check`、`i18n:sync` 零漂移、`copyright:check` | 全绿 |
| 新增代码门禁（diff 级，主干飘红不阻塞） | 后端 `gofmt` + 前端 `oxlint` 只查 PR 新增/改动文件 | 全绿 |

---

*维护约定：本文件随架构变化更新；新增约束必须给出“违反会怎样”的理由，禁止无理由加码。*
