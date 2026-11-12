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
- [x] **3.6** 消除 D4 列出的两处真实重复规则（2026-11-12 结项）
  - SearchChannels 内存分页设计要点（2026-10-21 只读分析）：handler 两支数据源——tag 模式「按 tag 分批查库 + 按原 tag 序内存重组」与普通模式「`model.SearchChannels` 单次查库」——汇入同一套「status 过滤 → type 统计 → type 过滤 → 内存分页」。`model.SearchChannels` 不带 LIMIT（返回全量匹配集），故内存分页是对全量切片。三个过滤条件（status/type/分页）均为纯列谓词、可下沉 SQL，但 tag 模式的多源特性决定：要么先把两支统一为「取全量候选 → 统一过滤/分页」管道，要么接受 tag 支维持内存分页。属需专门设计的项，本批未擅动。
  - **结项实现（`6866ca61e`）**：采用「普通模式完整下推 + tag 模式保留重组但下推过滤」的取舍。新增 `model/channel_search.go`：`NewChannelSearchQuery`（可组合查询，含与旧实现逐字相同的搜索谓词 + group/status/type 过滤 + `Omit("key")`）、`ChannelTypeCounts`（GROUP BY type）；方言列名自行判定，不再依赖 `initCol()` 启动顺序。普通模式改为 SQL `Count` + `Group(type)` + `Limit/Offset`，只加载当前页；tag 模式因「tag 顺序来自排序子查询、跨三方言无法用可移植 ORDER BY 表达」保留重组与内存分页，但 status/type 过滤下推以减少加载。响应 `items`/`total`/`type_counts` 语义保真，特别是 `type_counts` 基于「不含 type 过滤」的查询（与 `GetAllChannels` 一致）。`model.SearchChannels` 改为委托共享查询构造器，`SearchTags` 复用同一方言列名 helper，消除两套谓词。详见 §8.6。

**验收**：controller 中 `model.DB` 直调归零；不 import service 的文件 40 → 个位数。

### Phase 4 — 前端 web/

- [x] **4.1**（2026-10）：删 7 个零引用 `src/components/ui/` 组件（复核全仓无引用者）；`chart.tsx` 带走 `recharts`，`resizable.tsx` 带走 `react-resizable-panels`（同样零引用，顺手）；`bun install --frozen-lockfile` 通过。删 13 处预存重复 AGPL 头（脚本断言每文件恰好 2→1）。盲区处理**有取舍**：`knip.config.ts` 的 `ui/**` 忽略已删（当即照出 `breadcrumb`/`carousel` 等此前不可见项）；`.oxlintrc.json` 的忽略**保留**——实测去掉后冒出 14 个 vendored-shadcn 自有报错（only-export-components 等），修它们等于 fork 上游组件，得不偿失，已注释说明。
  - 过程教训：删依赖后首次 `typecheck` 报出某测试文件满屏语法错误，实为 tsgo 增量缓存幻觉（文件本身合法），删 `node_modules/.tmp/tsconfig.app.tsbuildinfo` 后从零重查全绿；`bun run format` 顺手修了本次改动的 `package.json` 缩进（tab 误用已纠正）。另：`format:check` 曾在 0.2 显示绿、本轮短暂飘红，系 oxfmt 并行/增量状态波动（前后两次运行文件集一致），以 `bun run format` 为准归零，现绿。
- [x] **4.2**（2026-10）section registry 支持 `lazy()`，拆掉 11 处的强制 eager；11 个 `$section.tsx` 收敛到类型化 helper（`guardSectionParam` / `redirectToDefaultSection`）。落地记录见 `docs/phase-4.2-section-registry.md`：8 个 system-settings registry 已 lazy，22 个路由收敛（-200/+138），3 个占位型 registry 保留（内容在 page 直 render）。
- [ ] **4.3** 拆 `channel-mutate-drawer.tsx`（实测 5092 行）—— `drawers/sections/` 已有 7 文件 295 行的半成品模式可循
- [ ] **4.4** 抽 `useServerDataTable` 组合 hook，消除 8 个表格页约 2600 行重复
- [x] **4.5**（2026-10）query key 工厂化：新增 `src/lib/query-keys.ts`（跨域全局键）+ 10 个特性域 `lib/query-keys.ts`，扩展 `modelsQueryKeys`/`channelsQueryKeys`；163 处内联字面量迁移 145 处，剩余 18 处均为单站点查询键（无 query↔invalidate 配对，按判定标准保留，含 3 个表格列表键与 15 个一次性静态/参数键）。工厂产出键结构与原字面量逐字一致（`['prefill_groups','model']` 的 snake_case、`['channels', id, provider, 'inference-status']` 等特殊形态均原样保留）。**判定标准**：迁移所有 ≥2 生命周期站点（query/invalidate/refetch/remove/cancel/isFetching/setQueryData）或同族多形态的键；单站点键无「拼写错误静默永不失效」风险，保留字面量。
- [ ] **4.6** 约 90 处 Intl locale 接线：现有 lint 只拦**不安全** locale，不拦**缺失** locale，故 `ai-elements/context.tsx` 的 10 处硬编码 `'en-US'` 等静默通过且不随语言切换重排
- [ ] **4.7** 补覆盖率与鉴权关键路径测试 —— **口径更正（2026-11-12 复核）**：Playwright **已就绪**（`web/package.json:19` `test:e2e`、`:92` `@playwright/test^1.64.1`、`web/playwright.config.ts`、`web/e2e/` 4 个 spec、`.github/workflows/ci.yml:172` e2e job），仍缺 `msw` / `@vitest/coverage-v8` / 80% 阈值；`web/src/routes/` 64 个 `.tsx` **零测试**，`routes/oauth/$provider.tsx`（**279 行**，含 code/state 交换、postMessage origin 校验、`sanitizeAuthRedirect`、登录去重）零测试，`_authenticated/route.tsx` 守卫同。

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
| Phase 3 越层治理 | 6 | 3.1✅（CI 门禁）；3.2 slice-1✅（subscription 写路径）/slice-2✅（channel 查询构造）+ §八批次✅（controller 直接 `model.DB` 引用 12→0，口径一达成）；3.3✅；3.4✅；3.5✅；3.6 部分✅（hasEnabledMultiKey、processChannelError；SearchChannels 内存分页需过滤下沉设计，延期） | 进行中（口径二「不 import service」40→36，余下多为薄控制器，按域推进） |
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
| `c02d0e493` | oauth / wechat / channel-test / setup | `model.MigrateLegacyGitHubBinding`、`model.CreateOAuthUserWithBinding`、`model.CreateOAuthUserWithProviderUpdate`（stamp 回调保真「insert→设 provider id→同事务更新」原顺序，provider id 列 map 收进 model）；`model.UpdateUserBindColumnForSession`；`model.GetRootUserID`；`model.CreateSetupRootUser`（裸 insert 保真）+ `model.InitializeSetup` |
| `230a6ee3b` | user 管理 | `model.GetUserByUsername`/`GetUserByIdUnscoped`；新 `service/user_admin.go`（`UpdateAdminUserWithPermissions`/`CreateAdminUserWithPermissions`/`DemoteUser`——事务横跨 user 表与授权矩阵故归 service；controller 本地 helper 的 root 判定改为 `operatorRole` 参数，gin 上下文不进事务 seam）；`user.go` 五处调用改道 |

