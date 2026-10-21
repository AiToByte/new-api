# new-api 技术债台账

> 用途：为后续架构可维护性迭代与重点功能重写提供依据。记录**已实测**的债务证据、
> **不可动的健康资产**，以及分阶段偿还顺序与状态。
>
> 维护约定：每项债务带 `file:line` 证据；状态勾选框在对应改动合入后更新；
> 数字为 2026-10 对 `main` 分支的实测值，改动代码后需重新校准。
> 规则类文档见 `.agents/rules/billing.md`，本文件是诊断与跟踪文档，不是规范性规则。

## 一、健康资产（不要动）

重构时必须保留的结构。「简化」以下任何一项都是倒退。

| 资产 | 位置 | 为什么不能动 |
|---|---|---|
| `relaykit/` 模块边界 | `relaykit/`（独立 module） | 全仓最干净的结构。`relay/` 只 import 5 个包，转换逻辑（~21k 行）隔离在注册表后，`Meta` 是接口而非结构体耦合。**任何改动必须用 `GOWORK=off go build ./...` 在该模块内独立验证** |
| 额度数值单一权威 | `common/quota_math.go` | 饱和 + 审计 + typed error 三合一，int32 与 JS-safe 双域边界清晰。**引入第二个转换助手即违反 `.agents/rules/billing.md`** |
| 反降级凭证分类 | `middleware/auth.go:205` | `ParseDashboardAccessToken` 先无签名解析，命中 issuer/audience 即无条件走 internal 分支，防止畸形 dashboard JWT 降级成 PAT。极其隐蔽但是关键不变量 |
| 访问令牌路由表 | `middleware/access_token_routes.go:43-307` | `DeclareAccessTokenPermissionRoute` 重复声明直接 panic，使 Casbin 权限与 PAT scope 永不漂移；未声明路由 **fail closed** |
| 行锁助手 | `model/locking.go:22` `lockForUpdate` | SQLite 跳过、MySQL/PG 发 `FOR UPDATE`，25 处调用点全部走它。**禁止在调用点重复写 `clause.Locking`** |
| 自定义 Migrator | `model/migration_dialector.go` | 抑制 GORM 对 MySQL 零填充 decimal 默认值、PG `bpchar→char` 的误判迁移。删掉会导致每次启动重复 `ALTER TABLE` |
| 前端数据层基础设施 | `web/src/lib/query-client.ts`、`http-client.ts`、`server-error-message.ts` | 生产级。40 个稳定错误码 + 401 刷新重试 + 单次安全凭证防重放 |
| 前端表格设计系统 | `web/src/components/data-table/` | 36 文件 / 5037 行，含 README 与精选 barrel，被 77 个文件复用 |
| 前端状态层 | `web/src/stores/` | 仅 5 个 store / 578 行，全类型化；Query 与 store 的重叠点已在 `status-query.ts` 显式解决 |

## 二、债务清单

### D1 · Adaptor 接口 57% 是样板 —— 新增渠道成本被放大 3-5 倍

`relay/channel/adapter.go:18` 定义了 **15 个方法**的单体接口，36 个 `relay/channel/*/adaptor.go`（共 **7856 行**）必须全部实现：

| 实测项 | 数量 |
|---|---|
| `errors.New("not implemented")` 返回 | **103** |
| `return request, nil` 直接透传 | **54** |
| `GetRequestURL` 中手写 `fmt.Sprintf` URL | 8（另 12 个用共享 helper，9 个 trivial） |
| `GetChannelName` 方法体 | 34 个全部 trivial |

Go 没有默认方法，因此每个新渠道都要手写 11 个「不支持」桩。

同时存在**三种互相矛盾的复用写法**：`relay/channel/tencent/dispatch.go:15` 嵌接口、`sub2api/adaptor.go` 嵌结构、`newapi`/`advancedcustom` 按字段组合。

> 注：task 侧已用正确的能力接口模式（`adapter.go:94-136` 定义了 6 个），可直接照搬。

### D2 · 12 个 `panic("implement me")` 被包成**可重试**错误

以下 adaptor 的 `ConvertClaudeRequest` 直接 panic：
`baidu:29` `cloudflare:30` `cohere:28` `dify:35` `jina:30` `mistral:27` `mokaai:29` `palm:28` `tencent:36` `xai:32` `xunfei:28` `zhipu:28`

危害链：panic → `controller/relay.go:128` 的 recover → 包装为 `types.ErrorCodeBadResponse` → **该错误码可重试** → 跨渠道重试放大（每次重试都可能再次命中另一个 panic adaptor）。

其次，`main.go:190` 与 `middleware/recover.go:20` 会把 panic 原文回给客户端。

> 2026-10 落地（首个 PR）：11 处 live panic 改为 `errors.New("not implemented")`；`xai:32` 核实为已安全的注释桩（返回 `not available`），顺手删除残留注释行；`main.go` 与 `middleware/recover.go` 的客户端响应不再携带 panic 原文（服务端日志保留）。

### D3 · 9 个 handler 重复同一条上游发送链

`relay/*_handler.go` 各自手写：

```
Marshal → RemoveDisabledFields → ApplyParamOverride → NewOutboundJSONBody → DoRequest → 状态码检查 → RelayErrorHandler → ResetStatusCode
```

- `service.ResetStatusCode` 在 `relay/*.go` 中有 **25 个**调用点，每个自己读 `c.GetString("status_code_mapping")`
- 成功状态判断重复 9 次，且存在**三种不同接受规则**：`!= http.StatusOK`（8 处）、Replicate 201→200 特判（`image_handler.go:143`）、`>= 200 && < 300`（`alpha_search_handler.go:89`）
- `relay/gemini_handler.go:149` 存在未保护的 `resp.(*http.Response)` 类型断言（`httpResp` 已在 139 行取出）
- `relay/relay_task.go:360` 把上游原始 body 原样回给客户端，而文本路径是屏蔽的

### D4 · controller 绕过 service 直连 model，分层形同虚设

| 实测项 | 数值（2026-10 复核） |
|---|---|
| 生产 controller 文件 | 78 |
| **完全不 import `service`** | **37（47%）** |
| 直接引用 `model.DB` | 12 文件 |
| 裸 `model.DB.Transaction` | 8 处（均为真实写事务，perf 批次已清掉只读事务） |
| `model.*` 引用 vs `service.*` 引用 | 949 vs 356（2.7 : 1） |
| 裸 `c.JSON(...)` 与 `common.Api*` 并存 | 278 vs 874 |

