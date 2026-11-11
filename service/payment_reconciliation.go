package service

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/shopspring/decimal"
)

// 支付金额对账。
//
// 背景：支付回调只按本地订单记录的额度入账，历史上从不核对渠道「实付金额」。
// 攻击者（或管理员误配的 100%-off 优惠券）可以让渠道返回 payment_status=paid
// 但 amount_total=0，验签通过即全额入账，造成额度凭空增发。因此回调验签之后、
// 结算之前必须核对实付金额与币种。
//
// 设计要点：
//  1. 「期望金额」来自下单时持久化的订单快照，绝不从回调参数读取，避免被篡改。
//  2. 比对在 controller 调用结算前的单一 seam 完成。订单快照一旦写入即不可变，
//     结算内部另有行锁 + 状态 CAS 幂等，因此前置校验不存在 TOCTOU 窗口。
//  3. 历史订单与部分渠道没有金额快照（快照为空），此时只执行「金额必须 > 0」
//     与「币种一致」的保守校验，不因升级导致在途订单无法结算。
//  4. 金额统一使用主单位 decimal.Decimal（元/美元等），由各渠道负责把
//     minor unit（分）或字符串金额换算后传入，避免浮点误差。

var (
	// ErrPaymentAmountInvalid 表示实付金额缺失、为负或为零。0 元结算必须拒绝：
	// 渠道侧 100%-off 优惠券会让 payment_status=paid 但 amount_total=0。
	ErrPaymentAmountInvalid = errors.New("payment amount is invalid")
	// ErrPaymentCurrencyMismatch 表示回调币种与订单期望币种不一致。
	ErrPaymentCurrencyMismatch = errors.New("payment currency mismatch")
	// ErrPaymentAmountMismatch 表示实付金额与订单快照不符。
	ErrPaymentAmountMismatch = errors.New("payment amount mismatch")
	// ErrPaymentPriceMismatch 表示回调的价格/商品标识与订单快照不符。
	ErrPaymentPriceMismatch = errors.New("payment price mismatch")
)

// paymentAmountTolerance 允许渠道在最小货币单位上的取整差异（主单位 0.01）。
var paymentAmountTolerance = decimal.NewFromFloat(0.01)

// PaidAmount 是一次「已验签」回调中由渠道提供的实付金额快照。
//
// AmountKnown 区分「金额字段缺失/无法解析」与「金额明确为 0」：后者必须拒绝
// （100%-off 优惠券会产生 paid 事件但 amount_total=0），前者只跳过金额校验并
// 告警，避免误拒某些不带金额字段的合法事件（如 Stripe 异步支付成功）。
// Amount 为主单位金额；Currency 为 ISO 4217 代码；PriceID 可选，用于绑定
// 渠道价格/商品标识，防止用 A 商品的付款结算 B 商品的订单。
type PaidAmount struct {
	AmountKnown bool
	Amount      decimal.Decimal
	Currency    string
	PriceID     string
}

// OrderAmount 是下单时持久化的期望金额快照，字段语义与 PaidAmount 对齐。
// ExpectedAmount 为零值（IsZero）代表「无快照」，见文件头设计要点 3。
type OrderAmount struct {
	ExpectedAmount decimal.Decimal
	Currency       string
	PriceID        string
}

// ReconcileWarn 记录一次「因缺少快照而只做保守校验」的结算，供调用方输出
// 告警日志。它不是错误，不应阻断结算。
type ReconcileWarn struct {
	Reason string
}