### 8.2 进度

- controller 生产文件直接 `model.DB`/`model.LOG_DB`/`.Transaction(` 引用：**12 → 0** ✅（`rg 'model\.(DB|LOG_DB)|\.Transaction\(' controller/ --glob '!*_test.go'` 无输出）。Phase 3.2 验收口径一（「controller 中 model.DB 直调归零」）达成。
- 下沉路径（按风险从低到高，每步原子提交）：纯数据访问单列 → model 函数（对齐既有 `model.DeleteDisabledChannel()` 惯例）；需补 service 依赖/未来横切面 → service seam（对齐 `service/subscription_plan.go`）；**事务横跨 model + service/authz → service 层拥有事务**（`service/user_admin.go`），controller 只传值 + 回调/参数，gin 上下文不进事务。
- 保真手法沉淀：① 事务闭包内只有一句 model 调用 → model 包一层非 Tx 函数；② 「事务内需按 controller 上下文分派」→ 传参（operatorRole）或回调（stamp），把分派逻辑移进事务所有者；③ 顺序敏感步骤（insert 后才 set provider id）→ 用回调保持原顺序；④ 查询列投影/`Unscoped` 等语义 → model 函数逐字复刻。
- 验收口径二（「不 import service 的 40 文件 → 个位数」）独立：79 个生产 controller 文件中 43 个 import service、36 个不 import。剩余 36 个中相当比例本就是薄控制器（audit/captcha/log/system_info 等无业务逻辑），需按域评估后单独推进，不宜机械凑数。
- 口径二首批落地（2026-10-21，`76ece2320`）：`ratio_sync.go` 整条「上游倍率同步管道」下沉——并发抓取 + 四种 wire 格式解析（ratio_config / pricing / OpenRouter / models.dev）+ 归一化 + diff，全部移入 `service/ratio_sync.go`（service 导出 `DefaultEndpoint`/`DefaultTimeoutSeconds`/`ValueMap`/`BuildDifferences`/`FetchUpstreamRatios`/`ListSyncableChannels`）；controller 从 1145 行瘦到 108 行（只做绑定/上游解析/渲染）。纯函数测试 `TestPricingSyncExpressionPriority` 随之移入 service 包，controller 保留两个 HTTP handler 测试。口径二计数 36 → **35**。
- 口径二第二弹（2026-10-21，`0d756cb2d`）：`task_plugin.go` 的同步状态机下沉——controller 包的 `taskPluginSyncState`（互斥状态：hashes/errors/lastRebuild）+ 145 行同步 pass + 周期循环整体移入 `service/task_plugin_sync.go`；新增 tracker/read API（`TaskPluginSyncPluginErrors`/`LastRebuild`/`SetLastRebuild`/`Remember`/`Tracker`/`Forget`），handler 与测试均经 API 访问，不再摸 controller 包级互斥量；`main.go` 改调 `service.SyncTaskPlugins()`。controller 872 → 688 行。口径二计数 35 → **34**。
- 口径二第三弹（2026-11-10，`8fcb614e3`）：`deployment.go`（io.net 模型部署薄代理，19 handler）下沉——`service/model_deployment.go` 接管 OptionMap 读取（`IoNetAPIKey`/`StoredIoNetAPIKey`/`ModelDeploymentSettings`）、API key 连通性校验（`ValidateIoNetAPIKey`，返回原始上游错误以保留 `ionet.APIError` 渲染）、以及两个纯投影/统计函数 `MapIoNetDeployment`/`ComputeDeploymentStatusCounts`；controller 807 → 718 行。口径二计数 34 → **33**。
- 口径二第四弹（2026-11-10，`d01f801f6`）：`performance.go`（371 行）下沉——`service/performance.go` 接管响应 DTO、运行时/磁盘采样（`GetPerformanceStats`/`GetDiskCacheInfo`）、日志文件列表与聚合（`ListLogFiles`/`BuildLogFilesResponse`）、清理 pass（`CleanupLogFiles` → `LogFileCleanupResult`）；controller 371 → 98 行，只留参数校验与渲染。注意：`service.LogCleanupResult` 已被异步 DB 日志清理系统任务占用，文件清理结果改名 `LogFileCleanupResult` 避冲突。口径二计数 33 → **32**。
- 口径二第五弹（2026-11-10，`cd60059b1`）：`redemption.go` 下沉——`service/redemption.go` 接管 `ValidateRedemptionExpiry`（过期时间业务规则）与 `CreateRedemptionBatch`（UUID 生成 + 插入循环，**失败时返回已生成的 keys**，保真原端点「部分成功」响应）；`AddRedemption`/`UpdateRedemption` 改道，本地 `validateExpiredTime` 删除。纯搬迁，无 SQL/schema 变化。
- 口径二第六弹（2026-11-10，`74f881c08`）：`audit.go` 的英文兜底模板与渲染下沉——`service/audit_content.go` 接管 `auditContentTemplates`（~57 行稳定业务文案）并导出 `AuditContentEN`；controller 4 个审计 helper + `oauth.go`/`subscription.go`/`user_quota.go` 共 8 处调用改道。**纯搬迁**：模板、`os.Expand` 渲染、审计参数与 OWASP 相关字段逐字未变。`user_quota.go` 因此新增 service 依赖。口径二计数 32 → **29**（79 个生产 controller 文件中 50 个 import service）。
- 候选评估结论（2026-11-10）：`task_plugin_debug.go`（269 行）的 `taskPluginSubmitDiagnostics` 结构体强绑 `*gin.Context`（`logger.LogDebug(d.context, …)`），搬进 service 会把 gin 带进 service 层，**判定为不该为凑计数而搬迁**；`SetTaskPluginStatus`（~97 行状态机）所在 `task_plugin.go` **已 import service**，属逻辑债而非口径二，单列后续；`audit.go` 的其余 helper（`recordManageAudit` 10 文件、`recordUserSecurityAudit` 10 文件）全部依赖 gin 上下文（IP/route/status/session），盲目搬迁爆炸半径过大，本次只取纯模板渲染部分。
- 口径二第七/八/九弹（2026-11-10）：
  - `4a2b842e3` Uptime Kuma：`service/uptime_kuma.go` 接管常量/DTO/`getAndDecode`/`fetchGroupData`/并发组编排（`FetchUptimeKumaGroups`）；controller 155 → 13 行。纯搬迁，HTTP 超时与 errgroup 扇出逐字未变。
  - `77e557c52` 支付回调路径 + Stripe 链接：`service.PaymentReturnPath` 替代 controller 全包共用的 `paymentReturnPath` wrapper，守卫测试 `TestPaymentReturnPathUsesDefaultDashboardRoutes` 随之移入 service 包；`service.GenStripeSubscriptionLink` 接管 Stripe Checkout 调用；6 个调用点改道，`controller/return_path.go` **整文件删除**（不留透传 wrapper），stripe handler 移除 stripe SDK import。纯搬迁。
  - `c103cc8b2` 支付渠道可用性规则：`service/payment_availability.go` 接管 16 个 `IsXxx` 谓词（Stripe/Creem/Waffo/WaffoPancake/Epay 的 top-up 与 webhook 开关 + 合规确认），只读取 setting 状态、无 gin 依赖；`controller/payment_webhook_availability.go` **整文件删除**；5 个 topup controller 与可用性测试改调 `service.IsXxx`，测试覆盖不减（含 `TestEpayCallbacksVerifyBeforeSettlement` 验签守卫）。纯搬迁。
  - 三次合计：口径二计数 29 → **25**（生产 controller 文件 79 → 77，已删除 2 个纯 helper 文件；import service 者 52）。