最严重单点：`controller/subscription.go:281-314` —— handler 内手搓 34 行事务、构造 20 字段 `updateMap`。
`controller/channel.go:148` —— handler 自己拼 `model.DB.Model(&model.Channel{})` 并执行 `Count`/`Find`/`Group`。
`controller/plugin_protocol.go:367` —— 直接 `model.DB.Model(outcome.Task).Update(...)`。

`service/` 本身很干净（仅 3 处 `model.DB`，0 处 `DB.Begin`）—— **问题在 controller 越层，不在 service**。

另有两处真实重复规则：`hasEnabledMultiKey` 在 `controller/channel.go:90` 与 `model/channel.go:725` 各实现一次且签名不同；`SearchChannels` 在 `model.SearchChannels` 已查库后又做**内存过滤+分页**（`controller/channel.go:401-452`）。

### D5 · 全局状态锁死可测试性

- `model.DB`（`model/main.go:64`）、`model.LOG_DB`（`model/main.go:66`）为包级全局
- `common/` 共 **137** 个包级 `var`，其中 `common/constants.go` 独占 **93** 个
- `model/utils.go:34` `InitBatchUpdater` 与 `service/system_task.go:106` runner 是**无法停止**的后台 goroutine，无生命周期归属

后果实测：`controller/model_management_test.go` 中 `modelManagementDB` 是一个 **62 行**的全局 save/restore 仪式（保存 `model.DB`、`common.OptionMap`、DB 类型、`IsMasterNode`、8 个 ratio map、billing config…）。全仓 **0 个 mock**（`testify/mock`、`gomock`、`sqlmock`、`miniredis` 全部 0 引用），只有真 SQLite；因此**无法 `t.Parallel()`**。

### D6 · `relay/common/override.go` 单文件塞了 4 个不相关子系统

**2036 行**、93 个顶层声明：

1. 参数覆盖引擎（`ApplyParamOverride:150`、`applyOperations:861-1110` 独占 250 行、`pruneObjects:1928`、`mergeObjects:2147`）
2. header 覆盖引擎（`resolveHeaderOverrideValue:1238`、`parseHeaderPassThroughNames:1410`）
3. reasoning effort 同步（`syncReasoningEffortAfterParamOverride:218`）
4. 审计日志（`paramOverrideAuditRecorder:53`、`buildParamOverrideAuditLine:419`）

四者之间只有审计记录器一处耦合，完全可以独立。

### D7 · 前端巨型组件 + section 注册表强制 eager

- `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx` —— **4885 行**，31 个 `useCallback`、15 个 `useMemo`、346 个 `t()` 调用
- `param-override-editor-dialog.tsx` 3165 行、`advanced-custom-editor-dialog.tsx` 1762 行
- 生产 ts/tsx 共 1203 个，**15 个超过 1000 行**
- 11 个 `section-registry.tsx` 的 `build` 要求模块级 `ReactNode`，导致**打开任一 section 加载全部同级 section**（`system-settings/operations/section-registry.tsx:19-27` 一次拉 8 个约 4500 行）
- 8 个表格页重复 `useTableUrlState → filter 提取 → sort memo → useQuery → useDataTable → DataTablePage`，约 **2600 行**；其中 `columnFilters.find(...)?.value as string[] || []` 逐字重复 3 处

另：~~query key 只有 2 个工厂（`modelsQueryKeys`/`channelsQueryKeys`），却有 40+ 处内联字面量 + 约 50 处命令式 `invalidateQueries` —— 拼写错误会静默永不失效~~ **已治理（2026-10，4.5）**：新增 10 个按域工厂 + 1 个全局注册表，`queryKey:` 内联字面量从 163 处降至 18 处（剩余全部为单站点查询键，无失效配对，按判定标准保留）。

### D8 · 限流三套实现、三种失败策略

| 实现 | 位置 | 算法 |
|---|---|---|
| 内存 | `common/rate-limit.go` | 滑动窗口（Reserve/Complete） |
| Redis IP/用户 | `middleware/rate-limit.go:23-37` | 固定窗口 Lua |
| Redis 模型限流 | `middleware/model-rate-limit.go` | 滑动窗口 Lua 预留/释放 + 令牌桶 |

- 失败策略不一致：主限流 Redis 报错返回 500（fail-closed，`rate-limit.go:110-126`），邮件限流降级到内存（`email-verification-rate-limit.go:24-27`），模型限流 fail-open 并 `logger.LogError` 告警（与主限流降级契约一致）
- **已修（2026-10）**：模型限流旧的 `LLen` + `LIndex -1` 两次非原子往返已替换为单次 Lua「清理超窗 → 预留席位 → 判定」原子操作（`redisSuccessWindowScript`），预留即会计记录、失败 `LREM` 精确释放（席位值带随机 nonce），并发突发不再越过上限；崩溃泄漏由窗口 TTL 兜底。附 4 个回归测试（陈旧条目剪枝、拒绝路径零写入、同毫秒席位互不误伤、成功只记一次）
- `rateLimitFactory`（`rate-limit.go:148`）在**路由注册期**就固化后端选择，运行期切换 `RedisEnabled` 不生效
- 配置 Redis 与否，同一 IP 攻击者拿到的突发额度实质不同

### D9 · CI 门禁缺口（本台账的最高优先级前置项）

`.github/workflows/ci.yml` 现状：

- **前端 job 只有 `typecheck` + `test`** —— `.oxlintrc.json` 那 214 行规则和自研 `intl-locale` 插件**根本没在 CI 运行**
- 后端三数据库矩阵：已有 `TEST_MYSQL_DSN` / `TEST_POSTGRES_DSN` 各 **28 处**引用，但工作流**未提供这两个环境变量**，导致 **32 处 `t.Skip`** 静默跳过 MySQL 与 PostgreSQL
- 这意味着 `AGENTS.md` 强制要求的「三数据库实证验证」**在 CI 中实际从未执行**

> 2026-10 落地（首个 PR）：后端 job 接入 `mysql:8` + `postgres:15` services 并注入 loopback DSN（测试 helper 强制要求 loopback，见 `controller/access_token_audit_test.go:831`），32 处静默 skip 被激活。前端 job 接入 `format:check`、`copyright:check`、`i18n:sync` + `git diff` 漂移检查（`sync-i18n` 无 check 模式，`_reports/` 已 gitignore）。`lint` 全量门禁与 `knip` 延期，原因见本节末「门禁落地记录」。

### D10 · 一致性缺陷与冗余（低风险，可批量清理）

