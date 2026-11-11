package controller

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/shopspring/decimal"
	"github.com/stripe/stripe-go/v81"
)

func decimalMust(s string) decimal.Decimal {
	return decimal.RequireFromString(s)
}

// 构造一个「已验签」的 Stripe 事件（测试中直接构造对象，等价于验签后的事件）。
func stripeEventWithAmount(amountTotal string, currency string) stripe.Event {
	return stripe.Event{
		Type: stripe.EventTypeCheckoutSessionCompleted,
		Data: &stripe.EventData{
			Object: map[string]any{
				"amount_total":        amountTotal,
				"currency":            currency,
				"client_reference_id": "ref_test",
				"status":              "complete",
				"payment_status":      "paid",
			},
		},
	}
}

// TestStripePaidAmountDetectsZeroAmountCoupon 覆盖本次修复的核心攻击路径：
// 管理员开启 Stripe 促销码后，攻击者用 100%-off 优惠券完成 checkout，
// Stripe 会发出 payment_status=paid 且 amount_total=0 的合法验签事件。
// 该事件必须被金额对账拒绝，绝不能按本地订单金额全额入账。
func TestStripePaidAmountDetectsZeroAmountCoupon(t *testing.T) {
	paid := stripePaidAmount(stripeEventWithAmount("0", "usd"))
	if !paid.AmountKnown {
		t.Fatal("amount_total=0 是可解析的明确金额，必须标记为已知")
	}
	if !paid.Amount.IsZero() {
		t.Fatalf("0 元金额解析错误：%s", paid.Amount.String())
	}
	_, err := service.ReconcilePaidAmount(*paid, service.OrderAmount{})
	if !errors.Is(err, service.ErrPaymentAmountInvalid) {
		t.Fatalf("0 元结算必须被拒绝，实际 err=%v", err)
	}
}

func TestStripePaidAmountParsesMinorUnits(t *testing.T) {
	paid := stripePaidAmount(stripeEventWithAmount("1250", "usd"))
	if !paid.AmountKnown || !paid.Amount.Equal(decimalMust("12.50")) {
		t.Fatalf("分转主单位错误：known=%v amount=%s", paid.AmountKnown, paid.Amount.String())
	}
	if paid.Currency != "USD" {
		t.Fatalf("币种应规范化为大写，实际 %q", paid.Currency)
	}
	warn, err := service.ReconcilePaidAmount(*paid, service.OrderAmount{
		ExpectedAmount: decimalMust("12.50"), Currency: "USD",
	})
	if err != nil || warn != nil {
		t.Fatalf("合法金额应通过对账，err=%v warn=%v", err, warn)
	}
}

// TestStripePaidAmountMissingFieldDoesNotBlock 保证不带金额字段的合法事件
// （如部分异步支付成功事件）不会被误判为 0 元而拒绝。
func TestStripePaidAmountMissingFieldDoesNotBlock(t *testing.T) {
	paid := stripePaidAmount(stripe.Event{Data: &stripe.EventData{Object: map[string]any{}}})
	if paid.AmountKnown {
		t.Fatal("缺少 amount_total 时应标记为金额未知")
	}
	warn, err := service.ReconcilePaidAmount(*paid, service.OrderAmount{
		ExpectedAmount: decimalMust("12.50"), Currency: "USD",
	})
	if err != nil {
		t.Fatalf("金额字段缺失不应阻断结算：%v", err)
	}
	if warn == nil {
		t.Fatal("跳过金额校验时应产生告警")
	}
}

// TestStripeSettlementOutcomeClassification 锁定 webhook 响应码策略：
// 永久性拒绝（0 元券等）不能让渠道无限重试；临时性失败必须返回 5xx，
// 否则「已收款、未入账」的订单永远等不到补单。
func TestStripeSettlementOutcomeClassification(t *testing.T) {
	if stripeSettlementRejected == stripeSettlementRetryable {
		t.Fatal("永久拒绝与临时失败必须是不同的结果类别")
	}
	if stripeSettlementRejected == stripeSettlementSettled || stripeSettlementRetryable == stripeSettlementSettled {
		t.Fatal("失败类别不得与成功类别混淆")
	}
	// 缺订单号的事件属于被忽略（永久），不应触发重试风暴。
	if got := fulfillOrder(t.Context(), stripe.Event{}, "", "", "127.0.0.1"); got != stripeSettlementIgnored {
		t.Fatalf("缺少订单号的事件应被忽略，实际 %v", got)
	}
}
