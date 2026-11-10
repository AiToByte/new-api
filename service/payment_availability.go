package service

import (
	"strings"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// 支付渠道可用性判定。
//
// 这里刻意区分两类开关，二者不可互相替代：
//   - TopUp 开关：控制是否允许用户「发起充值」。它要求完整配置，包括该渠道
//     的充值商品（Stripe 价格 ID、Creem 商品列表、Epay 支付方式、Pancake
//     商品 ID），因为下单时必须知道卖什么、按什么价格收钱。
//   - Webhook 开关：控制是否「处理支付回调」。它只依赖合规确认与验签凭证，
//     绝不能依赖充值商品配置。
//
// 为什么 Webhook 开关不能耦合充值商品：
// 订阅付款使用「订阅计划」自带的 price_id / product_id，并不要求全局充值项。
// 若把 WebhookEnabled 直接等同于 TopUpEnabled（历史实现），管理员仅配置订阅
// 计划而未配置充值商品时，用户依旧可以完成付款，但回调会被 403 拒绝，导致
// 「已收款、未发货」的资损，且渠道侧收到 2xx 后停止重试、无法自动补单。
//
// 合规确认（IsPaymentComplianceConfirmed）仍是全局资金总开关：撤销合规时
// 充值入口与所有回调一并关闭，保证「停用生效」。清空充值商品只影响新充值
// 下单，不会阻断「已经发生的付款」完成结算——这是有意且正确的行为。

func IsPaymentComplianceConfirmed() bool {
	return operation_setting.IsPaymentComplianceConfirmed()
}

// IsStripeTopUpEnabled 判断 Stripe 充值入口是否可用（要求充值价格 ID）。
func IsStripeTopUpEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	return strings.TrimSpace(setting.StripeApiSecret) != "" &&
		strings.TrimSpace(setting.StripeWebhookSecret) != "" &&
		strings.TrimSpace(setting.StripePriceId) != ""
}

func IsStripeWebhookConfigured() bool {
	return strings.TrimSpace(setting.StripeWebhookSecret) != ""
}

// IsStripeWebhookEnabled 判断 Stripe 回调是否可处理：合规已确认 + API 密钥
// + 验签密钥齐备即可。不要求 StripePriceId（订阅计划自带价格 ID）。
func IsStripeWebhookEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	return strings.TrimSpace(setting.StripeApiSecret) != "" &&
		IsStripeWebhookConfigured()
}

// IsCreemTopUpEnabled 判断 Creem 充值入口是否可用（要求充值商品列表）。
func IsCreemTopUpEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	products := strings.TrimSpace(setting.CreemProducts)
	return strings.TrimSpace(setting.CreemApiKey) != "" &&
		products != "" &&
		products != "[]"
}

func IsCreemWebhookConfigured() bool {
	return strings.TrimSpace(setting.CreemWebhookSecret) != ""
}

// IsCreemWebhookEnabled 判断 Creem 回调是否可处理：合规 + API 密钥 + 验签
// 密钥。不要求 CreemProducts（订阅计划自带 product_id）。
func IsCreemWebhookEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	return strings.TrimSpace(setting.CreemApiKey) != "" &&
		IsCreemWebhookConfigured()
}

// IsWaffoTopUpEnabled 判断 Waffo 充值入口是否可用。
func IsWaffoTopUpEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	if !setting.WaffoEnabled {
		return false
	}
	return IsWaffoWebhookConfigured()
}

func IsWaffoWebhookConfigured() bool {
	if setting.WaffoSandbox {
		return strings.TrimSpace(setting.WaffoSandboxApiKey) != "" &&
			strings.TrimSpace(setting.WaffoSandboxPrivateKey) != "" &&
			strings.TrimSpace(setting.WaffoSandboxPublicCert) != ""
	}
	return strings.TrimSpace(setting.WaffoApiKey) != "" &&
		strings.TrimSpace(setting.WaffoPrivateKey) != "" &&
		strings.TrimSpace(setting.WaffoPublicCert) != ""
}

// IsWaffoWebhookEnabled 判断 Waffo 回调是否可处理：合规 + 渠道启用 +
// 验签凭证齐备。Waffo 本身没有独立的充值商品配置。
func IsWaffoWebhookEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	return setting.WaffoEnabled && IsWaffoWebhookConfigured()
}

// IsWaffoPancakeTopUpEnabled 判断 Pancake 充值入口是否可用（要求商品 ID）。
func IsWaffoPancakeTopUpEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	// 凭证齐备即启用：Webhook 公钥内置于 SDK，运行模式由每个事件自带。
	return strings.TrimSpace(setting.WaffoPancakeMerchantID) != "" &&
		strings.TrimSpace(setting.WaffoPancakePrivateKey) != "" &&
		strings.TrimSpace(setting.WaffoPancakeProductID) != ""
}

// IsWaffoPancakeWebhookConfigured 只校验验签所需的商户凭证。商品 ID 是
// 「下单」参数，不是「验签」参数，因此不属于回调门禁。
func IsWaffoPancakeWebhookConfigured() bool {
	return strings.TrimSpace(setting.WaffoPancakeMerchantID) != "" &&
		strings.TrimSpace(setting.WaffoPancakePrivateKey) != ""
}

// IsWaffoPancakeWebhookEnabled 判断 Pancake 回调是否可处理：合规 + 商户
// 凭证。不要求 WaffoPancakeProductID（订阅计划自带 product_id）。
func IsWaffoPancakeWebhookEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	return IsWaffoPancakeWebhookConfigured()
}

// IsEpayTopUpEnabled 判断 Epay 充值入口是否可用（要求已配置支付方式）。
func IsEpayTopUpEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	return IsEpayWebhookConfigured() && len(operation_setting.PayMethods) > 0
}

func IsEpayWebhookConfigured() bool {
	return strings.TrimSpace(operation_setting.PayAddress) != "" &&
		strings.TrimSpace(operation_setting.EpayId) != "" &&
		strings.TrimSpace(operation_setting.EpayKey) != ""
}

// IsEpayWebhookEnabled 判断 Epay 回调是否可处理：合规 + 商户验签凭证。
// 不要求 PayMethods：清空支付方式只能阻止新的充值下单，不能阻断已经
// 付款成功的订单完成结算。撤销合规确认则同时关闭入口与所有回调。
func IsEpayWebhookEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	return IsEpayWebhookConfigured()
}