- 口径二同批评估结论：`log.go`（143 行）全是参数解析 + `model.*` 查询转发、`vendor_meta.go`（138 行）全是薄 CRUD + `vendorAPIError` 响应渲染，均无业务逻辑可下沉；`channel_authz.go`（136 行）依赖 `PatchChannel`/`equalStringPtr` 等 controller 包类型，搬进 service 会造成依赖倒置，且有 `TestChannelFieldsAreClassified` 守卫测试——**三者判定不搬**。
- 口径二经验：① 跨包匿名结构体提升为命名类型（ratio_sync 的 `UpstreamSource`）；② 「测试需要 seed/readback 的内部状态」用一组命名清晰的 tracker API 暴露，而非测试特判；③ sed 区段提取 + 机械替换（`c.Request.Context()`→ctx 等）显著降低大块搬迁的转录风险；④ 外部 SaaS 集成的薄代理型 controller，把「配置读取 + 校验/请求构造 + 纯投影」下沉即可让 controller 专注绑定/渲染，无需把每个 handler 都包一层 service；⑤ 新 service 类型名先 `rg` 查重（撞过 `service.LogCleanupResult`）；⑥ **纯搬迁优先**：只挪不改，能同时拿到「逻辑归位 + 计数下降 + 零行为风险」，比大改 handler 划算；⑦ 整文件纯 helper（return_path / payment_webhook_availability）下沉后可直接删除源文件，比留透传 wrapper 更干净，计数同时下降。
- 口径二下一候选（待评估）：`plugin_protocol_limiter.go`（157 行，含限流器状态）、`billing.go`（108 行）、`usedata.go`（104 行）、`prefill_group.go`（90 行）、`model_pricing_config.go`（81 行）、`payment_compliance.go`（80 行）、`perf_metrics.go`（79 行）、`checkin.go`（62 行）；`captcha`/`system_info`/`image`/`missing_models` 等薄控制器不宜机械凑数。

### 8.3 验证

- 每提交 `gofmt -l` 无输出、`go build`/`go vet ./controller/ ./model/ ./service/` 零告警。
- 子集回归（三方言矩阵，DSN 缺省走 sqlite 分支；2026-10-21 起重跑均显式带 DSN 实证三方言）：metadata 14.8s✅；channel `Upstream|AdvancedCustom` 7.5s✅；`Channel|Codex` 22.3s✅；`Plugin|Subscription` 8.1s✅；`OAuth|GitHub|Legacy|Bind` 150.4s✅；`WeChat|Bind|Channel` 214.1s✅；`User|Manage|Register` 218.2s✅（零失败零跳过）。
- layering 扫描（`rg 'model\.(DB|LOG_DB)|\.Transaction\(' controller/ --glob '!*_test.go'`）逐提交复核，移除项逐条确认。
- **全量 controller 回归（回填 2026-10-21）**：`go test ./controller/ -count=1 -timeout 40m` + 三方言 DSN（mysql 8.4.11 @127.0.0.1:13306 / pg 15.18 @127.0.0.1:15432，凭据取自 `newapi-test-*` 容器）—— **1179.155s 全绿、零失败（EXIT=0）**。三方言实证执行：`TestModelManagementDatabaseMatrix` 的 sqlite/mysql/postgres 三分支各自建隔离库并 PASS（`isolated database: newapi_audit_* (mysql|postgres)`）。结论与 3.2 既有记录闭环——单包串行此前唯一失败即 `TestDeleteRedemptionBatch`，`bf3784977` 修复后全量归零。
- 注：三方言矩阵由 `TEST_MYSQL_DSN`/`TEST_POSTGRES_DSN` 环境变量驱动，**未设置时 mysql/postgres 子测试静默 skip**；此前若干“子集通过”实为仅 sqlite 执行，全量口径须显式带 DSN。
- **下沉批次后终版全量回归（回填 2026-10-21 下午）**：`go test ./controller/ -count=1 -timeout 40m` + 三方言 DSN —— **1289.631s 全绿、零失败（EXIT=0）**。覆盖本批全部下沉提交（oauth/wechat/channel-test/setup/user + 8 个业务域）。另跑 `./model/ ./service/ ./middleware/`（同为改动包）。
- 全 controller 全量仍受 Windows SQLite 文件锁/多 DB 池影响，看门禁以 CI（Linux）为准（同 3.3 既有结论）。
- **口径二第五/六弹后终版全量回归（回填 2026-11-10）**：`go test ./controller/ -count=1 -timeout 40m` + 三方言 DSN —— **304.249s 全绿、零失败（EXIT=0）**。三方言实证执行：`TestModelManagementDatabaseMatrix` 的 sqlite/mysql/postgres 三分支全 PASS（mysql/pg 各自建隔离库 `newapi_audit_*`）。注：本次重建了全新测试容器，耗时比 2026-10-21 的 1289.631s 快约 4 倍——推断为旧容器累积上千个 `newapi_audit_*` 隔离库导致 DDL 元数据膨胀；**回归前重建空容器可作为提速手段**（`mysql:8` / `postgres:15` 本地镜像，端口 13306/15432，root/123456）。

