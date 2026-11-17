package common

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
)

// DiskSpaceInfo 磁盘空间信息
type DiskSpaceInfo struct {
	// 总空间（字节）
	Total uint64 `json:"total"`
	// 可用空间（字节）
	Free uint64 `json:"free"`
	// 已用空间（字节）
	Used uint64 `json:"used"`
	// 使用百分比
	UsedPercent float64 `json:"used_percent"`
}

// SystemStatus 系统状态信息
type SystemStatus struct {
	CPUUsage    float64
	MemoryUsage float64
	DiskUsage   float64
}

var latestSystemStatus atomic.Value

func init() {
	latestSystemStatus.Store(SystemStatus{})
}

var (
	systemMonitorMu     sync.Mutex
	systemMonitorCancel context.CancelFunc
)

// StartSystemMonitor 用包级句柄启动系统监控，保留既有调用范式。
// 重复调用是幂等的：已有实例在运行时不再新建协程。
func StartSystemMonitor() {
	systemMonitorMu.Lock()
	defer systemMonitorMu.Unlock()
	if systemMonitorCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	systemMonitorCancel = cancel
	startSystemMonitorLoop(ctx)
}

// StopSystemMonitor 在进程关停时停止监控协程并释放定时器。
// 与 StartSystemMonitor 配对，供 main.go 的关停段调用。
func StopSystemMonitor() {
	systemMonitorMu.Lock()
	cancel := systemMonitorCancel
	systemMonitorCancel = nil
	systemMonitorMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// startSystemMonitorLoop 在后台协程中运行可取消的监控循环。
//
// 旧实现是无退出路径的 for{}：即使监控被禁用（Enabled=false）也每 30s
// 空转一次，关停时协程与定时器都无法回收。现在启用时按监控间隔采集，
// 禁用时按 30s 低成本轮询配置变化，两种情况都响应 ctx.Done()。
//
// 配置间隔在运行期可变，因此按轮重建 timer 而非使用固定 ticker：
// 管理员调整采集间隔能立即生效，且每轮都检查取消信号。
func startSystemMonitorLoop(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		const disabledPollInterval = 30 * time.Second
		for {
			config := GetPerformanceMonitorConfig()
			interval := disabledPollInterval
			if config.Enabled {
				interval = 5 * time.Second
				updateSystemStatus()
			}
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return done
}

func updateSystemStatus() {
	var status SystemStatus

	// CPU
	// 注意：cpu.Percent(0, false) 返回自上次调用以来的 CPU 使用率
	// 如果是第一次调用，可能会返回错误或不准确的值，但在循环中会逐渐正常
	percents, err := cpu.Percent(0, false)
	if err == nil && len(percents) > 0 {
		status.CPUUsage = percents[0]
	}

	// Memory
	memInfo, err := mem.VirtualMemory()
	if err == nil {
		status.MemoryUsage = memInfo.UsedPercent
	}

	// Disk
	diskInfo := GetDiskSpaceInfo()
	if diskInfo.Total > 0 {
		status.DiskUsage = diskInfo.UsedPercent
	}

	latestSystemStatus.Store(status)
}

// GetSystemStatus 获取当前系统状态
func GetSystemStatus() SystemStatus {
	return latestSystemStatus.Load().(SystemStatus)
}
