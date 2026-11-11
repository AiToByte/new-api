package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/webhook"
	"github.com/thanhpk/randstr"
)

var stripeAdaptor = &StripeAdaptor{}

// StripePayRequest represents a payment request for Stripe checkout.
type StripePayRequest struct {
	// Amount is the quantity of units to purchase.
	Amount int64 `json:"amount"`
	// PaymentMethod specifies the payment method (e.g., "stripe").
	PaymentMethod string `json:"payment_method"`
	// SuccessURL is the optional custom URL to redirect after successful payment.
	// If empty, defaults to the server's console log page.
	SuccessURL string `json:"success_url,omitempty"`
	// CancelURL is the optional custom URL to redirect when payment is canceled.
	// If empty, defaults to the server's console topup page.
	CancelURL string `json:"cancel_url,omitempty"`
}

type StripeAdaptor struct {
}

func (*StripeAdaptor) RequestAmount(c *gin.Context, req *StripePayRequest) {
	if req.Amount < getStripeMinTopup() {
		paymentError(c, common.NewMessage("Top-up amount cannot be less than {{min}}", map[string]any{"min": getStripeMinTopup()}))
		return
	}
	if req.Amount > 10000 {
		paymentError(c, common.NewMessage("Top-up amount cannot exceed {{max}}", map[string]any{"max": 10000}))
		return
	}
	id := c.GetInt("id")
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		paymentError(c, common.NewMessage("Failed to get the user group"))
		return
	}
	if rejectInvalidCreditedQuota(c, id, getStripeCreditedQuota(req.Amount, group)) {
		return
	}
	payMoney := getStripePayMoney(float64(req.Amount), group)
	if payMoney <= 0.01 {
		paymentError(c, common.NewMessage("Payment amount is too low"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": strconv.FormatFloat(payMoney, 'f', 2, 64)})
}

func (*StripeAdaptor) RequestPay(c *gin.Context, req *StripePayRequest) {
	if req.PaymentMethod != model.PaymentMethodStripe {
		paymentError(c, common.NewMessage("Unsupported payment channel"))
		return
	}
	if req.Amount < getStripeMinTopup() {
		common.ApiErrorStatus(c, http.StatusOK, common.NewMessage("Top-up amount cannot be less than {{min}}", map[string]any{"min": getStripeMinTopup()}), gin.H{"data": 10})
		return
	}
	if req.Amount > 10000 {
		common.ApiErrorStatus(c, http.StatusOK, common.NewMessage("Top-up amount cannot exceed {{max}}", map[string]any{"max": 10000}), gin.H{"data": 10})
		return
	}

	if req.SuccessURL != "" && common.ValidateRedirectURL(req.SuccessURL) != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Payment success redirect URL is not in the trusted domain list"), gin.H{"data": ""})
		return
	}

	if req.CancelURL != "" && common.ValidateRedirectURL(req.CancelURL) != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Payment cancel redirect URL is not in the trusted domain list"), gin.H{"data": ""})
		return
	}

	id := c.GetInt("id")
	user, err := model.GetUserById(id, false)
	if err != nil || user == nil {
		paymentError(c, common.NewMessage("User does not exist"))
		return
	}
	chargedMoney := GetChargedAmount(float64(req.Amount), *user)
	if rejectInvalidCreditedQuota(c, id,
		decimal.NewFromFloat(chargedMoney).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
	) {
		return
	}

	reference := fmt.Sprintf("new-api-ref-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	referenceId := "ref_" + common.Sha1([]byte(reference))

	// 先落单、后创建 Checkout Session。
	// Stripe 可能在用户完成付款后立即推送 webhook，若订单尚未入库，回调会
	// 因「订单查无」被记失败；旧实现还无条件返回 2xx，导致 Stripe 停止重试，
	// 最终「收款成功、额度未入账」。订单先在库可彻底消除该竞态。
	// 若后续创建 Session 失败，把订单标记为 Failed，避免留下 pending 孤儿单。
	topUp := &model.TopUp{
		UserId:          id,
		Amount:          req.Amount,
		Money:           chargedMoney,
		TradeNo:         referenceId,
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	// 注意：Stripe 充值的 Money 是 GetChargedAmount 得到的「充值额度金额」，
	// 而渠道实付由 Stripe Price × Quantity 决定，两者不是同一概念，故此处
	// 不写金额快照。回调仍会执行「实付金额 > 0」与币种一致性校验，
	// 可拦截 100%-off 优惠券产生的 0 元 paid 事件。
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), common.LogText("Stripe failed to create top-up order user_id=%d trade_no=%s amount=%d error=%q", id, referenceId, req.Amount, err.Error()))
		paymentError(c, common.NewMessage("Failed to create order"))
		return
	}
	logger.LogInfo(c.Request.Context(), common.LogText("Stripe top-up order created user_id=%d trade_no=%s amount=%d money=%.2f", id, referenceId, req.Amount, chargedMoney))

	payLink, err := genStripeLink(referenceId, user.StripeCustomer, user.Email, req.Amount, req.SuccessURL, req.CancelURL)
	if err != nil {
		// 创建渠道会话失败时，本地订单不可能被付款，标记为 Failed 以免
		// 长期占用 pending 状态、也避免运营误判为「已收款未结算」。
		topUp.Status = common.TopUpStatusFailed
		if updateErr := topUp.Update(); updateErr != nil {
			logger.LogError(c.Request.Context(), common.LogText("Stripe failed to mark orphan top-up order as failed trade_no=%s error=%q", referenceId, updateErr.Error()))
		}
		logger.LogError(c.Request.Context(), common.LogText("Stripe failed to create Checkout Session user_id=%d trade_no=%s amount=%d error=%q", id, referenceId, req.Amount, err.Error()))
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

func RequestStripeAmount(c *gin.Context) {
	var req StripePayRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		paymentError(c, common.NewMessage("Invalid parameters"))
		return
	}
	stripeAdaptor.RequestAmount(c, &req)
}