### 8.4 性能与资金安全综合研判及 P0 修复（2026-11-10 ~ 11-11）

四线只读研判（付款安全 / 认证与越权 / 性能热点 / 通用代码安全）后，按「不破坏既有契约、不误拒正常付款」原则实施以下 P0 修复。用户要求：中文注释完整、架构健壮、不引入回归。

| 提交 | 修复 | 要点 |
|---|---|---|
| `ebac75529` | #2 webhook 开关与充值项解耦 + #4 Epay 订阅门禁 | `service/payment_availability.go` 明确区分 TopUp（要求充值商品）与 Webhook（只要合规 + 验签凭证）；仅配订阅计划时不再出现「已收款、回调 403、渠道停止重试」。`SubscriptionEpayNotify`/`SubscriptionEpayReturn` 补同一门禁。测试重写锁定新边界，新增 `TestEpayCallbacksRejectedWhenWebhookDisabled`（nil DB 证明门禁在结算之前）。 |
| `6dd86f88d` | #7 日志凭据泄露 | 新增 `common.LogFingerprint`（HMAC-SHA256 + CryptoSecret，取 16 hex）：签名/回调 body/Epay verify payload/API 响应/兑换码全部改为不可逆指纹。**刻意不改动 `LogText` 格式串**（它是 `log_text.zh-CN.json` 的翻译 key），故 20 条既有中文翻译全部保留。OWASP：认证/支付事件日志不得记录可用凭据。 |
| `3c4b4e0a8` | #1 金额对账 + #3 先落单/失败重试 | 见下。 |

#### #1 金额对账（最高资损风险）

- 威胁：渠道侧 100%-off 优惠券会让 `payment_status=paid` 但 `amount_total=0`；旧实现验签后只按本地订单金额入账，不核对实付金额 → 额度凭空增发。
- 架构：`service/payment_reconciliation.go` 定义 `PaidAmount`/`OrderAmount` 与 `ReconcilePaidAmount`；`service/payment_fulfillment.go` 成为**强制**结算 seam（所有回调必须传已验签实付金额，内部路径传 nil）；`model/payment_amount_snapshot.go` + `TopUp`/`SubscriptionOrder` 各 3 列（AutoMigrate 自动加列）持久化下单时的金额/币种/商品快照。
- **避免过度设计导致误拒**（本次最关键的设计约束）：
  - 快照存「下单时提交给渠道的金额字符串」，不存内部 `float64`：Waffo 零小数币种（JPY/KRW）会取整，用 float 会误拒。
  - Stripe 充值 `Money` 是 `GetChargedAmount` 的「充值额度金额」，与 Stripe `Price×Quantity` 实付不是同一概念 → **不写金额快照**，只靠「实付 > 0」拦截 0 元券。
  - Stripe/Creem/Pancake 订阅由渠道侧 Price/Product 定价，本地 `plan.PriceAmount` 不是权威来源 → 不写金额快照，只写币种/商品绑定。
  - Epay 回调不带币种，传空表示「未知」，不硬编码 CNY（否则与订阅计划币种冲突误拒）。
  - Waffo 优先用 `OrderAmount`（与下单快照同源），`FinalDealAmount` 可能含渠道折扣/汇率，直接比对会误拒。
  - 回调无金额字段（如部分异步支付成功事件）或历史订单无快照 → 只做保守校验并告警，绝不误判为 0 元。
  - 充值/订阅快照按各自表读取，消除「充值回调误读同名订阅快照」的跨表歧义。
- 覆盖：Stripe / Creem / Epay / Waffo / WaffoPancake 的充值与订阅共 10 个结算入口。

#### #3 Stripe 下单竞态与重试

- 旧实现先建 Checkout Session 后落单，且 webhook 无论成败都返 2xx（渠道收到即停止重试）→ DB 抖动或回调早于落库时「收款成功、额度未入账」且永不补单。
- 现在先落单、后建 Session；建 Session 失败把订单标记 Failed（充值）/ Expire（订阅），不留 pending 孤儿单。
- `StripeWebhook` 引入 `stripeSettlementOutcome` 四类结果：永久拒绝（金额对账）返 200 不重试；临时失败（订单缺失/DB 抖动）返 **503** 触发 Stripe 重投；成功/忽略返 200。幂等仍由 model 行锁 + 状态 CAS 保证。

#### 验证与 OWASP 依据

- 依据：OWASP Authentication Cheat Sheet（敏感操作重认证、凭据轮换、认证事件日志不记密钥）、Session Management Cheat Sheet（特权变更后会话 ID 更新/销毁、服务端强制失效、日志用盐哈希关联而非原始凭据）、项目 AGENTS.md 的 OWASP 强制条款。
- 新增测试：`service/payment_reconciliation_test.go`（0 元拒绝、金额/币种/商品不符、合法付款、无快照与缺金额字段的向后兼容）、`controller/stripe_amount_test.go`（0 元券事件解析、分→主单位换算、缺字段不阻断、结算结果分类）、`common/log_safety_test.go`（指纹确定性/不泄露/空值）。
- `gofmt`/`go build ./...`/`go vet` 全绿；定向支付回归全绿。
- **终版验证（2026-11-11，遵守本机资源最小化约束）**：不启动 Docker 中间件，采用
  `GOMAXPROCS=2 GOWORK=off go test -p 1 ./service/ ./common/ ./controller/ -run '<支付相关定向用例>' -count=1`
  —— **service 0.430s / common 0.379s / controller 0.692s，零失败**。覆盖新增的金额对账、
  日志指纹、0 元券、结算结果分类、五渠道 webhook 门禁、Epay 回调验签守卫、兑换码批量删除。
  已移除 `newapi-test-*` 容器释放磁盘。
