package common

import (
	"math/rand/v2"
	"time"
)

// 后台周期任务的错峰抖动。
//
// 多个实例若使用完全相同的同步周期并在同一时刻启动，会在每秒的同一瞬间
// 同时全表扫描主库（SyncChannelCache / SyncOptions / BatchUpdater /
// TaskPluginSync）。在实例数较多时，这会把周期性的全表读叠加成尖峰，
// 推高主库负载与 P99 延迟。给每轮间隔加一个小的随机抖动即可把尖峰摊平，
// 且不改变平均同步频率（抖动关于基准对称）。
//
// 这里使用 math/rand/v2 的包级函数：它并发安全、无需播种，也不需要把随机源
// 注入到每个调用点。抖动只用于「何时同步」，绝不参与任何安全或计费判定。

// syncJitterDivisor 决定抖动幅度：基准 / 5 的总跨度，即 ±10%。
const syncJitterDivisor = 5

// PeriodicSyncInterval 返回带 ±10% 错峰抖动的同步间隔。
// base 非正时返回 1 秒，避免 Sleep(0) 造成忙循环打满 CPU。
func PeriodicSyncInterval(base time.Duration) time.Duration {
	if base <= 0 {
		return time.Second
	}
	spread := base / syncJitterDivisor
	if spread <= 0 {
		return base
	}
	// rand.N 返回 [0, spread)，减去 spread/2 得到关于基准对称的偏移。
	jitter := time.Duration(rand.N(int64(spread))) - spread/2
	return max(base+jitter, time.Millisecond)
}