| 项 | 位置 | 问题 |
|---|---|---|
| 死代码 | `middleware/recover.go:12` | ~~`RelayPanicRecover()` 零调用点~~ **已删（2026-10）** |
| panic 泄漏 | `main.go:190`、`middleware/recover.go:20` | ~~panic 原文回给客户端~~ **已修（2026-10，服务端日志保留）** |
| 锁错误 | `setting/rate_limit.go:40` | ~~`UpdateModelRequestRateLimitGroupByJSONString` 在 `RLock` 下赋值 map~~ **已修为 `Lock`（2026-10）** |
| 冗余事务 | `model/user.go:405,444`、`model/topup.go:302,338,371,411` | ~~6 处只读分页查询开写事务~~ **已销账（perf 批次已改直查，本表行过时）** |
| 全局写锁 | `model/option.go:345` | ~~`updateOptionMap` 约 340 行 stringly-typed switch，每次刷新持全局写锁做全部赋值~~ **锁域已收窄（2026-10）：仅 `OptionMap[key]=value` 持锁，解析/分发/变量赋值移出临界区**；switch 本身保留（Phase 5.3） |
| 迁移静默失败 | `model/main.go:338-340` | ~~`migrateOptionPrimaryKey` 失败仅记日志不返回错误~~ **已改为 `return err`（2026-10）** |

### D11 · 依赖版本偏旧（评估后再定，非紧急）

`go.mod` 中：`gin v1.9.1`（2023）、`go-redis/v8 v8.11.5`（已被 v9 取代）、`gorm.io/gorm v1.25.12`。
gin 1.9.1 与 Go 1.26 存在版本落差。**升级属独立提案**，须按 `AGENTS.md` 跑完整三数据库矩阵验证，且不可与业务重构混在同一批次。

---

## 三、分阶段偿还计划

节奏：中批次重构，允许分阶段不兼容。每阶段独立可合并、可回滚。

### Phase 0 — 地基：装上护栏

> 依据 D9。当前任何 DB 相关改动都在无三库验证的状态下合入。

- [x] **0.1** `ci.yml` 后端 job 加 `services:` 起 mysql:8 + postgres:15（与 `docker-compose.dev.yml` 的 PG 大版本对齐），注入 `TEST_MYSQL_DSN` / `TEST_POSTGRES_DSN`，消除 32 处静默 skip
- [x] **0.2（门禁子集落地）** `ci.yml` 前端 job 加 `format:check`、`copyright:check`、`i18n:sync` + `git diff` 漂移检查。`lint` 全量与 `knip` 未进 CI，原因：
  - `lint` 在 main 上实测 164 errors / 65 warnings，`lint:fix` 后残留 60 errors（26 处 index-key、14 处 nested-ternary 需人工判断；2 处 iframe-sandbox 需产品决策，不能顺手修）；
  - `knip` 实测大面积红（多为 barrel/动态引用下的疑似误报，需逐项 triage）；
  - 两者作为独立前端清理批次立项，不阻塞护栏 PR。本 PR 内只修了 `import/no-cycle` 的 2 处真实循环（`search-provider` ↔ `command-menu`，把 `CommandMenu` 渲染点上移到唯一调用处 `authenticated-layout`，行为不变），并跑通全量 2216 个前端测试。
- [x] **0.3** 记录性能基线（2026-10，main，本地构建）：
  - 前端：`dist` 共 65MB；入口 `index.*.js` **5.05MB**（gzip 1.4MB）；最大 4 个 async chunk 分别为 6.8/5.4/5.1/3.2MB；JS 共 1633 个 chunk。入口 5MB 佐证 D7（section 注册表强制 eager + katex/markdown/vchart 进首屏包），是 Phase 4.2/4.3 要啃的硬骨头。
  - 后端单二进制 154MB（含内嵌前端 65MB）。
  - relay 首字节延迟与接口 P99 **本次未测**：需要可出网的上游渠道 key + 稳定负载，本地无条件伪造。测量方法固定为：`curl -w 'time_starttransfer' 打 /v1/chat/completions（非流）` + 响应头 `X-Oneapi-Request-Id` 对 `logs/` 全链路耗時；在有真实渠道的 staging 环境补测后回填本行。
- [x] **0.4** 为 D3 的发送链补行为黄金测试（`relay/convert_request_error_test.go`，零新文件）：
  - `TestClaudeHelperUnsupportedConversionIsClassifiedError`：Claude→zhipu 返回 `ErrorCodeConvertRequestFailed` + skip-retry + 500（锁定 1.2 修复后的分类；旧代码此处直接 panic，测试天然失败）。
  - `TestTextHelperAppliesStatusCodeMappingOnUpstreamError`：上游 429 经 `RelayErrorHandler` 映射 + `status_code_mapping` → 503（锁定组合链，为 1.3 的 `SendUpstream` 抽取护航）。
  - 勘误：台账初版称 panic 会被"跨渠道重试放大"不准确——`controller/relay.go:137` 在 recover 后会 re-panic，实际行为是直接 500 + 原文泄漏（已在 1.2 修掉）。

**验收**：CI 全绿；三库测试不再 skip；任一分支可复现同一基线。

### Phase 1 — relay 中转核心（最大杠杆）

- [x] **1.2【先做】** 修 D2：11 个 `ConvertClaudeRequest` 的 live `panic` 改为 `errors.New("not implemented")`（`xai` 核实已安全）；`main.go:190`、`middleware/recover.go:20` 不再回传 panic 原文；附带删除 `main.go` 因此变多余的 `fmt` import。`go build` + `go vet` + `relay/`、`middleware/` 测试全绿。
- [x] **1.1** 拆 D1（2026-10）：新增 `relay/channel/base_adaptor.go`（`BaseAdaptor` 空结构，提供 5 个经实测完全一致的默认方法：空 `Init` + 4 个 `not implemented` 转换桩）；34 个 adaptor 嵌入、`sub2api` 靠传递继承、`tencent/dispatch` 与 `task/jsplugin` 不动；删除 101 个样板方法（24 Init + 22 Audio + 13 Claude + 24 Gemini + 18 Image），`adaptor.go` 总计 7856→7542 行。接口本身**未拆**（见下），`go build ./...` + `go vet` + relay 全量测试全绿。
  - 范围依据（脚本聚类 36 文件全部方法体，禁止目测）：`ConvertRerankRequest` 有 3 种-trivial 形态（19×`nil,nil` vs 5×`request,nil`，下游 `Marshal(nil)` 行为不同，不可统一）；`ConvertEmbeddingRequest` / `ConvertOpenAIResponsesRequest` 各有 2 种形态；`GetChannelName`/`GetModelList` 是各包私有符号（34/31 处各 1 行，保留）；`ConvertOpenAIRequest` / `GetRequestURL`（仅 3 处完全一致）/ `SetupRequestHeader` / `DoRequest` / `DoResponse` 保持编译器强制显式实现——静默继承 OpenAI 形态 URL 或空 auth 头是 footgun。
  - 能力接口的真拆分留到 1.3（handlers 重写时自然引入）；`tencent` 嵌接口 / `sub2api` 嵌结构 / `newapi` 按字段组合三种复用写法均为有效代码，本轮不动。
