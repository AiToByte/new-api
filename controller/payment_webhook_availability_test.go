package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func confirmPaymentComplianceForTest(t *testing.T) {
	t.Helper()
	paymentSetting := operation_setting.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
}

// revokePaymentComplianceForTest 模拟管理员撤销合规确认（全局资金总开关）。
// 与 confirmPaymentComplianceForTest 一样，通过 t.Cleanup 恢复现场。
func revokePaymentComplianceForTest(t *testing.T) {
	t.Helper()
	paymentSetting := operation_setting.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	t.Cleanup(func() { paymentSetting.ComplianceConfirmed = originalConfirmed })
	paymentSetting.ComplianceConfirmed = false
}

// 以下用例共同锁定两类开关的边界：
//   - WebhookEnabled 只看「合规 + 验签凭证」；清空充值商品不得关闭回调，
//     否则仅配置订阅计划时会出现「已收款、未发货」的资损。
//   - TopUpEnabled 仍要求完整充值商品配置，保证下单时价格/商品确定。
//   - 撤销合规确认必须同时关闭入口与回调，保证「停用生效」。

func TestStripeWebhookEnabledDependsOnlyOnCredentials(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalAPISecret := setting.StripeApiSecret
	originalWebhookSecret := setting.StripeWebhookSecret
	originalPriceID := setting.StripePriceId
	t.Cleanup(func() {
		setting.StripeApiSecret = originalAPISecret
		setting.StripeWebhookSecret = originalWebhookSecret
		setting.StripePriceId = originalPriceID
	})

	setting.StripeApiSecret = "sk_test_123"
	setting.StripeWebhookSecret = "whsec_test"
	setting.StripePriceId = "price_123"
	require.True(t, service.IsStripeWebhookEnabled())
	require.True(t, service.IsStripeTopUpEnabled())

	// 缺少验签密钥时回调必须 fail-closed。
	setting.StripeWebhookSecret = ""
	require.False(t, service.IsStripeWebhookEnabled())
	setting.StripeWebhookSecret = "whsec_test"

	// 仅清空充值价格 ID：充值入口关闭，但订阅回调仍须可处理。
	setting.StripePriceId = ""
	require.False(t, service.IsStripeTopUpEnabled())
	require.True(t, service.IsStripeWebhookEnabled(), "订阅计划自带 StripePriceId，回调不得因全局充值价格缺失被拒")

	// 撤销合规确认是全局资金总开关，回调必须关闭。
	revokePaymentComplianceForTest(t)
	require.False(t, service.IsStripeWebhookEnabled())
}

func TestCreemWebhookEnabledDependsOnlyOnCredentials(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalAPIKey := setting.CreemApiKey
	originalProducts := setting.CreemProducts
	originalWebhookSecret := setting.CreemWebhookSecret
	t.Cleanup(func() {
		setting.CreemApiKey = originalAPIKey
		setting.CreemProducts = originalProducts
		setting.CreemWebhookSecret = originalWebhookSecret
	})

	setting.CreemApiKey = "creem_api_key"
	setting.CreemWebhookSecret = "creem_secret"
	setting.CreemProducts = `[{"productId":"prod_123"}]`
	require.True(t, service.IsCreemWebhookEnabled())
	require.True(t, service.IsCreemTopUpEnabled())

	setting.CreemWebhookSecret = ""
	require.False(t, service.IsCreemWebhookEnabled())
	setting.CreemWebhookSecret = "creem_secret"

	// 清空充值商品列表：入口关闭，订阅回调仍可处理。
	setting.CreemProducts = "[]"
	require.False(t, service.IsCreemTopUpEnabled())
	require.True(t, service.IsCreemWebhookEnabled(), "订阅计划自带 CreemProductId，回调不得因全局商品列表为空被拒")

	revokePaymentComplianceForTest(t)
	require.False(t, service.IsCreemWebhookEnabled())
}

