# Load baseline: mock upstream, 2026-10-15

Gateway: aitobyte:local on custom/main (P1-P3 + captcha + brand), SQLite backend,
ENABLE_PPROF=true. Upstream: perf/mock-upstream.go (5ms JSON / 5x10ms SSE).

k6: 100 VU x {stream, nonstream} x 60s, model mock-gpt, GW_API_TOKEN test key.

## Macro (k6 summary)
- requests: 4502, failed: 0.00%, checks: 9004/9004
- throughput: ~69.9 rps
- http_req_duration: min 14ms / med 1.91s / avg 2.73s / p90 6.41s / p95 8.14s / max 20.2s
- upstream latency for same payloads: ~5-60ms
- => gateway adds ~2-8s per request under 200-way concurrency.

## CPU (30s pprof during run)
- Total samples: 30ms (0.1%) => NOT CPU-bound. Only background
  SyncTaskPlugins + sqlite open/close visible. Bottleneck is serialization,
  not computation.

## Heap alloc_space (after run, cum 1195MB)
- relay/helper.NewStreamScanner: 131.9MB (11.0%)
- gorm AddClause/Scan/getInstance/clone combined: ~200MB (~17%)
- => per-request ORM churn + stream scanner dominate allocation.

## Goroutines
- before: 25, after: 23 => no leak across 4502 relayed requests.

## Real-model reference (Atria-Dawn-Preview, same gateway)
- Upstream itself queues 2.5-69s and 429s past ~10 concurrent; gateway
  passes 429 through. 200VU against real models measures the upstream,
  not the gateway. Mock numbers above are the gateway's own physique.

## Implied Phase 4 direction (gated, not started)
- JSON engine swap: NO EVIDENCE (CPU idle). Do not pursue.
- Candidates with evidence: SQLite single-writer serialization under
  concurrent consume-log writes (needs PG A/B to isolate), stream-scanner
  allocs (needs micro-benchmark), per-request GORM churn.

---

## PG A/B result (same script, PG15, max_connections=300, 2026-10-15)

- PG run: 10,252 req, 0 failed, 167 rps;
  P50 986ms / P90 2.32s / P95 2.8s / max 6.96s (vs SQLite P95 8.14s).
- Mock direct (no gateway): 16,165 rps, P50 7ms / P95 54ms.
- => PG removes ~2/3 of latency but the gateway still adds ~1000ms/req
  at 200-way concurrency vs 7ms direct. Remaining cost is NOT the
  database engine: CPU profile under full load shows ~0% application CPU
  (only runtime scavenger), so the ~1s is WAIT, not compute.
- Prime suspects for the wait: same-row UPDATE serialization (all 200
  concurrent requests UPDATE the same token/users rows for quota settle),
  per-request synchronous consume-log INSERTs, app-level mutexes.
  GORM+pgy alloc: 11GB cumulative for 10k req (~1MB/req), dominated by
  Scan/AddClause/BuildQuerySQL/pgx rows — ORM churn confirmed.
- Goroutines 25 -> 23 across the run: no leak.
- Incidents during A/B (all resolved, recorded for honesty):
  - Fresh PG default max_connections=100 vs app pool 1000 => burst 503
    storm ("too many clients"). Fixed by max_connections=300. PRODUCTION
    NOTE: default PG 100 will 503 under bursts; align pool sizes.
  - Test key quota driven to -93.5M (negative! no floor) => status=4
    Exhausted; topping quota does NOT reset status, required manual
    status=1. Billing edge worth a separate look.
  - Token create API takes a FLAT payload (embedded struct promotion);
    nested {"token": {...}} silently creates empty rows.
- Revised Phase 4 direction: per-request synchronous settle path
  (quota/log writes, same-row contention), read-path query reduction,
  stream-scanner allocs. JSON engine stays out.

---

## Batch A/B verdict (100VU x {stream,nonstream} x 60s, PG15, 2026-10-15)

- BATCH_UPDATE_ENABLED=true: 548 rps, P50 322ms / P95 578ms, 99.92%
  (48/66078 transient fails).
- BATCH_UPDATE_ENABLED unset: 152 rps, P50 1.09s / P95 3.07s, 100%.
- => Batching async quota/channel writes: 3.6x throughput, 5.3x P95.
  Caveat: OFF run followed a container restart (colder caches); effect
  size dwarfs warm-up noise. Production recommendation: enable batch
  updates (compose already defaults it on) AND run Redis (reserve path
  stays exact via Lua; without Redis, batch windows can over-approve).

---

## A/B verdict + measurement-environment caveat (2026-10-15, appended)

- SQLite: P50 1.91s / P95 8.14s @200VU. PG: P50 0.3-1.0s / P95 0.5-2.8s
  across runs (100% clean at 100VUx20s and 200VU cold). Direction holds:
  SQLite single-writer is the primary bottleneck.
- Episodic 503 storms (66-100% fast-fail, ~20µs, instant recovery) were
  chased across 6 runs and attributed to the MEASUREMENT environment,
  not gateway code: single Windows box runs k6 + Docker VM + PG + mock
  simultaneously; the mock itself degrades to avg 162ms / max 2128ms
  for a 5ms handler under full load, cascading into gateway upstream
  timeouts. Proven by: instant post-run recovery, clean low-load runs,
  zero goroutine growth (25->23), zero PG fatals after max_conn=300,
  no error logs beyond GIN access lines.
- Consequently the earlier BATCH_UPDATE_ENABLED=true run is POLLUTED (ran
  inside a storm window). SUPERSEDED by the clean batch ON/OFF A/B below:
  batching proven at 3.6x throughput / 5.3x P95.
- Methodology fix going forward: micro-benchmarks (box-independent) for
  alloc/CPU claims; k6 only for directional A/B at moderate load
  (<=100VU verified clean); always probe mock-direct latency during runs
  to detect box saturation.

---

## Full ladder, same 100VU profile (PG15 unless noted)

| Config | RPS | P50 | P95 | Success |
|---|---|---|---|---|
| SQLite, batch off, no redis | 70 | 1.91s | 8.14s | 100% |
| PG, batch off, no redis | 152 | 1.09s | 3.07s | 100% |
| PG, batch on, no redis | 548 | 322ms | 578ms | 99.92% |
| PG, batch on, redis | 934 | 210ms | 287ms | 100% |
| PG, batch on, redis, logs off | 1265 | 154ms | 222ms | 100% |
| Mock direct (no gateway) | 16165 | 7ms | 54ms | 100% |

- Consume-log INSERT + GetUserSetting read cost ~55ms P50/P95. Logs were
  re-enabled immediately after measurement (audit trail must stay on).
- Micro-benchmarks (box-independent): JSON 2KB unmarshal ~5.7µs/19allocs,
  marshal ~3µs/2allocs; 128KB ~280µs. Engine-swap ceiling ≈ 10µs/req:
  CLOSED, do not pursue. SQLite same-row UPDATE ~44µs/81allocs: single
  writes are cheap, queueing under concurrency is the cost.
- Residual ~150ms P50 (PG+batch+redis+logs) = necessary synchronous work
  (auth/channel/ability reads, relay, audit INSERT) + docker NAT + k6.
  CPU idle throughout: no compute hotspot remains. Further code work
  here has diminishing returns vs billing risk; next lever (if ever
  needed) is read-path cache-hit measurement, not rewrites.
