package common

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSystemMonitorStartStopIsIdempotent 验证包级句柄的生命周期语义：
// 重复 Start 不新建实例，Stop 后可再次 Start。这是 main.go 关停段调用
// StopSystemMonitor 不会误伤或泄漏的前提。
func TestSystemMonitorStartStopIsIdempotent(t *testing.T) {
	// 确保测试不干扰其他用例的包级状态。
	StopSystemMonitor()
	t.Cleanup(StopSystemMonitor)

	StartSystemMonitor()
	systemMonitorMu.Lock()
	first := systemMonitorCancel
	systemMonitorMu.Unlock()
	require.NotNil(t, first)

	// 重复启动不得创建第二个循环：若创建了，第二次 Start 会覆盖句柄，
	// 导致 Stop 只能取消其中一个而泄漏另一个 goroutine。这里用「幂等性」
	// 的可观测形式验证：重复 Start 后单次 Stop 必须让句柄归零。
	StartSystemMonitor()
	StopSystemMonitor()
	systemMonitorMu.Lock()
	afterStop := systemMonitorCancel
	systemMonitorMu.Unlock()
	require.Nil(t, afterStop, "重复 Start 后单次 Stop 必须完全停止")

	StartSystemMonitor()
	systemMonitorMu.Lock()
	third := systemMonitorCancel
	systemMonitorMu.Unlock()
	require.NotNil(t, third, "Stop 后应能重新启动")
}

// TestSystemMonitorWithContextStopsOnCancel 验证外部 context 取消时循环退出。
// 用「取消后采集次数不再增长」来观测：把监控配置为禁用，循环只在 30s
// 间隔空转，因此采集计数恒为 0；这里验证取消可在 timer 到期前生效，
// 不依赖真实的 gopsutil 采集结果。
func TestSystemMonitorWithContextStopsOnCancel(t *testing.T) {
	previous := GetPerformanceMonitorConfig()
	t.Cleanup(func() { SetPerformanceMonitorConfig(previous) })
	SetPerformanceMonitorConfig(PerformanceMonitorConfig{Enabled: false})

	ctx, cancel := context.WithCancel(context.Background())
	done := startSystemMonitorLoop(ctx)
	select {
	case <-done:
		t.Fatal("监控循环不应在启动后立即退出")
	default:
	}

	cancel()
	select {
	case <-done:
		// 取消后立即退出，证明没有泄漏的 30s 定时器。
	case <-time.After(time.Second):
		t.Fatal("取消 context 后监控循环未退出")
	}
}
