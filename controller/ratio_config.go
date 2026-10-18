package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func GetRatioConfig(c *gin.Context) {
	if !ratio_setting.IsExposeRatioEnabled() {
		common.ApiErrorStatus(c, http.StatusForbidden, common.NewMessage("The ratio config API is not enabled"))
		return
	}

	common.ApiSuccess(c, billing_setting.GetPricingSyncData(map[string]any(ratio_setting.GetExposedData())))
}