- [x] **1.3** 拆 D3（2026-10）：新增 `relay/send_upstream.go`（`SendUpstream` + `AcceptStatusOK`/`AcceptStatus2xx` 策略），9 文件 10 处调用点收敛；`gemini_handler.go:149` 的裸断言随之消除（改传已定型 `httpResp`，nil 安全）；`DoRequest` 返回类型**未改**（`any`，动接口签名会波及全部 adaptor，收益不抵风险）；`image_handler`（Replicate 201→200 改写）与 `websocket.go`（WSS 断言 + 非 OpenAI 错误包）保持 bespoke 并有注释说明；`relay_task.go:360` 上游 body 原样回传问题**未动**（任务错误包与文本错误包是两套类型体系，需单独立项）。
  - 行为差异清单（逐项核对）：裸断言变分类 500（原地 panic，不可达路径）；`alpha_search` 的 nil-resp 守卫保留（否则 `defer Body.Close()` 空指针）；`gemini` 两处 `Do gemini request failed` 日志行删除（`channel.DoApiRequest` 已在 `api_request.go:565` 统一记录，零诊断损失）；`IsStream` 嗅探留在各调用点（`GeminiEmbeddingHandler` 本来就没有，加进去反而是行为变更）。
  - 新增 `TestSendUpstreamPolicies`（4 子用例：429 双策略映射、201 双策略分界），0.4 的两个 handler 级黄金测试同步验证抽取无损。
- [x] **1.4** 收敛结算派发（2026-10）：新增 `relay/settle_usage.go`（`SettleUsage`），收敛 3 处**完全一致**的 audio-token 判定（compatible×2、audio×1；含类型断言次数 3→1 的顺带收敛）；`compatible_handler.go` 因此去掉 `ratio_setting` import。**有意未动** 9 处直调 `PostTextConsumeQuota` 的站点（embedding/rerank/image/gemini/claude/alpha）：把 audio-token 响应路由到 `PostAudioConsumeQuota` 会改变结算金额，属 Phase 2 计费 gate 范畴，不是机械抽取。`PostWssConsumeQuota` 与 `ConsumeResponsesQuota` 是独立路径，保持。
- [x] **1.5** 拆 D6：2247 行 `override.go`（实测 2247 行，非初版估计的 2036）→ `param_override.go`（50 声明）/ `header_override.go`（26）/ `reasoning_effort_sync.go`（4）/ `param_override_audit.go`（8）。Python 按顶层声明切分，同包纯搬运；符号集 88 vs 88 零差异（字节级对照）；`relay/common` 全量测试（含 2443 行 `override_test.go`）全绿。
- [x] **1.6** `RelayInfo` 减负（2026-10）：`relay_info.go`（实测 1273 行）剥离出 `task_info.go`（4 类型 + 4 方法 + 顺手归位的 `FailTaskInfo` 构造器）与 `body_fields.go`（3 函数）；`relay_info.go` -275 行。本体 struct 不动。符号集 65 vs 65 零差异（字节级对照）；附带清掉因此多余的 `encoding/json`/`strconv` import。全绿。
  - 勘误：初版引用的 "1115 行"有误，文件实测 1273 行；1001-1272 的声明行号本身正确。

**验收**：`GOWORK=off go build ./...` + 54 个 relay 测试全绿；`adaptor.go` 总体行数下降 > 50%。

### Phase 2 — 计费与结算（最高风险）

> **强制前置**：本阶段任何改动前必须完整读取 `.agents/rules/billing.md`。

- [x] **2.1**（2026-10，有意收敛而非强行统一）：抽取 `resolveTieredUsedVars`（`service/tiered_settle.go`），消除 text/audio 两处逐字重复的 snapshot→usedVars 块；新增 `TestResolveTieredUsedVars` 回归。**不碰**的差异及理由（均属计费语义，需领域确认，非机械重复）：
  - WSS 传裸 P/C/Len 且无 usedVars：`RealtimeUsage` 无 cache/image/audio 明细可归一化，现状正确；
  - audio 硬编码非 Claude 语义、text 有 fixed-price nil 合成与 tool-surcharge 合成：各路径用量形态不同；
  - task polling 走 usage-facts + 差额结算（`EvaluateTaskCompletionUsage`），与 token-params 是两个域。
  - 强行统一四者会改结算金额，违反 billing.md 的 defense-in-depth；待领域确认后再议。
- [x] **2.2**（2026-10，实证后决定**不加超时、无代码变更**）：
  - expr v1.17.8 的 `vm.Run` 无 context 支持（`vm` 包零 `context` 引用），任何 wall-clock 超时只能用 goroutine+select 实现——超时后 VM 照跑（泄漏），比病本身更糟，故否决。
  - 实测（临时 benchmark，事后删除）：1 探针×100KB ≈ 8µs；5 探针×1MB ≈ 0.8ms；5 探针×10MB ≈ 9.7ms，随体量线性增长但绝对值小。曾尝试单次 `ParseBytes` 复用，反而慢 2-8 倍——gjson `GetBytes` 是惰性路径扫描，`ParseBytes` 是全量 DOM 物化（128MB 体会产生数百 MB 瞬时分配），已回滚（`run.go` 与 HEAD 字节一致）。
  - 现有约束已够：VM `MemoryBudget=1e6` + AST `MaxNodes=1e4` 卡死表达式复杂度；`MaxRequestBodyMB`（默认 128MB，管理员可调小）卡死输入规模。结论：记录边界，不写代码。