func TestWaffoWebhookEnabledRequiresCredentialsAndToggle(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalEnabled := setting.WaffoEnabled
	originalSandbox := setting.WaffoSandbox
	originalAPIKey := setting.WaffoApiKey
	originalPrivateKey := setting.WaffoPrivateKey
	originalPublicCert := setting.WaffoPublicCert
	originalSandboxAPIKey := setting.WaffoSandboxApiKey
	originalSandboxPrivateKey := setting.WaffoSandboxPrivateKey
	originalSandboxPublicCert := setting.WaffoSandboxPublicCert
	t.Cleanup(func() {
		setting.WaffoEnabled = originalEnabled
		setting.WaffoSandbox = originalSandbox
		setting.WaffoApiKey = originalAPIKey
		setting.WaffoPrivateKey = originalPrivateKey
		setting.WaffoPublicCert = originalPublicCert
		setting.WaffoSandboxApiKey = originalSandboxAPIKey
		setting.WaffoSandboxPrivateKey = originalSandboxPrivateKey
		setting.WaffoSandboxPublicCert = originalSandboxPublicCert
	})

	setting.WaffoEnabled = true
	setting.WaffoSandbox = false
	setting.WaffoApiKey = ""
	setting.WaffoPrivateKey = "private"
	setting.WaffoPublicCert = "public"
	require.False(t, service.IsWaffoWebhookEnabled())

	setting.WaffoApiKey = "api"
	require.True(t, service.IsWaffoWebhookEnabled())

	setting.WaffoEnabled = false
	require.False(t, service.IsWaffoWebhookEnabled())

	setting.WaffoEnabled = true
	setting.WaffoSandbox = true
	setting.WaffoSandboxApiKey = ""
	setting.WaffoSandboxPrivateKey = "sandbox_private"
	setting.WaffoSandboxPublicCert = "sandbox_public"
	require.False(t, service.IsWaffoWebhookEnabled())

	setting.WaffoSandboxApiKey = "sandbox_api"
	require.True(t, service.IsWaffoWebhookEnabled())

	// 撤销合规确认是全局资金总开关。
	revokePaymentComplianceForTest(t)
	require.False(t, service.IsWaffoWebhookEnabled())
}

func TestWaffoPancakeWebhookEnabledDependsOnlyOnCredentials(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalMerchantID := setting.WaffoPancakeMerchantID
	originalPrivateKey := setting.WaffoPancakePrivateKey
	originalProductID := setting.WaffoPancakeProductID
	t.Cleanup(func() {
		setting.WaffoPancakeMerchantID = originalMerchantID
		setting.WaffoPancakePrivateKey = originalPrivateKey
		setting.WaffoPancakeProductID = originalProductID
	})

	// 验签公钥内置于 SDK，商户 ID + 私钥是回调验签的最小凭证集合。
	setting.WaffoPancakeMerchantID = ""
	setting.WaffoPancakePrivateKey = "private"
	setting.WaffoPancakeProductID = "product"
	require.False(t, service.IsWaffoPancakeWebhookEnabled())

	setting.WaffoPancakeMerchantID = "merchant"
	require.True(t, service.IsWaffoPancakeWebhookEnabled())
	require.True(t, service.IsWaffoPancakeTopUpEnabled())

	// 清空充值商品 ID：入口关闭，但订阅回调仍须可处理（订阅计划自带商品 ID）。
	setting.WaffoPancakeProductID = ""
	require.False(t, service.IsWaffoPancakeTopUpEnabled())
	require.True(t, service.IsWaffoPancakeWebhookEnabled(), "回调验签不需要全局充值商品 ID")

	setting.WaffoPancakeMerchantID = "merchant"
	setting.WaffoPancakePrivateKey = ""
	require.False(t, service.IsWaffoPancakeWebhookEnabled())

	setting.WaffoPancakePrivateKey = "private"
	revokePaymentComplianceForTest(t)
	require.False(t, service.IsWaffoPancakeWebhookEnabled())
}

func TestEpayWebhookEnabledDependsOnlyOnCredentials(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalPayAddress := operation_setting.PayAddress
	originalEpayID := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	originalPayMethods := operation_setting.PayMethods
	t.Cleanup(func() {
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayID
		operation_setting.EpayKey = originalEpayKey
		operation_setting.PayMethods = originalPayMethods
	})

	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "epay_id"
	operation_setting.EpayKey = ""
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	require.False(t, service.IsEpayWebhookEnabled())

	operation_setting.EpayKey = "epay_key"
	require.True(t, service.IsEpayWebhookEnabled())
	require.True(t, service.IsEpayTopUpEnabled())

	// 清空支付方式：只关闭新的充值下单，不阻断已付款订单的回调结算。
	operation_setting.PayMethods = nil
	require.False(t, service.IsEpayTopUpEnabled())
	require.True(t, service.IsEpayWebhookEnabled(), "已付款订单必须能完成结算，清空支付方式不得让资金悬空")

	// 撤销合规确认则必须同时关闭入口与回调。
	revokePaymentComplianceForTest(t)
	require.False(t, service.IsEpayWebhookEnabled())
}

