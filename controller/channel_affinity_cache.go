package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetChannelAffinityCacheStats(c *gin.Context) {
	stats := service.GetChannelAffinityCacheStats()
	common.ApiSuccess(c, stats)
}

// GetReadCacheStats 返回请求级读路径（token/user/channel）的缓存命中计数，
// 用于定位缓存未命中造成的数据库压力。Root 权限，见路由 optionRoute。
func GetReadCacheStats(c *gin.Context) {
	common.ApiSuccess(c, service.ReadCacheStats())
}

func ClearChannelAffinityCache(c *gin.Context) {
	all := strings.TrimSpace(c.Query("all"))
	ruleName := strings.TrimSpace(c.Query("rule_name"))

	if all == "true" {
		deleted := service.ClearChannelAffinityCacheAll()
		common.ApiSuccess(c, gin.H{
			"deleted": deleted,
		})
		return
	}

	if ruleName == "" {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Missing parameter rule_name, or use all=true to clear everything"))
		return
	}

	deleted, err := service.ClearChannelAffinityCacheByRuleName(ruleName)
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"deleted": deleted,
	})
}

func GetChannelAffinityUsageCacheStats(c *gin.Context) {
	ruleName := strings.TrimSpace(c.Query("rule_name"))
	usingGroup := strings.TrimSpace(c.Query("using_group"))
	keyFp := strings.TrimSpace(c.Query("key_fp"))

	if ruleName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "missing param: rule_name",
		})
		return
	}
	if keyFp == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "missing param: key_fp",
		})
		return
	}

	stats := service.GetChannelAffinityUsageCacheStats(ruleName, usingGroup, keyFp)
	common.ApiSuccess(c, stats)
}