- [x] **2.3**（2026-10）：新增 `pkg/billingexpr/lru_cache.go`（stdlib `container/list`，无新依赖；`pkg/cachex` 是 Redis 混合缓存，工具不对），`compile.go` 5 处 access 收敛到 `get/add/clear`；单 mutex 替代 RWMutex（临界区均为微秒级 map/list 操作，无读写分离价值）。白盒 `lru_cache_test.go`（内部包，直接断言淘汰序；外部黑盒测不出透明 miss）+ 全量 suite 全绿。`-race` 本机无 CGO 跑不了，已记录。
- [x] **2.4**（2026-10，评估后决定**不搬**）：实测 `skipRetry`/`recordErrorLog` 在 relaykit 内**零消费**（仅 `types/error.go` 自身的字段+option+谓词），168 处宿主调用全是分类器打标，唯一策略点 `service.DecideRelayRetry`（`service/relay_error.go:20`）本就在宿主侧；且 `error.go` 零宿主 import，模块独立性完好。搬移=168 站点 churn + relaykit 字段导出（封装降级），零功能收益。结论：flag 是错误元数据、决策已在宿主侧，现状成立，不改。

**明确不动的计费不变量**：`MaxQuota=int32` 的批量安全论证、`WalletFunding.Refund` 的非幂等性、订阅被排除在 trust-bypass 外的理由、`image_handler.go:103` 的二次预扣（override 可能改 `n`）、`ForcePreConsume` 语义。

**验收**：现有 24 个 service 测试（含 billing 4 文件 4825 行）全绿；新增四路径对称性表驱动测试；三库矩阵下额度账实一致。

### Phase 3 — controller→model 越层治理

> 策略：加法式收口，不做 repository 大爆炸（DB 抽象留到 Phase 5）。

- [x] **3.1**（2026-10）：`ci.yml` 新增 `layering-guard` job，对 PR 新增行做 diff 检查：`controller/*.go`（不含测试）新增 `model.DB`/`model.LOG_DB` 引用或 `.Transaction(` 即失败。 Grandfather 存量 28+8 处；正负矩阵实测（`[(]`/`[.]` 写法，避开某 grep 构建不支持 `\(` 的坑）；`subscription.go` 全文件模拟检出全部 5 处、`relay.go` 零误报。`c.JSON` 有意不进自动化门禁（relay 信封/download/SSE 有正当用途），留作书面约定。
- [ ] **3.2** 40 个越层文件按业务域分组下沉到 `service/`（2026-10 slice-1 落地：`subscription.go` 的 3 条写路径——`model.DB.Transaction` 手搓 updateMap、`Update("enabled")`、`DB.Create`——下沉为 `model.Create/Update/UpdateStatusSubscriptionPlan` + `service/subscription_plan.go` 薄封装；controller 只保留校验/响应/缓存失效。附带发现并利用：单条 UPDATE 原子写入，277 行起的显式 Transaction 是多余的，已删（行为等价）；`updated_at` 必须显式进 map（`BeforeUpdate` 只 stamp model 不进 map）。新增 `model/subscription_plan_test.go` roundtrip（零值写入 + nil 指针保持 + 字段漂移守卫）。待：`plugin_protocol.go:367` 等 38 文件）
- slice-2（2026-10）：`channel.go` 的查询构造（`buildChannelListQuery` + `applyChannelStatusFilter`，7 处调用）下沉为 `model.NewChannelListQuery`（与既有的 `model.ApplyChannelGroupFilter` 归位）；执行编排（Count/Find/分页）暂留 handler（tag-mode 的 N+1 循环搬家需单独设计）。`hasEnabledMultiKey` 双实现收敛为 `(*model.Channel).HasEnabledMultiKey` 方法（逐分支比对等价：nil/非多键/空键/无状态表全一致），controller 2 处调用点切换。`processChannelError` 纯透传 wrapper 内联到 3 处调用点后删除。
- [x] **3.3 slice-1**（2026-10）：响应统一先啃无争议子集——脚本扫描 `c.JSON(200, {success,message,data})` 三键精确形态（与 `ApiSuccess` 字节等价，map 序列化键有序），81 处跨 26 文件转 `common.ApiSuccess`，附带清 8 个文件的多余 `net/http` import。`go build/vet/gofmt` 全绿；controller 全量对照干净树：失败集完全一致（105 vs 105，dirty-only 回归为零）。
  - 范围纪律：额外键（`time`/`error_code`）、缺 `data` 键、非 200 状态、字面量 message（需 i18n key + 七个前端 locale 文件，非机械可定）全部保留；`c.JSON(200,...)` 数字字面量形态、`ShouldBindJSON` vs `DecodeJson` 的绑定统一留待按文件迁移时逐个处理。
  - 环境注：controller 全量在 Windows 本机本就 105 失败（SQLite 文件锁，干净树复现），与改动无关；看门禁以 CI（Linux）为准。
- [x] **3.4**（2026-10）：`UpdateSelf`（145 行）拆为路由内分派 + `UpdateSelfRequest` 类型化 DTO + 三个命名函数（`updateSelfPassword` / `updateSelfSetting` 闭包复用 / `updateSelfProfile`），消灭 map→marshal→unmarshal 回环。**有意不拆三个路由**：前端存在 profile+password 混合单请求（`acceptAuthRotation: Boolean(data.password)`），拆路由会逼出多调用+proof 杂耍并破坏第三方兼容；安全语义（step-up proof、session rotation）原位保留。已核对的边缘：`password:null/""` 视为未请求、`password:123` 解码 400（与旧 remarshal 失败同码）、null 设置键落 profile 空操作（struct-update 跳零值，无 wipe 风险）、超长 email/remark 不再 400（本就丢弃的字段）。新增 `controller/update_self_test.go` 分发表（5 用例）+ 既有 5 个安全测试断言全绿（仅 Windows 文件锁清理失败）。
- [x] **3.5**（2026-10）：普查后采用**薄封装而非搬函数**——记账逻辑与事务同处 model 是良好内聚，真正要修的只是调用方向。新增 `service/payment_fulfillment.go`（6 个透传：4×Recharge + Purchase/Complete），7 文件 10 处 controller 调用改道；`TryReserveUserQuota/TokenQuota` 核实早已在 service 内部调用，无需动。合规：billing.md 已在 Phase 2 通读，本次只改调用路径、零算术变更；model 钱测试（exactly-once/redis-DB 对账/溢出上限等 10 项）全绿。
- [ ] **3.6** 消除 D4 列出的两处真实重复规则

**验收**：controller 中 `model.DB` 直调归零；不 import service 的文件 40 → 个位数。

### Phase 4 — 前端 web/