func TestEpayCallbacksVerifyBeforeSettlement(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalPayAddress := operation_setting.PayAddress
	originalEpayID := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	originalPayMethods := operation_setting.PayMethods
	originalServerAddress := system_setting.ServerAddress
	originalDB := model.DB
	t.Cleanup(func() {
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayID
		operation_setting.EpayKey = originalEpayKey
		operation_setting.PayMethods = originalPayMethods
		system_setting.ServerAddress = originalServerAddress
		model.DB = originalDB
	})
	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "epay_id"
	operation_setting.EpayKey = "test-key"
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	system_setting.ServerAddress = "https://dashboard.example.com"
	// Rejected callbacks and valid non-success events must never reach settlement.
	// A nil database makes an accidental attempt to settle fail the test.
	model.DB = nil
	require.True(t, service.IsEpayWebhookEnabled())

	// Both ambiguous maps below had this same valid signing string in v0.0.4.
	// This must fail because of parameter ambiguity, not a mismatched signature.
	ambiguousSign := epay.MD5String("money=1.00&name=item&out_trade_no=other&out_trade_no=order&pid=epay_id&trade_status=TRADE_SUCCESS&type=alipay", "test-key")
	pendingSign := epay.MD5String("money=1.00&name=item%26other&out_trade_no=order&pid=epay_id&trade_status=WAIT_BUYER_PAY&type=alipay", "test-key")
	cases := []struct {
		name         string
		productName  string
		tradeNo      string
		status       string
		sign         string
		validPending bool
	}{
		{"separator in product name", "item&out_trade_no=other", "order", epay.StatusTradeSuccess, ambiguousSign, false},
		{"separator in order number", "item", "other&out_trade_no=order", epay.StatusTradeSuccess, ambiguousSign, false},
		{"invalid signature", "item", "order", epay.StatusTradeSuccess, "invalid", false},
		{"missing signature", "item", "order", epay.StatusTradeSuccess, "", false},
		{"valid pending with literal percent escape", "item%26other", "order", "WAIT_BUYER_PAY", pendingSign, true},
	}
	endpoints := []struct {
		name             string
		path             string
		handler          gin.HandlerFunc
		status           int
		rejectedBody     string
		pendingBody      string
		rejectedLocation string
		pendingLocation  string
	}{
		{
			name: "topup notify", path: "/api/user/epay/notify", handler: EpayNotify,
			status: http.StatusOK, rejectedBody: "fail", pendingBody: "success",
		},
		{
			name: "subscription notify", path: "/api/subscription/epay/notify", handler: SubscriptionEpayNotify,
			status: http.StatusOK, rejectedBody: "fail", pendingBody: "fail",
		},
		{
			name: "subscription return", path: "/api/subscription/epay/return", handler: SubscriptionEpayReturn,
			status:           http.StatusFound,
			rejectedLocation: "https://dashboard.example.com/wallet?pay=fail",
			pendingLocation:  "https://dashboard.example.com/wallet?pay=pending",
		},
	}
	router := gin.New()
	for _, endpoint := range endpoints {
		router.Any(endpoint.path, endpoint.handler)
	}
	for _, endpoint := range endpoints {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			for _, tc := range cases {
				t.Run(endpoint.name+"/"+method+"/"+tc.name, func(t *testing.T) {
					params := url.Values{
						"pid": {"epay_id"}, "type": {"alipay"}, "money": {"1.00"},
						"name": {tc.productName}, "out_trade_no": {tc.tradeNo},
						"trade_status": {tc.status}, "sign_type": {"MD5"},
					}
					if tc.sign != "" {
						params.Set("sign", tc.sign)
					}
					request := httptest.NewRequest(method, endpoint.path+"?"+params.Encode(), nil)
					if method == http.MethodPost {
						request = httptest.NewRequest(method, endpoint.path, strings.NewReader(params.Encode()))
						request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					}
					recorder := httptest.NewRecorder()
					require.NotPanics(t, func() { router.ServeHTTP(recorder, request) })
					assert.Equal(t, endpoint.status, recorder.Code)
					wantBody, wantLocation := endpoint.rejectedBody, endpoint.rejectedLocation
					if tc.validPending {
						wantBody, wantLocation = endpoint.pendingBody, endpoint.pendingLocation
					}
					if wantBody != "" {
						assert.Equal(t, wantBody, recorder.Body.String())
					}
					assert.Equal(t, wantLocation, recorder.Header().Get("Location"))
				})
			}
		}
	}
}