- **资源约束（用户明确要求，后续遵循）**：本机 Docker 配置有限，运行组件/中间件/构建时
  务必最小化资源。因此：① 默认跑 sqlite-only 定向子集（无需 Docker），并加
  `GOMAXPROCS=2 -p 1` 限制并行；② 三方言矩阵仅在必要时按需启动 `mysql:8`/`postgres:15`
  两个容器（端口 13306/15432），用完立即 `docker rm -f`；③ 不并发跑多个全量回归
  （曾出现 SQLite 文件锁与容器残留导致的假失败）；④ 三方言全量的金标准以 CI（Linux）为准。
- 三方言口径说明：未设置 `TEST_MYSQL_DSN`/`TEST_POSTGRES_DSN` 时 mysql/postgres 子测试静默
  skip，因此**本次定向验证仅覆盖 sqlite 分支**；三方言真跑需带 DSN（历史实证见 8.3）。
- 一次环境假失败记录：容器 `docker start` 复用旧数据卷时，`TestDeleteRedemptionBatch` 的
  「use an empty test database」断言失败、`TestSecurityAccountDeletionConcurrentRequests`
  受 SQLite 锁影响；重建全新容器后两用例 1.757s 全过，确认为环境而非代码回归。

#### 未决项（不擅自改动，需产品决策或前后端协同）

- **#5 `add_quota` 二次验证**：`service/security_verification.go:52` 注释明确「Quota adjustments are not gated」，`controller/security_enrollment_test.go:778` 把 `add_quota` 从 `admin.user.manage` scope 排除——属**有意设计**。**口径更正（2026-11-12 复核）**：前端并非没有调用，而是 `web/src/features/users/api.ts:168` 的 `adjustUserQuota` 直发 `POST /api/user/manage`（对照同文件 `:152` 的 `manageUser` 带 `securityProofConfig`），即「有调用、但无 proof 链」。补偿控制：`AdminAuth`(`router/api-router.go:156`)、`model/user_quota_adjustment.go:39/42` 行锁 + 目标角色校验、`controller/user_quota.go:34-45` defer 审计（审计失败不回滚，见 `controller/user_manage_test.go:370`）。强加 proof 需前后端协同设计新 scope 与迁移方案。
- **#6 改密不撤 PAT/relay token（口径更正 2026-11-12）**：复核发现 **PAT 实际已失效**——`model/user_access_token.go:282` 校验 `user.AuthVersion != identity.UserAuthVersion` 即返回 `ErrUserSessionInactive`，改密事务内 `model/account_security.go:50` 递增 auth_version 后 PAT 随即失效；`controller/user.go:863` 对 PAT 返回 nil bundle 属预期行为。**真正残留仅 relay token**：`middleware/auth.go:558` → `model/token.go:220` `ValidateUserToken` 不读取 auth_version。OWASP 两份 Cheat Sheet 均未强制「改密后全局撤销所有 token」（Session Management 明列为 coverage gap，仅要求会话 ID 更新/销毁与重认证），且 relay token 是 API 网关核心凭据，全局撤销属产品级决策。既有缓解：`auth_version` 递增使浏览器会话与 PAT 失效；软/硬删用户删 PAT 并清缓存；禁用用户被 Status 拒。
- **P1 性能**（本次未改）：`channel_cache.go:129` 全局读锁覆盖整段选渠道 + 写方三重循环；`model/log.go:101` 每请求同步 INSERT；后台 goroutine/ticker 无 context；`model/channel.go:446` SearchChannels 内存分页（3.6 既有项）。
- **P1 SSRF**：项目已有 `common/ssrf_protection.go`/`service/protected_fetch_client.go`，但 `custom_oauth.go:157`、`service/ratio_sync.go:741`、`service/uptime_kuma.go:146` 的管理员/匿名外发请求未复用（均 RootAuth 或匿名可达，需单独设计）。

### 8.5 P1 轮：SSRF 收敛与后台周期任务错峰（2026-11-11）

延续 §8.4 未决的 P1 项。全程遵守资源最小化约束：**零 Docker、GOMAXPROCS=2、-p 1、sqlite-only 定向验证**。

| 提交 | 内容 |
|---|---|
| `07589bd7e` | 三处 SSRF 绕过收敛 |
| `0819ed0ca` | 后台周期任务错峰抖动 + ticker 生命周期 |

#### SSRF：精确区分「用户可控 URL」与「operator-managed 目标」

项目已有完整防护（`common/ssrf_protection.go`、`service/protected_fetch_client.go`：`ValidateSSRFProtectedFetchURL` 做 URL 层校验，受保护 client 在拨号层做 IP 校验 + 重定向校验），但三处外发请求绕过了它。本次不另造轮子，只做接线与边界划分：

- `controller/custom_oauth.go:157`（RootAuth，discovery URL 由请求体直接给出，且回显上游错误体与 JSON）：**最严重的绕过**。现在先 `ValidateSSRFProtectedFetchURL`，再用 `GetSSRFProtectedHTTPClient()` 发请求（拨号层 IP 校验 + 禁重定向）。
- `service/ratio_sync.go:741` + `controller/ratio_sync.go:58`（RootAuth，请求体的 BaseURL 与 Endpoint 可为任意 http(s) URL）：在 `resolveUpstreams` 里对**请求体显式 upstreams** 逐个校验；从数据库渠道读取的 BaseURL **刻意不校验**——那是管理员配置的部署目标，`service/http_client.go:146-152` 明确说明其可能合法指向内网/私有链路/本地代理，一刀切会误伤现网部署。
- `service/uptime_kuma.go:146`（`/api/uptime/status` 匿名可达，URL 由管理员在控制台配置，默认 client 跟随重定向）：初始目标保持可达（管理员可能自建在内网），但**禁止重定向逃逸**到内网/云元数据。