func RequestStripePay(c *gin.Context) {
	var req StripePayRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		paymentError(c, common.NewMessage("Invalid parameters"))
		return
	}
	stripeAdaptor.RequestPay(c, &req)
}

// stripeSettlementOutcome 描述一次 Stripe 回调的结算结果，决定给渠道的响应码。
//
// 旧实现无论结算成败都返回 2xx，Stripe 收到后即停止重试；一旦出现
// 「订单尚未落库」「数据库短暂不可用」，就会造成「已收款、未入账」且
// 永远无法自动补单。现在把结果分为四类，只有临时性失败返回 5xx 让渠道重试。
type stripeSettlementOutcome int

const (
	// stripeSettlementIgnored：非结算事件或无需处理，返回 200。
	stripeSettlementIgnored stripeSettlementOutcome = iota
	// stripeSettlementSettled：结算成功，返回 200。
	stripeSettlementSettled
	// stripeSettlementRejected：金额对账等永久性拒绝，重试无意义，返回 200 并告警。
	stripeSettlementRejected
	// stripeSettlementRetryable：订单缺失、数据库抖动等临时失败，返回 503 触发重试。
	stripeSettlementRetryable
)

func StripeWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	if !service.IsStripeWebhookEnabled() {
		logger.LogWarn(ctx, common.LogText("Stripe webhook rejected reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.LogError(ctx, common.LogText("Stripe webhook failed to read request body path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}

	signature := c.GetHeader("Stripe-Signature")
	logger.LogInfo(ctx, common.LogText("Stripe webhook received request path=%q client_ip=%s signature=%q body=%q", c.Request.RequestURI, c.ClientIP(), common.LogFingerprint(signature), common.LogFingerprint(string(payload))))
	event, err := webhook.ConstructEventWithOptions(payload, signature, setting.StripeWebhookSecret, webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	})

	if err != nil {
		logger.LogWarn(ctx, common.LogText("Stripe webhook signature verification failed path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	callerIp := c.ClientIP()
	logger.LogInfo(ctx, common.LogText("Stripe webhook signature verified event_type=%s client_ip=%s path=%q", string(event.Type), callerIp, c.Request.RequestURI))
	outcome := stripeSettlementIgnored
	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted:
		outcome = sessionCompleted(ctx, event, callerIp)
	case stripe.EventTypeCheckoutSessionExpired:
		sessionExpired(ctx, event)
	case stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded:
		outcome = sessionAsyncPaymentSucceeded(ctx, event, callerIp)
	case stripe.EventTypeCheckoutSessionAsyncPaymentFailed:
		sessionAsyncPaymentFailed(ctx, event, callerIp)
	default:
		logger.LogInfo(ctx, common.LogText("Stripe webhook ignored event event_type=%s client_ip=%s", string(event.Type), callerIp))
	}

	if outcome == stripeSettlementRetryable {
		// 5xx 会让 Stripe 按退避策略重投，从而自动补上临时失败期间的订单；
		// 幂等由 model 层的行锁 + 状态 CAS 保证，重试不会重复入账。
		logger.LogWarn(ctx, common.LogText("Stripe webhook settlement temporarily failed, requesting retry event_type=%s client_ip=%s", string(event.Type), callerIp))
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Status(http.StatusOK)
}

func sessionCompleted(ctx context.Context, event stripe.Event, callerIp string) stripeSettlementOutcome {
	customerId := event.GetObjectValue("customer")
	referenceId := event.GetObjectValue("client_reference_id")
	status := event.GetObjectValue("status")
	if "complete" != status {
		logger.LogWarn(ctx, common.LogText("Stripe checkout.completed has unexpected status, ignored trade_no=%s status=%s client_ip=%s", referenceId, status, callerIp))
		return stripeSettlementIgnored
	}

	paymentStatus := event.GetObjectValue("payment_status")
	if paymentStatus != "paid" {
		logger.LogInfo(ctx, common.LogText("Stripe Checkout payment not completed, waiting for async result trade_no=%s payment_status=%s client_ip=%s", referenceId, paymentStatus, callerIp))
		return stripeSettlementIgnored
	}

	return fulfillOrder(ctx, event, referenceId, customerId, callerIp)
}

// sessionAsyncPaymentSucceeded handles delayed payment methods (bank transfer, SEPA, etc.)
// that confirm payment after the checkout session completes.
func sessionAsyncPaymentSucceeded(ctx context.Context, event stripe.Event, callerIp string) stripeSettlementOutcome {
	customerId := event.GetObjectValue("customer")
	referenceId := event.GetObjectValue("client_reference_id")
	logger.LogInfo(ctx, common.LogText("Stripe async payment succeeded trade_no=%s client_ip=%s", referenceId, callerIp))

	return fulfillOrder(ctx, event, referenceId, customerId, callerIp)
}

// sessionAsyncPaymentFailed marks orders as failed when delayed payment methods
// ultimately fail (e.g. bank transfer not received, SEPA rejected).
func sessionAsyncPaymentFailed(ctx context.Context, event stripe.Event, callerIp string) {
	referenceId := event.GetObjectValue("client_reference_id")
	logger.LogWarn(ctx, common.LogText("Stripe async payment failed trade_no=%s client_ip=%s", referenceId, callerIp))

	if len(referenceId) == 0 {
		logger.LogWarn(ctx, common.LogText("Stripe async payment failed event missing order number client_ip=%s", callerIp))
		return
	}

	LockOrder(referenceId)
	defer UnlockOrder(referenceId)

	topUp := model.GetTopUpByTradeNo(referenceId)
	if topUp == nil {
		logger.LogWarn(ctx, common.LogText("Stripe async payment failed but local order not found trade_no=%s client_ip=%s", referenceId, callerIp))
		return
	}

	if topUp.PaymentProvider != model.PaymentProviderStripe {
		logger.LogWarn(ctx, common.LogText("Stripe async payment failed but order payment gateway mismatch trade_no=%s payment_provider=%s client_ip=%s", referenceId, topUp.PaymentProvider, callerIp))
		return
	}

	if topUp.Status != common.TopUpStatusPending {
		logger.LogInfo(ctx, common.LogText("Stripe async payment failed but order not pending, ignored trade_no=%s status=%s client_ip=%s", referenceId, topUp.Status, callerIp))
		return
	}

	topUp.Status = common.TopUpStatusFailed
	if err := topUp.Update(); err != nil {
		logger.LogError(ctx, common.LogText("Stripe failed to mark top-up order as failed trade_no=%s client_ip=%s error=%q", referenceId, callerIp, err.Error()))
		return
	}
	logger.LogInfo(ctx, common.LogText("Stripe top-up order marked as failed trade_no=%s client_ip=%s", referenceId, callerIp))
}

// stripePaidAmount 从「已验签」的 Stripe 事件提取实付金额。
//
// amount_total 是最小货币单位（分），需除以 100 转成主单位。若事件不带该字段
// （例如部分异步支付成功事件），返回 AmountKnown=false，让对账只做币种校验并
// 跳过金额比对，避免误拒合法事件；明确为 0 时才会在对账中被拒绝。
// 金额一律用 decimal 解析，避免浮点误差把 0 元解析成极小正数。
func stripePaidAmount(event stripe.Event) *service.PaidAmount {
	paid := &service.PaidAmount{Currency: strings.ToUpper(strings.TrimSpace(event.GetObjectValue("currency")))}
	raw := strings.TrimSpace(event.GetObjectValue("amount_total"))
	if raw == "" {
		return paid
	}
	minor, err := decimal.NewFromString(raw)
	if err != nil {
		return paid
	}
	paid.AmountKnown = true
	paid.Amount = minor.Div(decimal.NewFromInt(100))
	return paid
}

// fulfillOrder 是支付确认后入账的共享逻辑。
// 返回值决定 webhook 响应码：永久性拒绝（金额对账）不重试，临时性失败
// （订单缺失/DB 抖动）返回 5xx 让 Stripe 重投，成功或忽略返回 2xx。
func fulfillOrder(ctx context.Context, event stripe.Event, referenceId string, customerId string, callerIp string) stripeSettlementOutcome {
	if len(referenceId) == 0 {
		logger.LogWarn(ctx, common.LogText("Stripe order completion missing order number client_ip=%s", callerIp))
		return stripeSettlementIgnored
	}

	LockOrder(referenceId)
	defer UnlockOrder(referenceId)
	// 结算前必须携带已验签的实付金额，由 service 统一核对金额/币种，
	// 防止 100%-off 优惠券产生 paid 但 0 元的事件全额入账。
	paid := stripePaidAmount(event)
	payload := map[string]any{
		"customer":     customerId,
		"amount_total": event.GetObjectValue("amount_total"),
		"currency":     strings.ToUpper(event.GetObjectValue("currency")),
		"event_type":   string(event.Type),
	}
	if err := service.CompleteSubscriptionOrder(referenceId, common.GetJsonString(payload), model.PaymentProviderStripe, "", paid); err == nil {
		logger.LogInfo(ctx, common.LogText("Stripe subscription order processed trade_no=%s event_type=%s client_ip=%s", referenceId, string(event.Type), callerIp))
		return stripeSettlementSettled
	} else if !errors.Is(err, model.ErrSubscriptionOrderNotFound) {
		// 金额对账拒绝属于安全事件：记录专用告警，且不回退到充值结算。
		if service.IsPaymentAmountRejected(err) {
			logger.LogWarn(ctx, common.LogText("Stripe settlement rejected by amount reconciliation trade_no=%s event_type=%s client_ip=%s reason=%q", referenceId, string(event.Type), callerIp, err.Error()))
			return stripeSettlementRejected
		}
		logger.LogError(ctx, common.LogText("Stripe subscription order processing failed trade_no=%s event_type=%s client_ip=%s error=%q", referenceId, string(event.Type), callerIp, err.Error()))
		return stripeSettlementRetryable
	}

	err := service.Recharge(referenceId, customerId, callerIp, paid)
	if err != nil {
		if service.IsPaymentAmountRejected(err) {
			logger.LogWarn(ctx, common.LogText("Stripe top-up settlement rejected by amount reconciliation trade_no=%s event_type=%s client_ip=%s reason=%q", referenceId, string(event.Type), callerIp, err.Error()))
			return stripeSettlementRejected
		}
		logger.LogError(ctx, common.LogText("Stripe top-up processing failed trade_no=%s event_type=%s client_ip=%s error=%q", referenceId, string(event.Type), callerIp, err.Error()))
		return stripeSettlementRetryable
	}

	total, _ := strconv.ParseFloat(event.GetObjectValue("amount_total"), 64)
	currency := strings.ToUpper(event.GetObjectValue("currency"))
	logger.LogInfo(ctx, common.LogText("Stripe top-up succeeded trade_no=%s amount_total=%.2f currency=%s event_type=%s client_ip=%s", referenceId, total/100, currency, string(event.Type), callerIp))
	return stripeSettlementSettled
}

func sessionExpired(ctx context.Context, event stripe.Event) {
	referenceId := event.GetObjectValue("client_reference_id")
	status := event.GetObjectValue("status")
	if "expired" != status {
		logger.LogWarn(ctx, common.LogText("Stripe checkout.expired has unexpected status, ignored trade_no=%s status=%s", referenceId, status))
		return
	}

	if len(referenceId) == 0 {
		logger.LogWarn(ctx, common.LogText("Stripe checkout.expired missing order number"))
		return
	}

	// Subscription order expiration
	LockOrder(referenceId)
	defer UnlockOrder(referenceId)
	if err := model.ExpireSubscriptionOrder(referenceId, model.PaymentProviderStripe); err == nil {
		logger.LogInfo(ctx, common.LogText("Stripe subscription order expired trade_no=%s", referenceId))
		return
	} else if err != nil && !errors.Is(err, model.ErrSubscriptionOrderNotFound) {
		logger.LogError(ctx, common.LogText("Stripe subscription order expiry processing failed trade_no=%s error=%q", referenceId, err.Error()))
		return
	}

	err := model.UpdatePendingTopUpStatus(referenceId, model.PaymentProviderStripe, common.TopUpStatusExpired)
	if errors.Is(err, model.ErrTopUpNotFound) {
		logger.LogWarn(ctx, common.LogText("Stripe top-up order not found, cannot mark as expired trade_no=%s", referenceId))
		return
	}
	if err != nil {
		logger.LogError(ctx, common.LogText("Stripe top-up order expiry processing failed trade_no=%s error=%q", referenceId, err.Error()))
		return
	}

	logger.LogInfo(ctx, common.LogText("Stripe top-up order expired trade_no=%s", referenceId))
}

// genStripeLink generates a Stripe Checkout session URL for payment.
// It creates a new checkout session with the specified parameters and returns the payment URL.
//
// Parameters:
//   - referenceId: unique reference identifier for the transaction
//   - customerId: existing Stripe customer ID (empty string if new customer)
//   - email: customer email address for new customer creation
//   - amount: quantity of units to purchase
//   - successURL: custom URL to redirect after successful payment (empty for default)
//   - cancelURL: custom URL to redirect when payment is canceled (empty for default)
//
// Returns the checkout session URL or an error if the session creation fails.
func genStripeLink(referenceId string, customerId string, email string, amount int64, successURL string, cancelURL string) (string, error) {
	if !strings.HasPrefix(setting.StripeApiSecret, "sk_") && !strings.HasPrefix(setting.StripeApiSecret, "rk_") {
		return "", errors.New("invalid Stripe API key")
	}

	stripe.Key = setting.StripeApiSecret

	// Use custom URLs if provided, otherwise use defaults
	if successURL == "" {
		successURL = service.PaymentReturnPath("/usage-logs")
	}
	if cancelURL == "" {
		cancelURL = service.PaymentReturnPath("/wallet")
	}

	params := &stripe.CheckoutSessionParams{
		ClientReferenceID: stripe.String(referenceId),
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(setting.StripePriceId),
				Quantity: stripe.Int64(amount),
			},
		},
		Mode:                stripe.String(string(stripe.CheckoutSessionModePayment)),
		AllowPromotionCodes: stripe.Bool(setting.StripePromotionCodesEnabled),
	}

	if "" == customerId {
		if "" != email {
			params.CustomerEmail = stripe.String(email)
		}

		params.CustomerCreation = stripe.String(string(stripe.CheckoutSessionCustomerCreationAlways))
	} else {
		params.Customer = stripe.String(customerId)
	}

	result, err := session.New(params)
	if err != nil {
		return "", err
	}

	return result.URL, nil
}

func GetChargedAmount(count float64, user model.User) float64 {
	topUpGroupRatio := common.GetTopupGroupRatio(user.Group)
	if topUpGroupRatio == 0 {
		topUpGroupRatio = 1
	}

	return count * topUpGroupRatio
}

func getStripeCreditedQuota(amount int64, group string) decimal.Decimal {
	topUpGroupRatio := common.GetTopupGroupRatio(group)
	if topUpGroupRatio == 0 {
		topUpGroupRatio = 1
	}
	return decimal.NewFromInt(amount).
		Mul(decimal.NewFromFloat(topUpGroupRatio)).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit))
}

func getStripePayMoney(amount float64, group string) float64 {
	originalAmount := amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amount = amount / common.QuotaPerUnit
	}
	// Using float64 for monetary calculations is acceptable here due to the small amounts involved
	topupGroupRatio := common.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}
	// apply optional preset discount by the original request amount (if configured), default 1.0
	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(originalAmount)]; ok {
		if ds > 0 {
			discount = ds
		}
	}
	payMoney := amount * setting.StripeUnitPrice * topupGroupRatio * discount
	return payMoney
}

func getStripeMinTopup() int64 {
	minTopup := setting.StripeMinTopUp
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		minTopup = minTopup * int(common.QuotaPerUnit)
	}
	return int64(minTopup)
}
