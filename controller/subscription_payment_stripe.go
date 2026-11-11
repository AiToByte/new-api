package controller

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/thanhpk/randstr"
)

type SubscriptionStripePayRequest struct {
	PlanId int `json:"plan_id"`
}

func SubscriptionRequestStripePay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	var req SubscriptionStripePayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}

	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorT(c, "Subscription plan is not enabled")
		return
	}
	if plan.StripePriceId == "" {
		common.ApiErrorT(c, "StripePriceId is not configured for this plan")
		return
	}
	if !strings.HasPrefix(setting.StripeApiSecret, "sk_") && !strings.HasPrefix(setting.StripeApiSecret, "rk_") {
		common.ApiErrorT(c, "Stripe is not configured or key is invalid")
		return
	}
	if setting.StripeWebhookSecret == "" {
		common.ApiErrorT(c, "Stripe webhook is not configured")
		return
	}

	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user == nil {
		common.ApiErrorT(c, "User does not exist")
		return
	}

	if plan.MaxPurchasePerUser > 0 {
		count, err := model.CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			common.ApiErrorT(c, "Purchase limit for this plan has been reached")
			return
		}
	}

	reference := fmt.Sprintf("sub-stripe-ref-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	referenceId := "sub_ref_" + common.Sha1([]byte(reference))

	// 先落单、后创建 Checkout Session：避免 webhook 早于订单入库导致
	// 「已收款、订阅未开通」，且旧实现无条件 2xx 会让渠道停止重试。
	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         referenceId,
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	// Stripe 订阅由渠道侧 Price 定价，本地 plan.PriceAmount 不是渠道实付的
	// 权威来源，故不写金额快照；但仍绑定币种，且 client_reference_id +
	// 渠道签名保证事件与订单一一对应。
	order.SetExpectedPayment("", strings.ToUpper(strings.TrimSpace(plan.Currency)), "")
	if err := order.Insert(); err != nil {
		paymentError(c, common.NewMessage("Failed to create order"))
		return
	}

	payLink, err := service.GenStripeSubscriptionLink(referenceId, user.StripeCustomer, user.Email, plan.StripePriceId)
	if err != nil {
		// 会话创建失败时订单不可能被付款，立即置为失效，避免 pending 孤儿单。
		if expireErr := model.ExpireSubscriptionOrder(referenceId, model.PaymentProviderStripe); expireErr != nil {
			logger.LogError(c.Request.Context(), common.LogText("Stripe failed to expire orphan subscription order trade_no=%s error=%q", referenceId, expireErr.Error()))
		}
		logger.LogError(c.Request.Context(), common.LogText("Stripe failed to create subscription payment link trade_no=%s plan_id=%d error=%q", referenceId, plan.Id, err.Error()))
		paymentError(c, common.NewMessage("Failed to start payment"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"pay_link": payLink,
		},
	})
}
