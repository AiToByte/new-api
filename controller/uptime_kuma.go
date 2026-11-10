package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetUptimeKumaStatus renders the aggregated Uptime Kuma status pages. The
// HTTP fan-out, decoding and projection live in service.FetchUptimeKumaGroups.
func GetUptimeKumaStatus(c *gin.Context) {
	common.ApiSuccess(c, service.FetchUptimeKumaGroups(c.Request.Context()))
}