- [x] **4.1**（2026-10）：删 7 个零引用 `src/components/ui/` 组件（复核全仓无引用者）；`chart.tsx` 带走 `recharts`，`resizable.tsx` 带走 `react-resizable-panels`（同样零引用，顺手）；`bun install --frozen-lockfile` 通过。删 13 处预存重复 AGPL 头（脚本断言每文件恰好 2→1）。盲区处理**有取舍**：`knip.config.ts` 的 `ui/**` 忽略已删（当即照出 `breadcrumb`/`carousel` 等此前不可见项）；`.oxlintrc.json` 的忽略**保留**——实测去掉后冒出 14 个 vendored-shadcn 自有报错（only-export-components 等），修它们等于 fork 上游组件，得不偿失，已注释说明。
  - 过程教训：删依赖后首次 `typecheck` 报出某测试文件满屏语法错误，实为 tsgo 增量缓存幻觉（文件本身合法），删 `node_modules/.tmp/tsconfig.app.tsbuildinfo` 后从零重查全绿；`bun run format` 顺手修了本次改动的 `package.json` 缩进（tab 误用已纠正）。另：`format:check` 曾在 0.2 显示绿、本轮短暂飘红，系 oxfmt 并行/增量状态波动（前后两次运行文件集一致），以 `bun run format` 为准归零，现绿。
- [x] **4.2**（2026-10）section registry 支持 `lazy()`，拆掉 11 处的强制 eager；11 个 `$section.tsx` 收敛到类型化 helper（`guardSectionParam` / `redirectToDefaultSection`）。落地记录见 `docs/phase-4.2-section-registry.md`：8 个 system-settings registry 已 lazy，22 个路由收敛（-200/+138），3 个占位型 registry 保留（内容在 page 直 render）。
- [ ] **4.3** 拆 `channel-mutate-drawer.tsx`（实测 5092 行）—— `drawers/sections/` 已有 7 文件 295 行的半成品模式可循
- [ ] **4.4** 抽 `useServerDataTable` 组合 hook，消除 8 个表格页约 2600 行重复
- [x] **4.5**（2026-10）query key 工厂化：新增 `src/lib/query-keys.ts`（跨域全局键）+ 10 个特性域 `lib/query-keys.ts`，扩展 `modelsQueryKeys`/`channelsQueryKeys`；163 处内联字面量迁移 145 处，剩余 18 处均为单站点查询键（无 query↔invalidate 配对，按判定标准保留，含 3 个表格列表键与 15 个一次性静态/参数键）。工厂产出键结构与原字面量逐字一致（`['prefill_groups','model']` 的 snake_case、`['channels', id, provider, 'inference-status']` 等特殊形态均原样保留）。**判定标准**：迁移所有 ≥2 生命周期站点（query/invalidate/refetch/remove/cancel/isFetching/setQueryData）或同族多形态的键；单站点键无「拼写错误静默永不失效」风险，保留字面量。
- [ ] **4.6** 约 90 处 Intl locale 接线：现有 lint 只拦**不安全** locale，不拦**缺失** locale，故 `ai-elements/context.tsx` 的 10 处硬编码 `'en-US'` 等静默通过且不随语言切换重排
- [ ] **4.7** 补 E2E 与覆盖率工具链 —— `web/AGENTS.md:193` 要求 MSW/Playwright 与 80% 覆盖率，当前两者都不存在、无 coverage provider；`routes/oauth/$provider.tsx`（261 行，鉴权关键）零测试

### Phase 5 — 全局状态与可测试性（最底层，必须最后）

- [ ] **5.1** 引入 `model.Store` 接口，采用**加法式迁移**（新接口版本与旧全局版本并存，按域迁移），不要一次性替换 `model.DB`
- [ ] **5.2** 收敛 `common/` 的 137 个包级变量：先区分「真可变状态」与「启动期常量」，后者移入不可变结构体
- [ ] **5.3** 修 `model/option.go:345` 的 340 行 switch 与全局写锁 —— `setting/config` 的反射机制已是现成替代品
- [ ] **5.4** 为 `InitBatchUpdater` 与 system task runner 加 shutdown handle
- [ ] **5.5** 修 D10 表中全部条目

---

## 四、依赖关系

```
Phase 0 地基 ──┬──> Phase 1 relay 核心 ──> Phase 2 计费（须在 1.4 之后）
               ├──> Phase 3 越层治理 ──> Phase 5 全局状态
               └──> Phase 4 前端（与后端仅通过 API 契约耦合，可完全并行）
```

- Phase 1 与 Phase 4 **无依赖，可并行**
- Phase 2 必须在 1.4（结算派发收敛）之后
- Phase 5 必须最后 —— 它波及所有层的函数签名

## 五、状态总览

| 阶段 | 项数 | 已完成 | 状态 |
|---|---|---|---|
| Phase 0 地基 | 4 | 0.1✅；0.2 门禁子集✅；0.3✅（前端 65MB/入口 5MB/后端 154MB；延迟待 staging 回填）；0.4✅（2 黄金测试 + `TestSendUpstreamPolicies`） | ✅ 完成 |
| Phase 1 relay 核心 | 6 | 1.1✅（BaseAdaptor，101 桩删除）；1.2✅；1.3✅（SendUpstream，9 文件 10 站点；image/websocket 除外）；1.4✅（SettleUsage，3 判定点；9 直调点有意不动）；1.5✅（override 4 文件）；1.6✅（task_info/body_fields） | ✅ 完成 |
| Phase 2 计费结算 | 4 | 2.1✅（usedVars 收敛+文档化分歧）；2.2✅（实证无代码）；2.3✅（LRU）；2.4✅（评估不搬） | ✅ 完成 |
| Phase 3 越层治理 | 6 | 3.1✅（CI 门禁）；3.2 slice-1✅（subscription 写路径）/slice-2✅（channel 查询构造）；3.3✅；3.4✅；3.5✅；3.6 部分✅（hasEnabledMultiKey、processChannelError；SearchChannels 内存分页需过滤下沉设计，延期） | 进行中（controller 直接 `model.DB` 引用 12→5 生产文件；见 §八） |
| Phase 4 前端 | 7 | 4.1✅（删 7 死组件+去 2 依赖、knip 去盲区、13 重复头清零）；4.2✅（section registry lazy + 22 路由收敛）；4.5✅（163→18 内联 query key） | 进行中（4.3/4.4/4.6/4.7 未动） |
| Phase 5 全局状态 | 5 | 0 | 未开始 |

**建议的第一个 PR**：Phase 0.1 + 0.2 + 1.2 合并。
三数据库 CI、前端 lint/knip 门禁、12 个 panic 桩修复 —— 零行为变更、立刻消除真实缺陷、
且给后续所有重构装上护栏。