三处外发客户端统一挂 `checkProtectedFetchRedirect`。为避免后续重构悄悄退回裸 client，抽出 `newRatioSyncHTTPClient()` / `newUptimeKumaHTTPClient()` 并加回归测试锁定 `CheckRedirect`。同时修掉 `GetSSRFProtectedHTTPClient()` 在 `InitHttpClient()` 之前被调用时返回 nil 导致 `client.Do` panic 的健壮性缺陷（惰性构造兜底，不写回全局变量以免与启动初始化/测试替换产生竞态）。

测试全部离线（httptest，无公网、无 Docker）：内网/元数据 IP 被拒、放行路径能解析 discovery JSON、协议白名单、重定向逃逸被拦、两个 client 均保留 `CheckRedirect`。

#### 性能：错峰抖动（零行为破坏）

- 新增 `common/periodic_sync.go`：`PeriodicSyncInterval(base)` 返回 ±10% 对称抖动（`math/rand/v2` 包级函数并发安全、无需播种），并对非正 base 做防护避免 `Sleep(0)` 忙循环打满 CPU。
- 接入 `SyncChannelCache` / `SyncOptions` / `InitBatchUpdater` / `SyncTaskPlugins`：多实例不再在同一瞬间一起全表扫描主库，把周期性尖峰摊平；平均同步频率不变。
- `SyncTaskPlugins` 原 `for range time.NewTicker(30*time.Second).C` 的 ticker 从未 Stop 且无退出路径，改为显式间隔循环。**优雅关停需要调用方传 context，属后续架构改动，已在代码注释中标明**，本次不改启动/立即同步语义。

#### 仍然未改的高风险性能项（仅给方案）

- `model/channel_cache.go:129`：全局 `channelSyncLock.RLock()` 覆盖整段 `GetRandomSatisfiedChannel`，写方 `CacheUpdateChannelStatus:265-282` 持写锁三重循环遍历全 `group2model2channels`。auto-ban 时所有请求串行阻塞。方案：改为 `atomic.Value` 不可变快照或按 group 分片锁；涉及缓存一致性与既有锁顺序死锁约束（`InvalidatePricingCache` / `updatePricingLock`），需独立设计与压测。
- `model/log.go:101`：开消费日志时每请求同步 `LOG_DB.Create` + 2~3 次查库。方案：有界异步队列 + `CreateInBatches` + TTL 分区；会改变日志可靠性语义（可能丢日志），需产品确认。
- `model/channel.go:446` + `controller/channel.go:368-419`：SearchChannels 在 Go 里全量加载后过滤/切片分页（台账 3.6 既有项）。方案：把 status/type 过滤、COUNT、LIMIT/OFFSET 全部下推 SQL，需覆盖三方言与 tag 分支。
- `common/rate-limit.go:95`：InMemoryRateLimiter 的过期清理 ticker 随 limiter 生命周期常驻、无 Stop；与 limiter 销毁流程绑定，需一并设计。

#### 验证

- `gofmt -l` 无输出；`go build ./...` EXIT=0；`go vet ./controller/ ./service/ ./model/ ./common/` EXIT=0。
- 定向回归（零 Docker）：common 0.189s / service 0.320s / controller 0.458s，零失败；model 无匹配用例。
- 口径说明：未带 DSN，mysql/postgres 子测试静默 skip，本次仅覆盖 sqlite 分支。

### 8.6 3.6 结项：SearchChannels 过滤/统计/分页下沉 SQL（2026-11-12）

延续 §8.5 未改的高风险性能项。用户资源约束继续生效：**零 Docker、GOMAXPROCS=2、-p 1、sqlite-only 定向验证**。

提交 `6866ca61e`。

#### 问题

`controller.SearchChannels` 先把匹配渠道全量查进内存（含 `key`、`settings`、`param_override` 等大字段），再在 Go 里做 status 过滤 → type 统计 → type 过滤 → 切片分页。渠道数千时，后台列表每次轮询都是 O(N) 全表加载与分配，成为内存/GC 与主库压力源。`model.SearchChannels` 与 `SearchTags` 还各自维护一份相同的方言列名与搜索谓词，属 D4 登记的重复规则。

#### 实现

- `model/channel_search.go`：
  - `NewChannelSearchQuery(keyword, group, modelKeyword, statusFilter, typeFilter)` 返回可组合查询，谓词与旧 `SearchChannels` 逐字一致（id/name/key/base_url 模糊或精确匹配 + models LIKE + group 过滤），叠加 status/type 纯列谓词，恒定 `Omit("key")`。
  - `ChannelTypeCounts` 暴露 `GROUP BY type`，供前端筛选器计数。
  - `channelSearchColumns()` 自行判定 PostgreSQL 双引号列名，**不复用 `commonKeyCol` 等依赖 `initCol()` 启动顺序的包级变量**，消除早期调用/测试构造空列名导致 SQL 语法错误的隐患；判定逻辑与 `initCol()` 完全一致。
- `controller.SearchChannels`：
  - **普通模式（默认高频路径）**：`Count` 取 total、`Group(type)` 取 type_counts、`Limit+Offset` 取当前页，只把一页 Channel 读进内存。
  - **tag 模式**：保留「按 `SearchTags` 顺序重组」与内存分页——tag 顺序来自带排序的子查询，跨 SQLite/MySQL/PostgreSQL 无法用一条可移植 `ORDER BY` 表达，强行统一会改变返回顺序；但把 status/type 过滤下推到候选查询，显著减少加载行数。这是台账 3.6 明确列出的两种取舍之一，已在代码注释中写明理由。
  - 提前解析 type/page/pageSize，使全部谓词可在 SQL 层完成。
- 去重：`model.SearchChannels` 改为委托 `NewChannelSearchQuery`（保留导出 API 供确有全量需求的调用方，并注明不得用于请求热路径）；`SearchTags` 复用 `channelSearchColumns()`。

#### 行为保真点（最容易踩坑）

- `type_counts` 必须基于**不含 type 过滤**的查询：旧内存实现是「先统计、后按 type 过滤」，`GetAllChannels` 的 `countQuery := NewChannelListQuery(group, statusFilter, -1)` 也是同样语义。若误用带 type 过滤的查询，切换筛选器时计数会跳变。
- `total` 是 type 过滤后的总数；超出范围的页返回空 items 但 total 不变（旧实现 `startIdx = min(..., total)` 的语义由 SQL Offset 天然保证）。
- `Omit("key")` 必须保留，渠道密钥不得进入内存或响应。
- 排序仍由 `sortOptions.Apply` 施加；`NewChannelSortOptions(..., idSort)` 已含 IDSort，与旧 `resolveChannelSortOptions` 等价。
- status 语义：`-1` 全部、`1` 仅启用、`0` 仅停用（`status != enabled`），沿用既有 `applyChannelStatusFilter`，与内存过滤逐条等价。

