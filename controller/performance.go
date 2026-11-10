package controller

import (
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetPerformanceStats 获取性能统计信息
func GetPerformanceStats(c *gin.Context) {
	stats := service.GetPerformanceStats()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    stats,
	})
}

// ClearDiskCache 清理不活跃的磁盘缓存
func ClearDiskCache(c *gin.Context) {
	// 清理超过 10 分钟未使用的缓存文件
	// 10 分钟是一个安全的阈值，确保正在进行的请求不会被误删
	err := common.CleanupOldDiskCacheFiles(10 * time.Minute)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccessT(c, "Inactive disk cache cleared", nil)
}

// ResetPerformanceStats 重置性能统计
func ResetPerformanceStats(c *gin.Context) {
	common.ResetDiskCacheStats()

	common.ApiSuccessT(c, "Statistics reset", nil)
}

// ForceGC 强制执行 GC
func ForceGC(c *gin.Context) {
	runtime.GC()

	common.ApiSuccessT(c, "GC executed", nil)
}

// GetLogFiles 获取日志文件列表
func GetLogFiles(c *gin.Context) {
	if *common.LogDir == "" {
		common.ApiSuccess(c, service.LogFilesResponse{Enabled: false})
		return
	}
	files, err := service.ListLogFiles()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, service.BuildLogFilesResponse(files))
}

// CleanupLogFiles 清理过期日志文件
func CleanupLogFiles(c *gin.Context) {
	mode := c.Query("mode")
	valueStr := c.Query("value")
	if mode != "by_count" && mode != "by_days" {
		common.ApiErrorMsg(c, "invalid mode, must be by_count or by_days")
		return
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil || value < 1 {
		common.ApiErrorMsg(c, "invalid value, must be a positive integer")
		return
	}
	if *common.LogDir == "" {
		common.ApiErrorMsg(c, "log directory not configured")
		return
	}

	result, err := service.CleanupLogFiles(mode, value)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if len(result.FailedFiles) > 0 {
		msg := common.NewMessage("Some files could not be deleted ({{failed}}/{{total}})", map[string]any{
			"failed": len(result.FailedFiles),
			"total":  result.TotalPlanned,
		})
		common.ApiErrorStatus(c, http.StatusOK, msg, gin.H{"data": result})
		return
	}

	common.ApiSuccess(c, result)
}