> 2026-10 落地状态：0.1✅、0.2 部分✅、1.2✅（11 live + 1 已安全）。`lint` 全量门禁与 `knip` 因主干本就飘红而延期，
> 不是本 PR 引入。验证矩阵见「六、首个 PR 落地记录」。

## 六、首个 PR 落地记录（2026-10）

本节记录执行中发现的、影响后续工作的实证结论。

### 验证矩阵

| 项 | 结果 |
|---|---|
| `GOWORK=off go build ./relay/... ./middleware/ .` | ✅ |
| `go vet`（11 个 adaptor 包 + `middleware/`） | ✅ |
| `gofmt -l`（全部改动 Go 文件） | ✅ 无输出 |
| `go test ./relay/ ./middleware/` | ✅ |
| `go test ./relay/...` | ⚠️ 2 个 HTTP2 GOAWAY 用例在全量并行下失败，见下 |
| 前端 `typecheck` | ✅（修复 `--fix` 引入的 7 处，见下） |
| 前端 `vitest run` 全量 | ✅ 183 文件 / 2216 用例 |
| 前端 `format:check` / `copyright:check` | ✅（后者先补了约 20 处缺失头） |
| 前端 `i18n:sync` + `git diff` | ✅ 零漂移，门禁可行 |

### 预存失败（与本 PR 无关，已在干净树复现）

`TestUpstreamGetBody_HTTP2RetryAfterGracefulGoAway_PassThrough` 与
`TestUpstreamGetBody_HTTP2CannotRetryWithoutGetBody`（`relay/channel/api_request_getbody_test.go:547,585`）
在 Windows 本机全量并行跑时以 `wsarecv` 失败，单跑（干净树与改动树）均通过，
干净树全量跑同样失败。结论：HTTP/2 GOAWAY 时序用例对本机负载敏感，与 adaptor 改动无关。
后续若在 CI（Linux）同样飘红，再单独立项修时序 margin。

### 工具陷阱（后续清理必读）

1. **`oxlint --fix` 会引入类型错误**，不可盲跑。本次实测引入 7 处：
   `prefer-set-has` 把 `RankingPeriod[]` 改成 `new Set` 却保留数组类型声明；
   3 处 `prefer-at` 把 `lines[len-1]` 改成 `lines.at(-1)`（返回 `T|undefined`）；
   `tag-input` 的 `.at(-1)` 传入 `string` 参数；
   `prompt-input` 对不可迭代的 `SpeechRecognitionResultList` 用 spread。
   修复策略：Set 补 `ReadonlySet` 类型声明；`.at()` 加 `?.length ?? 0`；
   不可迭代处回退 `Array.from` + 带理由的 `eslint-disable-next-line`。
   **每次跑 `--fix` 后必须重跑 `typecheck`**，以此为铁律。
2. **`add-copyright.mjs` 看不见文件中间的头**。它只处理顶部的缺失，
   对已有中间头的 5 个文件又在顶部加了一份造成重复，需手工删除中间块。
   预存的 13 处重复（`data-table/layout/*` 等，未被本 PR 触碰）保持不动，
   等 Phase 4 动到那些文件时再顺手清。
3. **Windows 下 `bun install` 偶发 EPERM**（cache 目录 move 失败），重试即过；
   非依赖问题，不要因此换安装方式。
4. PowerShell 5.1 不支持 `VAR=val cmd` 前缀写法，Go 命令须用 `$env:GOWORK='off'`；
   `Select-String -Path <目录>` 不递归，查代码用 `Get-ChildItem -Recurse | Select-String`
   或 grep 工具。
## 七、P0 收尾批次落地记录（2026-10-20）

范围：D8 模型限流并发修复、D10 剩余两项（`option.go` 锁域、`migrateOptionPrimaryKey` 错误传播）、4.5 query key 工厂化。

### 7.1 改动清单

| 项 | 文件 | 内容 |
|---|---|---|
| D8 | `middleware/model-rate-limit.go` | `checkRedisRateLimit` → `reserveRedisSuccessSlot` + `releaseRedisSuccessSlot`；新增 `redisSuccessWindowScript`（清理超窗 → `LLEN<max` 预留 → 判定，单次 Lua 原子）；席位值 `ts:nonce` 保证 `LREM` 精确释放；fail-open 路径保留「成功后补记」降级会计 |
| D8 测试 | `middleware/model_rate_limit_test.go` | 4 个新用例：陈旧条目剪枝、预留/释放往返与满员拒绝、成功只记一次、UTC 拒绝路径零写入 |
| D10 | `model/option.go` | `updateOptionMap` 锁域从「全函数」收窄到「`OptionMap[key]=value` 一行」；解析/`handleConfigUpdate`/包变量赋值移出临界区 |
| D10 | `model/main.go` | `migrateOptionPrimaryKey` 失败改为 `return err`（PK 完整性是计费类选项加载前提） |
| 缺陷修复 | `controller/audit.go` / `controller/redemption.go` / `controller/redemption_batch_test.go` | `DeleteRedemptionBatch` 无效请求路径补记失败审计（新增 `recordManageAuditFailure`）；测试由已退役 legacy access token 切换为 `createAccessTokenTestSession` 会话 JWT，并将 `UserSession` 纳入 AutoMigrate |
| 4.5 | `src/lib/query-keys.ts`（新）+ 10 个特性域 `lib/query-keys.ts`（新）+ `models`/`channels` 工厂扩展 | 163 处内联 `queryKey:` 迁移 145 处；剩余 18 处单站点键按判定标准保留 |

### 7.2 验证矩阵

| 项 | 结果 |
|---|---|
| `go build ./...` | ✅ |
| `go vet ./middleware/ ./model/ ./controller/` | ✅ |
| `gofmt -l`（7 个改动 Go 文件） | ✅ 无输出 |
| `go test ./...`（SQLite 内存 + MySQL 8.4 + PostgreSQL 15，`GOWORK=off`） | 见 7.3 |
| 前端 `typecheck` | ✅ |
| 前端 `vitest run` 全量 | ✅ 188 文件 / 2240 用例 |
| 前端 `oxlint`（86 个改动文件） | ✅ 0 warnings / 0 errors |
| 前端 `format:check` / `copyright:check` | ✅ |
| 前端 `build` + `check:budget` | 见 7.3 |

### 7.3 验证结果（已回填）