#### 测试（sqlite `:memory:`，零 Docker、零公网）

- model 层 `channel_search_test.go`：过滤计数矩阵、分页（首页/次页/越界页）、priority 排序、`Omit("key")`、关键词与模型谓词、`ChannelTypeCounts` 不受 type 过滤影响。
- controller 层 `channel_search_test.go`：handler 级响应断言——分页 items 顺序与 total、status/type 组合过滤、`type_counts` 不变性、关键词搜索、tag 模式分组顺序与过滤、响应绝不泄露 key。
- 最终定向回归：model 0.272s / controller 0.444s / service 0.325s / common 0.160s，零失败；`gofmt`/`go build ./...`/`go vet` 全绿。

#### 仍待办的高风险性能项（未改）

`model/channel_cache.go:129` 全局读锁覆盖整段选渠道（`atomic.Value` 快照 / 按 group 分片锁）、`model/log.go:101` 每请求同步 INSERT（有界异步队列，会改变日志可靠性语义）、`common/rate-limit.go:95` 清理 ticker 生命周期。

#### 口径说明

未带 `TEST_MYSQL_DSN`/`TEST_POSTGRES_DSN`，mysql/postgres 子测试静默 skip，本次仅覆盖 sqlite 分支；新查询的方言差异集中在 `channelSearchColumns()`，三方言金标准仍以 CI（Linux）为准。

### 8.7 下一批候选优化点清单（2026-11-12 只读研判，未实施）

四路只读调研（后端架构/代码质量、后端性能/正确性、前端/工程化、安全/OWASP）汇总。仅登记实证结论，**本轮不实施**。分级标准：【A】可直接做＝改动小、收益明确、有现成范式、不破坏契约；【B】需专门设计＝收益高但需压测或迁移；【C】需产品/前后端决策。

#### 【A】可直接做（高性价比，优先）

| 优先级 | 点 | 证据 | 影响 / 修法 |
|---|---|---|---|
| A1 | **日志脱敏漏网**（上一轮 #7 未覆盖） | `controller/topup.go:363`、`:436` 仍把 `common.GetJsonString(params)` 与 `path=%q` 完整落盘：Epay 下单/回调参数含 `sign`，GET 回调查询串亦完整记录。同文件 `:464/:476/:495` 及 Creem/Stripe/Waffo/Pancake 已改为 `common.LogFingerprint` | 签名可重放。直接套 `:464` 范式，仅改日志参数、不动格式串（保翻译 key） |
| A2 | **审计写失败被吞且抑制兜底** | `model/audit_log.go:115,134` 写库失败仅 `LogError` 且函数返回 void；`controller/audit.go:39,87,101` 无条件 `markAuditLogged(c)`；`middleware/audit.go:131` 见标记即 return 跳过兜底 | 审计库故障时**零留痕**，68 处站点受影响（45 manage + 23 security）。令 `RecordAuditLog` 返回 error、仅成功才置标记，约 5 文件 |
| A3 | **`UpdateOption` 丢弃 DB 写错误** | `model/option.go:271` `DB.FirstOrCreate(...)` 与 `:276` `DB.Save(...)` 均未取 `.Error`，`:278` 直接 `return updateOptionMap(...)`；对照 `:324,328` 的 `UpdateOptionsBulk` 逐步查错 | 写库失败仍返回 nil、内存已生效、重启丢失；7 处生产调用。补两处 `.Error` 即可 |
| A4 | **配置同步读错误被丢弃** | `model/option.go:208` `options, _ := AllOption()`；`:215-218` 逐 key 失败仅 SysLog；`SyncOptions:225` 每 60s 重放 145 legacy key + 25 config 模块 | DB 读失败时注册开关/验证码/限流/密钥变更永不生效且零告警 |
| A5 | **渠道缓存原地写导致 data race** | `model/channel_cache.go:270` `channel.Status = status` 直接改缓存对象；读方 `:131` RLock 与 `channel.go:774` 的 `GetChannelPollingLock` 两锁不互斥 | 仿 `CacheUpdateChannel:304`（已替换指针），改浅拷贝写回 `channelsIDM[id]=&copied`，约 3 行 |
| A6 | **写锁内三重循环 O(G×M×C)** | `model/channel_cache.go:274-284` 持写锁遍历全 `group2model2channels`；读锁 `:131-223` 还覆盖 `filterCandidateIDs`/`sort.Sort:169`/`rand` | **无需整体重写 atomic.Value**：在 `InitChannelCache:51-99` 顺带建 `channelID→[]group/model` 索引，写锁内 O(1) 定位，2 处改动 |
| A7 | **前端 footer XSS** | `web/src/components/layout/components/footer.tsx:238` `dangerouslySetInnerHTML={{__html: footerHtml}}` 无净化；对照 `html-content.tsx:126-131`、`markdown.tsx:760` 均用 DOMPurify；`footer_html` 来自服务端可配 | 复用现成 `sanitizeHtmlContent` 即可 |
| A8 | **渠道 key 前缀写入 Name** | `controller/channel.go:800-805` `BatchAddSetKeyPrefix2Name` 把 key 前 8 位拼进 `Name`；`Omit("key")` 不覆盖 name，`Name` 随 ChannelRead 全量返回并进 `channel.key_view` 审计 | 改用 `common.LogFingerprint` 生成不可逆标识 |
| A9 | **冷缓存/重试 DB 查询放大** | `model/user.go:1336 GetUserSetting`（`log.go:287/353` 每次日志调用）未命中 1 次 DB，defer `:1318-1324` 又触发 `updateUserCacheField:263` 第 2 次；`GetUsernameById:1547` 同理；`relay.go:279` 每次重选渠道、`:96` 每次同步 `RecordErrorLog` | defer 回填复用已得 `UserBase`/`AuthVersion`，`GetUserSetting` 按请求存 `gin.Context`，3 处改动可减半 |
| A10 | **鉴权关键路由零测试** | `web/src/routes/` 64 个 `.tsx` 零测试；`routes/oauth/$provider.tsx` 279 行含 code/state 交换、postMessage origin 校验、`sanitizeAuthRedirect`、登录去重 | 已有 Playwright + vitest 基建，补针对性用例即可 |
| A11 | **`common/` 启动期常量收敛** | 实测 137 个包级变量中 35 个仅 `common/init.go` 写一次、42 个从未赋值；`constants.go:218-246` 20 个限流配置仅 2 文件 25 次读 | 照 `common/disk_cache_config.go:9-41` 只读 struct + getter 范式；顺带清死项（`constants.go:187 GeminiSafetySetting` 0 引用，环境变量完全无效；8 处零调用导出函数） |