// ReconcilePaidAmount 校验渠道实付金额是否满足订单期望。
// 返回 nil 表示通过；返回 *ReconcileWarn 表示通过但仅有保守校验（调用方应
// 记录告警）；返回 error 表示必须拒绝结算。
func ReconcilePaidAmount(paid PaidAmount, order OrderAmount) (*ReconcileWarn, error) {
	// 明确为 0 或负金额一律拒绝：渠道侧 100%-off 优惠券会让
	// payment_status=paid 但 amount_total=0，验签通过即全额入账。
	if paid.AmountKnown && paid.Amount.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("%w: amount=%s", ErrPaymentAmountInvalid, paid.Amount.String())
	}

	wantCurrency := normalizeCurrency(order.Currency)
	if wantCurrency != "" && normalizeCurrency(paid.Currency) != "" {
		if normalizeCurrency(paid.Currency) != wantCurrency {
			return nil, fmt.Errorf("%w: order=%s callback=%s", ErrPaymentCurrencyMismatch, wantCurrency, normalizeCurrency(paid.Currency))
		}
	}

	// 价格/商品标识若下单时已确定，回调必须一致。
	if order.PriceID != "" && paid.PriceID != "" && !strings.EqualFold(order.PriceID, paid.PriceID) {
		return nil, fmt.Errorf("%w: order=%s callback=%s", ErrPaymentPriceMismatch, order.PriceID, paid.PriceID)
	}

	// 回调未提供金额字段：不猜测、不阻断，仅告警。这样既能放行 Stripe
	// 异步支付成功等合法事件，也不会把「缺字段」误当成「0 元」。
	if !paid.AmountKnown {
		return &ReconcileWarn{Reason: "callback carries no parseable paid amount; amount reconciliation skipped"}, nil
	}

	// 无金额快照的历史订单：仅保守校验，放行但告警。
	if order.ExpectedAmount.IsZero() {
		return &ReconcileWarn{Reason: "order has no expected amount snapshot; only amount>0 and currency were checked"}, nil
	}

	diff := paid.Amount.Sub(order.ExpectedAmount).Abs()
	if diff.GreaterThan(paymentAmountTolerance) {
		return nil, fmt.Errorf("%w: expected=%s actual=%s currency=%s", ErrPaymentAmountMismatch,
			order.ExpectedAmount.String(), paid.Amount.String(), wantCurrency)
	}
	return nil, nil
}

func normalizeCurrency(currency string) string {
	return strings.ToUpper(strings.TrimSpace(currency))
}

// ParsePaidAmountMinor 解析「最小货币单位」（分）金额，换算为主单位。
// Stripe、Creem 的 amount_total / amount_paid 均为分。解析失败或字段缺失时
// 返回 AmountKnown=false，让对账跳过金额比对而不是误判为 0 元。
func ParsePaidAmountMinor(raw string, currency string, priceID string) *PaidAmount {
	paid := &PaidAmount{Currency: normalizeCurrency(currency), PriceID: strings.TrimSpace(priceID)}
	minor, err := decimal.NewFromString(strings.TrimSpace(raw))
	if err != nil {
		return paid
	}
	paid.AmountKnown = true
	paid.Amount = minor.Div(decimal.NewFromInt(100))
	return paid
}

// ParsePaidAmountMajor 解析「主单位」（元/美元）金额字符串。
// Epay、Waffo、Waffo Pancake 的回调金额为主单位字符串。
func ParsePaidAmountMajor(raw string, currency string, priceID string) *PaidAmount {
	paid := &PaidAmount{Currency: normalizeCurrency(currency), PriceID: strings.TrimSpace(priceID)}
	major, err := decimal.NewFromString(strings.TrimSpace(raw))
	if err != nil {
		return paid
	}
	paid.AmountKnown = true
	paid.Amount = major
	return paid
}

// NewPaidAmountFromFloat 从已解析的浮点数构造金额，供 SDK 已结构化的事件使用。
// 非有限值（NaN/Inf）视为金额未知，避免绕过校验。
func NewPaidAmountFromFloat(value float64, currency string, priceID string) *PaidAmount {
	paid := &PaidAmount{Currency: normalizeCurrency(currency), PriceID: strings.TrimSpace(priceID)}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return paid
	}
	paid.AmountKnown = true
	paid.Amount = decimal.NewFromFloat(value)
	return paid
}