- `go test ./...`（`GOWORK=off` + `TEST_MYSQL_DSN`/`TEST_POSTGRES_DSN`）：43 个包 `ok`。三库矩阵实证：`TestPreConsumePolicyDatabaseMatrix` 的 mysql / postgres 分支各自创建隔离库并 PASS（`isolated database: newapi_audit_* (mysql|postgres)`）。
- 2 个非本批失败，均已单独归因：
  - 根包 `[setup failed]`：与前端 `bun run build` 并行执行，rsbuild 重建 `web/dist` 期间 `index.html` 瞬时缺失，触发 embed 编译失败；串行复跑正常（环境竞态，非代码问题）。
  - `controller` 包：并行全量跑 44 包时 10 分钟超时（Windows 上 SQLite 文件锁 + 多 DB 池资源竞争，超时点 `TestPasskeyRPIDRotation...` 单独跑 3.3s 通过）属环境限制，非代码问题。
- 缺陷修复回填（`TestDeleteRedemptionBatch`）：干净树复现失败（三方言一致），根因有二 —— (a) `DeleteRedemptionBatch` 无效请求路径未写审计（`db5b26247` 引入的缺口）；(b) 测试使用已退役 legacy access token 直接认证 → 401，请求未达 handler。修复后：handler 在 `ShouldBindJSON` 失败分支补记 `recordManageAuditFailure`；测试改用 `createAccessTokenTestSession` 会话 JWT 并将 `UserSession` 纳入 AutoMigrate。三方言验证 PASS（`TestDeleteRedemptionBatch/sqlite|mysql|postgres`）；审计相关子集回归（`-run 'TestDeleteRedemptionBatch|Audit|Redemption'`）71s 全绿。
- 逐提交可编译：`cf423afa2`（middleware）在独立 worktree 中 `go build ./middleware/ ./model/` 通过；`5714aabae`（model）与最终树同构建；`3e91c756b`（web）不影响 Go 构建。
- 前端 `build` + `check:budget`：初始 JS 232.9KB / CSS 60.0KB gz（预算 245KB / 65KB），PASS。
- 已知边界：模型限流席位在进程崩溃时最多滞留一个窗口 TTL（设计内，注释已说明）；`updateOptionMap` 的 340 行 switch 未动（Phase 5.3 范围）。
- 提交：`cf423afa2`（D8）、`5714aabae`（D10）、`3e91c756b`（4.5）。

## 八、Phase 3.2 越层下沉批次（2026-10-20 续）

策略与 3.2 一脉相承：把 controller 里裸 `model.DB`/`model.LOG_DB`/`.Transaction(` 调用下沉。按域逐个下沉，每个提交都复核 `gofmt`/`go build`/`go vet` + 相关子集测试 + layering 扫描全绿后才提交。

### 8.1 改动清单（按提交）

| 提交 | 域 | 内容 |
|---|---|---|
| `c92e566e1` | 模型元数据 | `model.GetModelMetadata`/`UpdateModelStatus`；新 `service/model_metadata.go` seam（含 `GetMetadataSyncState`，由 service 供 `model.DB`）；`model_meta.go`/`model_sync.go` 改道 |
| `37b02bdf9` | channel 上游模型更新 | 新 `service/channel_upstream_scan.go`（`ListEnabledChannelsForUpstreamUpdate`/`CountEnabledChannels`/`UpdateChannelUpstreamModelSettings`）；扫描循环内联查询收敛进 `findEnabledChannelsAfterID`（二者字节等价，DRY） |
| `c73514fcf` | channel 数据访问 | `model.UpdateChannelKey`/`GetDisabledChannelIDs`；`codex_usage.go`/`channel.go` 改道 |
| `89c96a91b` | 任务 | `(*model.Task).UpdatePrivateData` 方法；`plugin_protocol.go` 背景标志写改道（台账点名的 367 行） |
| `fda022900` | 订阅计划 | `model.ListSubscriptionPlans`/`ListEnabledSubscriptionPlans` + service 包装；`subscription.go` 两个列表读改道 |

### 8.2 进度

- controller 生产文件直接 `model.DB`/`model.LOG_DB`/`.Transaction(` 引用：**12 → 5**（`channel-test.go`、`oauth.go`、`setup.go`、`user.go`、`wechat.go`）。
- 剩余 5 文件均涉及事务或根账号创建：`oauth`/`wechat`/`user` 各有 `DB.Transaction`，`setup` 建 root user + Setup 记录，`channel-test.go` 做 root user 查找——需逐事务核对「哪些步骤必须同事务、哪些抽离后仍是原子」后再下沉，单列下一批。
- 验收口径拆分：`model.DB` 直调归零按上面的 12→5 推进（可机械扫描复核）；「不 import service 的 40 文件 → 个位数」是独立口径，本次给 `model_meta.go`/`model_sync.go`/`subscription.go` 等补上 service 依赖，纯数据访问的单列下沉（codex key、task private_data）按既有 `model.DeleteDisabledChannel()` 惯例直连 model，不强制加 service 层。

### 8.3 验证

- 每提交 `gofmt -l` 无输出、`go build`/`go vet ./controller/ ./model/ ./service/` 零告警。
- 子集回归（三方言矩阵，DSN 缺省走 sqlite 分支）：metadata `TestModelManagementDatabaseMatrix|TestMetadataSync…|TestModelDeletion…` 14.8s✅；channel `Upstream|AdvancedCustom` 7.5s✅；`Channel|Codex` 22.3s✅；`Plugin|Subscription` 8.1s✅。
- layering 扫描（`rg 'model\.(DB|LOG_DB)|\.Transaction\(' controller/ --glob '!*_test.go'`）逐提交复核，移除项逐条确认。
- **全量 controller 回归（回填 2026-10-21）**：`go test ./controller/ -count=1 -timeout 40m` + 三方言 DSN（mysql 8.4.11 @127.0.0.1:13306 / pg 15.18 @127.0.0.1:15432，凭据取自 `newapi-test-*` 容器）—— **1179.155s 全绿、零失败（EXIT=0）**。三方言实证执行：`TestModelManagementDatabaseMatrix` 的 sqlite/mysql/postgres 三分支各自建隔离库并 PASS（`isolated database: newapi_audit_* (mysql|postgres)`）。结论与 3.2 既有记录闭环——单包串行此前唯一失败即 `TestDeleteRedemptionBatch`，`bf3784977` 修复后全量归零。
- 注：三方言矩阵由 `TEST_MYSQL_DSN`/`TEST_POSTGRES_DSN` 环境变量驱动，**未设置时 mysql/postgres 子测试静默 skip**；此前若干“子集通过”实为仅 sqlite 执行，全量口径须显式带 DSN。
- 全 controller 全量仍受 Windows SQLite 文件锁/多 DB 池影响，看门禁以 CI（Linux）为准（同 3.3 既有结论）。
