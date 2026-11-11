package model

import (
	"strings"

	"github.com/shopspring/decimal"
)

// 订单金额快照读取与写入。
//
// 支付回调结算前需要拿到下单时持久化的「期望实付金额」。充值订单与订阅订单
// 分属两张表，但回调可能只带一个 trade_no（Stripe 的 fulfillOrder 会先尝试
// 订阅订单再回退充值订单），因此这里提供统一的解析入口。
//
// 历史订单的三个快照字段为空：ExpectedAmount 解析为零值 decimal，
// 调用方据此只做保守校验，不阻断升级前创建的在途订单。
//
// 重要：TopUp.Money 在多数渠道表示「服务端计算的实付金额」，可直接作为
// 期望值；但 Stripe 充值的 Money 是 GetChargedAmount 得到的「充值额度金额」
// （受用户组倍率影响），与 Stripe 实际收取的 Price×Quantity 不是同一概念，
// 因此 Stripe 充值订单不得写入金额快照，只依赖「实付 > 0」的保守校验。

// OrderAmountSnapshot 是结算对账所需的订单金额快照。
type OrderAmountSnapshot struct {
	ExpectedAmount decimal.Decimal
	Currency       string
	PriceID        string
	// Found 表示确实存在该订单；trade_no 在两张表都查不到时为 false。
	Found bool
}

// amountSnapshotString 把「下单时实际提交给渠道的金额字符串」规范化后写入。
// 必须传提交给渠道的那个字符串，而不是内部的 float64 变量：例如 Waffo 对
// 零小数币种（JPY/KRW）会把金额取整后再提交，若快照写未取整的 float64，
// 回调金额与快照相差可达 0.5，反而会误拒正常付款。
// 无法解析的字符串视为「无快照」，走保守校验。
func amountSnapshotString(submittedAmount string) string {
	amount, err := decimal.NewFromString(strings.TrimSpace(submittedAmount))
	if err != nil {
		return ""
	}
	return amount.String()
}

// SetExpectedPayment 在下单时写入期望实付金额快照。
// submittedAmount 必须是「下单时提交给渠道的金额字符串」，不能是内部计算的
// 充值额度金额，也不能是渠道侧 Price 自行决定的金额。
// currency 传空表示币种不参与校验（用于回调不带币种的场景）。
func (t *TopUp) SetExpectedPayment(submittedAmount string, currency string, priceID string) {
	t.ExpectedAmount = amountSnapshotString(submittedAmount)
	t.ExpectedCurrency = currency
	t.ExpectedPriceID = priceID
}

// SetExpectedPayment 在下单时写入期望实付金额快照。
// 仅当订阅下单时服务端把确定金额提交给渠道（如 Epay）才可调用；
// 若渠道按 Price ID 自行定价（Stripe/Creem/Pancake），不得写入，
// 因为本地 plan.PriceAmount 与渠道实际收款金额不是同一来源。
func (o *SubscriptionOrder) SetExpectedPayment(submittedAmount string, currency string, priceID string) {
	o.ExpectedAmount = amountSnapshotString(submittedAmount)
	o.ExpectedCurrency = currency
	o.ExpectedPriceID = priceID
}

func parseAmountSnapshot(expectedAmount, currency, priceID string) OrderAmountSnapshot {
	amount, err := decimal.NewFromString(expectedAmount)
	if err != nil {
		// 损坏或非法的快照值不能当成「无快照」放行：保留零值由调用方
		// 按保守校验处理，同时不允许把它当作合法期望金额。
		amount = decimal.Zero
	}
	return OrderAmountSnapshot{
		ExpectedAmount: amount,
		Currency:       currency,
		PriceID:        priceID,
		Found:          true,
	}
}

// GetTopUpAmountSnapshot 读取充值订单的金额快照。
func GetTopUpAmountSnapshot(tradeNo string) OrderAmountSnapshot {
	var topUp TopUp
	if err := DB.Select("expected_amount", "expected_currency", "expected_price_id").
		Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
		return OrderAmountSnapshot{}
	}
	return parseAmountSnapshot(topUp.ExpectedAmount, topUp.ExpectedCurrency, topUp.ExpectedPriceID)
}

// GetSubscriptionOrderAmountSnapshot 读取订阅订单的金额快照。
func GetSubscriptionOrderAmountSnapshot(tradeNo string) OrderAmountSnapshot {
	var order SubscriptionOrder
	if err := DB.Select("expected_amount", "expected_currency", "expected_price_id").
		Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return OrderAmountSnapshot{}
	}
	return parseAmountSnapshot(order.ExpectedAmount, order.ExpectedCurrency, order.ExpectedPriceID)
}