#### 【B】需专门设计 + 压测

- **token 缓存失效 fail-open**：`model/token_cache.go:43-47` fence `SET` 与哈希 `DEL` 非原子；`model/token.go:330/338/346/475` 失效失败仅 SysLog 仍写库；读路径 `cacheGetTokenByKey:94-107` 不查 fence → DEL 失败而 SET 成功时，停用 token 在 TTL=60s 内持续放行。需 Lua 原子化或失败即中止写库；fence 固定 10s，DB 写超 10s 时旧快照可回填。
- **option.go 145 case 重复规则**：`model/option.go:348-690` 145 个 case 与 `:33-196` 145 个同 key 默认值逐字重复，已漂移（仅 Init 有 About/HomePageContent/Notice，仅 switch 有 LinuxDOClientId/ClientSecret/MinimumTrustLevel）。`setting/config/config.go:165-273` 反射机制不能直接替代扁平键；小步方案：抽共享 `key→setter` 表，并接线已写好却 0 调用的 `LoadFromDB:42`。
- **后台协程 shutdown handle**：`main.go` 11 个常驻协程，关停段 `:238-259` 仅 `srv.Shutdown` + `FlushBatchUpdates`；`common/rate-limit.go:95` ticker 无 Stop（2 个 limiter 各泄漏一个）、`common/system_monitor.go:37-49` 即使 Enabled=false 也 30s 空转。现成范式 `service/system_task.go:309-317` 的 `context.WithCancel + defer ticker.Stop()`。
- **无界 goroutine 池**：`common/gopool.go:13` `NewPool(..., math.MaxInt32, ...)` 等于无界，`token.go:400/431`、`user.go:1290/1532` 的 `gopool.Go` 不受控。
- **日志表索引优化**（不改可靠性语义）：`model/log.go:58-77` 约 16 个索引，4 个复合索引前缀已覆盖多个单列索引，每次 INSERT 全量维护；删冗余单列索引属 DDL 级改动，需查询计划验证。
- **前端 4.4 `useServerDataTable`**：8 个消费页 2587 行、`columnFilters.find(...)` 已涨到 14 处/6 文件；已有 `use-table-url-state.ts`(269)、`use-data-table.ts:278`、`data-table-page.tsx`(544) 半成品，缺组合 glue 与统一 query key/分页契约。
- **前端 4.3 拆 5095 行 drawer**：实测 `channel-mutate-drawer.tsx` 5095 行、`drawers/sections/` 7 文件 332 行；已接线 5 个 section（`:244-251`），但 `ChannelApiAccessSection` 仍包住 `:3551-4607` 千行主体。另有 `channel-advanced-section.tsx`(81 行) 全 src 零引用孤儿。
- **前端 4.6 Intl 缺失 locale**：生产调用 110 处，`ai-elements/context.tsx` 10 处硬编码 `'en-US'`；`scripts/oxlint/intl-locale.mjs:170-173` 对合法 BCP47 字面量返回 false、缺参数不报，需加分支 + 1 个 test fixture。
- **覆盖率工具链**：`vitest.config.ts` 无 coverage 块、无 `@vitest/coverage-v8`/`msw`、无阈值；CI 仅改文件 oxlint（主干 164 errors）。最小落地：加 provider + reporter + `test:coverage`（暂不设阈值）+ CI 一步。

#### 【C】需产品决策 / 前后端协同

- **#5 `add_quota` proof**（口径已更正，见 §8.4）：前端 `web/src/features/users/api.ts:168` 确有调用但无 proof 链，需新 scope 绑定金额 + 前后端协同。
- **`#6` relay token**（口径已更正）：PAT 已失效，仅 `middleware/auth.go:558` → `model/token.go:220` 不读 auth_version；全局撤销 relay token 属产品级决策。
- **step-up 未覆盖资金写**：`router/api-router.go:160` 人工补单、`:198` 订阅 admin bind、`:220` `PUT /api/option/` 仅 AdminAuth/RootAuth，无 `requireAdminUserProof`。
- **OAuth `iss`+`sub`**：`oauth/oidc.go:110-155` 直接取 userinfo 响应体 `sub` 作绑定 ID，`:106` 的 IDToken 未验签/未校 iss/aud；仅 `oauth/telegram.go:158` 用 `oidc.NewVerifier`。涉及 AGENTS.md 的 OWASP 强制条款。
- **日志异步化**：`model/log.go:101` 同步 INSERT；有界异步队列会改变日志可靠性语义（崩溃丢日志），`usedata.go:95-123` 有范式但不宜默认开启。

#### 口径更正（本轮复核）

- **4.7**：Playwright **已就绪**（`web/package.json:19/92`、`web/playwright.config.ts`、`web/e2e/` 4 spec、`.github/workflows/ci.yml:172`），仅缺 MSW/coverage provider/阈值；台账旧述「两者都不存在」已更正。
- **5.5**：D10 各行均已划线，仅 `model/option.go:348` switch 明确转 5.3，属空项，建议关闭。
- **CSRF**：`SESSION_COOKIE_SECURE=false` 默认值使 `middleware/auth_origin.go:19` 跳过 OriginGuard，但 `docker-compose.prod.yml:33` 已置 true 且 refresh cookie 恒 `SameSite=Strict`，风险低，不作为债。

#### 合规确认（无需改）

审计已排除密钥（`model/audit_log.go:61` TokenRef=SHA256、`middleware/audit.go:209` 不落 body、审计 code 是错误码非验证码）；批量接口带 `user_id` 归属校验；匿名路由未见被低估写操作；前端 token 存储健康（`auth-store.ts` 无 persist、`auth-session.ts:72` withCredentials + 内存 Bearer，localStorage 仅存 aff/view-mode）。
