package common

import (
	"testing"
	"time"
)

// TestPeriodicSyncIntervalStaysWithinBounds 保证抖动不会让同步间隔偏离基准
// 太多（±10%），从而维持可预期的平均同步频率。
func TestPeriodicSyncIntervalStaysWithinBounds(t *testing.T) {
	base := 60 * time.Second
	lower := base - base/syncJitterDivisor/2
	upper := base + base/syncJitterDivisor/2

	for range 1000 {
		got := PeriodicSyncInterval(base)
		if got < lower || got > upper {
			t.Fatalf("同步间隔越界: got=%s want [%s, %s]", got, lower, upper)
		}
	}
}

// TestPeriodicSyncIntervalActuallyJitters 确认间隔确实被打散，否则多节点
// 仍会在同一瞬间一起打主库。
func TestPeriodicSyncIntervalActuallyJitters(t *testing.T) {
	base := 60 * time.Second
	seen := make(map[time.Duration]bool)
	for range 200 {
		seen[PeriodicSyncInterval(base)] = true
	}
	if len(seen) < 10 {
		t.Fatalf("抖动不足，仅产生 %d 种间隔", len(seen))
	}
}

// TestPeriodicSyncIntervalGuardsNonPositiveBase 防止配置错误导致 Sleep(0)
// 忙循环打满 CPU。
func TestPeriodicSyncIntervalGuardsNonPositiveBase(t *testing.T) {
	for _, base := range []time.Duration{0, -time.Second, time.Nanosecond} {
		if got := PeriodicSyncInterval(base); got <= 0 {
			t.Fatalf("base=%s 得到非正间隔 %s", base, got)
		}
	}
	if got := PeriodicSyncInterval(time.Nanosecond); got != time.Nanosecond {
		t.Fatalf("极小正间隔应原样返回，实际 %s", got)
	}
}
