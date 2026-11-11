package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// FetchUpstreamRatios fetches ratio configs from the requested upstreams (or the
// channels behind the given ids), diffs them against local pricing, and renders
// the differences plus candidate prices. The transport, multi-format parsing and
// diffing pipeline lives in service.FetchUpstreamRatios.
func FetchUpstreamRatios(c *gin.Context) {
	var req dto.UpstreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.SysError(common.LogText("failed to bind upstream request: %s", err.Error()))
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid request parameter format"))
		return
	}

	upstreams, ok := resolveUpstreams(c, req)
	if !ok {
		return
	}

	result, err := service.FetchUpstreamRatios(c.Request.Context(), upstreams, req.Timeout)
	if err != nil {
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Failed to fetch upstream ratios"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"differences":  result.Differences,
			"prices":       result.Prices,
			"test_results": result.TestResults,
		},
	})
}

// resolveUpstreams maps the request onto upstream targets: the explicit
// upstreams when given, otherwise the channels referenced by id that expose a
// base URL. It writes the error response and reports false when the request
// yields no usable target.
func resolveUpstreams(c *gin.Context, req dto.UpstreamRequest) ([]dto.UpstreamDTO, bool) {
	var upstreams []dto.UpstreamDTO

	if len(req.Upstreams) > 0 {
		for _, u := range req.Upstreams {
			if !strings.HasPrefix(u.BaseURL, "http") {
				continue
			}
			// 请求体显式提供的 upstream 属于「调用方可控外发目标」：
			// BaseURL 与 Endpoint（可为完整 http(s) URL）都必须经过统一 SSRF
			// 校验，否则可借 root 会话探测云元数据/内网服务并回显状态。
			// 从数据库渠道读取的 BaseURL 不在此校验范围内——那是管理员配置的
			// 部署目标，项目允许其指向内网/私有链路（见 service/http_client.go）。
			for _, candidate := range []string{u.BaseURL, u.Endpoint} {
				if !strings.HasPrefix(candidate, "http://") && !strings.HasPrefix(candidate, "https://") {
					continue
				}
				if err := service.ValidateSSRFProtectedFetchURL(candidate); err != nil {
					common.ApiErrorT(c, "Upstream URL is not allowed by the server's fetch policy")
					return nil, false
				}
			}
			if u.Endpoint == "" {
				u.Endpoint = service.DefaultEndpoint
			}
			u.BaseURL = strings.TrimRight(u.BaseURL, "/")
			upstreams = append(upstreams, u)
		}
	} else if len(req.ChannelIDs) > 0 {
		intIds := make([]int, 0, len(req.ChannelIDs))
		for _, id64 := range req.ChannelIDs {
			intIds = append(intIds, int(id64))
		}
		dbChannels, err := model.GetChannelsByIds(intIds)
		if err != nil {
			logger.LogError(c.Request.Context(), common.LogText("failed to query channels: %s", err.Error()))
			common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Failed to query channels"))
			return nil, false
		}
		for _, ch := range dbChannels {
			if base := ch.GetBaseURL(); strings.HasPrefix(base, "http") {
				upstreams = append(upstreams, dto.UpstreamDTO{
					ID:       ch.Id,
					Name:     ch.Name,
					BaseURL:  strings.TrimRight(base, "/"),
					Endpoint: "",
				})
			}
		}
	}

	if len(upstreams) == 0 {
		common.ApiErrorT(c, "No valid upstream channel")
		return nil, false
	}
	return upstreams, true
}

// GetSyncableChannels lists the channels eligible for ratio sync plus the
// built-in upstream presets.
func GetSyncableChannels(c *gin.Context) {
	channels, err := service.ListSyncableChannels()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	common.ApiSuccess(c, channels)
}
