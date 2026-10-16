# 生产配置清单（AiToByte 定制线）

> 来源：2026-10-15 三库矩阵 + 100VU A/B + 微基准的全部实证结论，
> 详见 `perf/baseline-mock-20261015.md`。本清单只收录被数据证明过的项。
> 部署基座见 `docker-compose.prod.yml`（secrets 强制检查）。

## 一、部署前必查（按顺序打勾）

- [ ] **数据库 = PostgreSQL 15+**，禁用 SQLite 生产运行。原因：同压测 SQLite P95 8.14s vs PG 0.5–2.8s。
- [ ] **连接池配比**：`max_connections(PG)` ≥ 网关 `SQL_MAX_OPEN_CONNS`，并留 20% 给 `psql`/备份连接。反例：默认 100 vs 1000，突发直接 503 风暴（已实测）。
- [ ] **批量结算开**：`BATCH_UPDATE_ENABLED=true`（实测 3.6x 吞吐 / P95 ÷5.3）。注意：无 Redis 时批量窗口内可超批，生产必须配下一条。
- [ ] **Redis 开**：`REDIS_CONN_STRING` 配好并连通（实测再砍一半延迟至 P95 287ms；预扣走 Lua 精确账）。
- [ ] **审计日志开**：`LogConsumeEnabled=true`（关掉快 55ms，但丢资金审计链；压测后必须恢复，默认即开）。
- [ ] **Pprof 关**：生产环境不得设 `ENABLE_PPROF=true`（压测实例专用）。
- [ ] **Secrets 全换**：`POSTGRES_PASSWORD`、`REDIS_PASSWORD`、`SESSION_SECRET`（64+ 随机）、`SESSION_COOKIE_SECURE=true` + `SESSION_COOKIE_TRUSTED_URL` 精确公网 origin。`docker-compose.prod.yml` 用 `${VAR:?}` 强制拦截空值。
- [ ] **备份**：`pg_dump` 定时 + `pg_data` 卷快照；恢复演练至少做过一次。日志量大时把 `LOG_SQL_DSN` 拆到独立库/ClickHouse。
  - 已验证流程（2026-10-15 实测，10.7MB custom 格式）：
    1. `docker exec <pg> pg_dump -U root -d pgab -Fc -f /tmp/pgab-backup.dump`
    2. 新库 `pg_restore -U root -d pgab /tmp/restore.dump`，核对 `channels/tokens/logs` 行数一致
    3. 网关指新库启动，relay 冒烟 200（实测 206ms 正常区间）
  - 注意：`logs` 表是备份体积大头（压测后 32 万行）， retention 策略与备份窗口一起定。

## 二、开服运营配置（控制台，按顺序）

- [ ] 站点：`SystemName`/Logo/Footer/About、`ServerAddress`（公网地址，影响回调与邮件链接）。
- [ ] 机器人防护：图形验证码开（默认开）；高风险期叠加 Turnstile。
- [ ] 登录注册：密码登录按需开；`EmailVerificationEnabled` 按运营策略；OAuth（GitHub/LinuxDO/微信）配好 Client ID/Secret 再开开关（有前置校验，空配开不了）。
- [ ] 额度风控：新用户额度、邀请额度、信任额度、`pre_consume_multiplier` 保守起步；`ModelRequestRateLimit*` 按分组设并发上限（曾用 RLock 写 bug 已修，`setting/rate_limit.go`）。
- [ ] 定价：`ModelRatio`/`ModelPrice` 覆盖所有在售模型（缺价直接 `model_price_error` 拒单）；改价先灰度（见 billing 门禁）。
- [ ] 支付：沙箱跑通“下单→到账→分账”后再开生产网关；合规确认项先勾。
- [ ] 监控告警：`QuotaRemindThreshold`、uptime-kuma、`perf_metrics_setting`、磁盘缓存阈值；`update-checker` 开。

## 三、上线冒烟（每次发版/改配置后跑）

1. `/api/status` 200，`system_name` 正确。
2. 新建测试 key → 打一次非流 + 一次流式，确认 `finish_reason`/`[DONE]`。
3. 控制台查一条消费日志（审计链通）。
4. 小负载：k6 `GW_VUS=10 GW_DURATION=30s`，失败率必须 0（阈值已写进 `perf/k6-chat-completions.js`）。
5. 用完即删测试 key（剩余额度不留尾巴）。

## 四、已知坑位（压测亲历，防复踩）

| 坑 | 症状 | 解法 |
|---|---|---|
| PG 默认 100 连接 | 突发 503，20µs 速拒 | PG `max_connections` ≥ 网关池 |
| 额度打负数且锁死 | `remain_quota` 负数 + status=4，补额不回血 |  DB `status=1` 手动复位；根治需单独立项（无下限 bug） |
| 建 key 包 `{"token":{...}}` | 静默建出空 name/0 额度行 | 用扁平体（控制台 drawer 即如此） |
| 测试 key status≠1 | `Invalid token`，与额度无关 | 先查 `tokens` 表 status 列 |
| 单机压测 503 风暴 | mock 本体被饿死（5ms 活干出 2s） | 压测机与被测机分离；附 mock 直测延迟 |

## 五、性能基线（100VU×双场景×60s，mock 上游）

| 配置 | RPS | P50 | P95 |
|---|---|---|---|
| PG + batch + redis（生产推荐态） | 934 | 210ms | 287ms |
| 上同 −consume-log | 1265 | 154ms | 222ms |
| Mock 直连（天花板） | 16165 | 7ms | 54ms |

回归线：P95 超过 **500ms** 即视为退化，拉 profile 按本文方法定位后再谈优化。

## 六、密钥轮换手册（SESSION_SECRET）

影响面（已核查源码，`service/auth_token.go`、`model/user_session.go`、
`model/auth_flow.go`、`model/twofa_enrollment.go`、`model/passkey_option.go`）：
- 会话 JWT、登录 session、进行中的 auth flow / 2FA 绑定 / passkey 删除确认**全部失效**，即所有浏览器用户需重新登录一次。
- 任务产物 URL 走独立的 `CRYPTO_SECRET`（overlay 已强制与 SESSION_SECRET 不同值），轮换不影响。
- API 令牌（sk-...）、渠道密钥、用户密码哈希**不受影响**。

步骤（低峰期，提前公告）：
1. 生成新值（64+ 随机字符），只换 `SESSION_SECRET`，`CRYPTO_SECRET` 不动。
2. `docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d` 重启。
3. 验证：新登录成功；旧会话请求返回 401；任务旧链接仍可访问。
4. 观察 15 分钟错误率，无异常即完成。

说明：零中断双 secret 灰度需要动登录热路径（5 处 HMAC 点），收益仅是省一次全员重登，
风险收益比不划算，故采用文档化重启轮换；每年最多一次即可。