// TestEpayCallbacksRejectedWhenWebhookDisabled 锁定「停用生效」：
// 撤销合规确认后，即使携带合法签名与成功交易状态，充值/订阅的异步通知与
// 浏览器回跳都必须在结算之前被拒绝，避免存量 pending 订单被继续结算。
func TestEpayCallbacksRejectedWhenWebhookDisabled(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalPayAddress := operation_setting.PayAddress
	originalEpayID := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	originalPayMethods := operation_setting.PayMethods
	originalServerAddress := system_setting.ServerAddress
	originalDB := model.DB
	t.Cleanup(func() {
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayID
		operation_setting.EpayKey = originalEpayKey
		operation_setting.PayMethods = originalPayMethods
		system_setting.ServerAddress = originalServerAddress
		model.DB = originalDB
	})
	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "epay_id"
	operation_setting.EpayKey = "test-key"
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	system_setting.ServerAddress = "https://dashboard.example.com"

	// nil DB 会让任何意外进入结算流程的调用直接失败，从而证明门禁位于结算之前。
	model.DB = nil
	require.True(t, service.IsEpayWebhookEnabled())
	revokePaymentComplianceForTest(t)
	require.False(t, service.IsEpayWebhookEnabled())

	// 构造一个本来完全合法的「支付成功」回调。
	params := url.Values{
		"pid": {"epay_id"}, "type": {"alipay"}, "money": {"1.00"},
		"name": {"item"}, "out_trade_no": {"order"},
		"trade_status": {epay.StatusTradeSuccess}, "sign_type": {"MD5"},
	}
	params.Set("sign", epay.MD5String("money=1.00&name=item&out_trade_no=order&pid=epay_id&trade_status=TRADE_SUCCESS&type=alipay", "test-key"))

	endpoints := []struct {
		name             string
		path             string
		handler          gin.HandlerFunc
		status           int
		rejectedBody     string
		rejectedLocation string
	}{
		{
			name: "topup notify", path: "/api/user/epay/notify", handler: EpayNotify,
			status: http.StatusOK, rejectedBody: "fail",
		},
		{
			name: "subscription notify", path: "/api/subscription/epay/notify", handler: SubscriptionEpayNotify,
			status: http.StatusOK, rejectedBody: "fail",
		},
		{
			name: "subscription return", path: "/api/subscription/epay/return", handler: SubscriptionEpayReturn,
			status:           http.StatusFound,
			rejectedLocation: "https://dashboard.example.com/wallet?pay=fail",
		},
	}
	router := gin.New()
	for _, endpoint := range endpoints {
		router.Any(endpoint.path, endpoint.handler)
	}
	for _, endpoint := range endpoints {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(endpoint.name+"/"+method, func(t *testing.T) {
				request := httptest.NewRequest(method, endpoint.path+"?"+params.Encode(), nil)
				if method == http.MethodPost {
					request = httptest.NewRequest(method, endpoint.path, strings.NewReader(params.Encode()))
					request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				}
				recorder := httptest.NewRecorder()
				require.NotPanics(t, func() { router.ServeHTTP(recorder, request) })
				assert.Equal(t, endpoint.status, recorder.Code)
				if endpoint.rejectedBody != "" {
					assert.Equal(t, endpoint.rejectedBody, recorder.Body.String())
				}
				assert.Equal(t, endpoint.rejectedLocation, recorder.Header().Get("Location"))
			})
		}
	}
}
