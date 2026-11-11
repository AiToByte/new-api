package service

import (
	"errors"

	"github.com/QuantumNous/new-api/model"
)

// 支付结算统一入口。
//
// Controllers（支付回调、手动补单、余额购买）通过这里进入 model 层的资金变动
// 逻辑，保证 controller → service → model 的依赖方向，并为金额对账这类横切
// 安全控制提供唯一 seam。记账本身仍留在事务所属的表上。
//
// 安全约定：所有「支付回调」结算都必须传入已验签的实付金额（PaidAmount）。
// 结算前会核对「实付金额 > 0」「币种一致」「与订单金额快照相符」；任何一项
// 不满足即拒绝结算，绝不静默放行。手动补单、余额购买等不经过支付渠道的
// 内部路径传 nil，跳过渠道金额对账。

// reconcileOrder 对指定类型的订单执行金额对账。
// 充值与订阅订单分属两张表，trade_no 各自独立；明确指定类型可以杜绝
// 「充值回调误读到同名订阅订单快照」这类跨表歧义。
func reconcileOrder(snapshot model.OrderAmountSnapshot, paid *PaidAmount) error {
	if paid == nil {
		return nil
	}
	warn, err := ReconcilePaidAmount(*paid, OrderAmount{
		ExpectedAmount: snapshot.ExpectedAmount,
		Currency:       snapshot.Currency,
		PriceID:        snapshot.PriceID,
	})
	if err != nil {
		return err
	}
	// 缺少金额快照的历史订单只做保守校验（金额 > 0、币种一致）。
	// 这里不阻断升级前创建的在途订单；warn 由 ReconcilePaidAmount 返回，
	// 调用方可按需记录告警日志。
	_ = warn
	return nil
}

// reconcileTopUp 在充值结算前对账。
func reconcileTopUp(tradeNo string, paid *PaidAmount) error {
	if paid == nil {
		return nil
	}
	return reconcileOrder(model.GetTopUpAmountSnapshot(tradeNo), paid)
}

// reconcileSubscription 在订阅结算前对账。
func reconcileSubscription(tradeNo string, paid *PaidAmount) error {
	if paid == nil {
		return nil
	}
	return reconcileOrder(model.GetSubscriptionOrderAmountSnapshot(tradeNo), paid)
}

// IsPaymentAmountRejected 判断结算错误是否属于「金额对账拒绝」。
// controller 用它区分「需要告警并拒绝回调」与「订单不存在/已结算」等正常分支。
func IsPaymentAmountRejected(err error) bool {
	return errors.Is(err, ErrPaymentAmountInvalid) ||
		errors.Is(err, ErrPaymentCurrencyMismatch) ||
		errors.Is(err, ErrPaymentAmountMismatch) ||
		errors.Is(err, ErrPaymentPriceMismatch)
}

// RechargeEpay 结算易支付充值回调。paid 为验签后的实付金额。
func RechargeEpay(tradeNo string, actualPaymentMethod string, callerIp string, paid *PaidAmount) (alreadyDone bool, err error) {
	if err := reconcileTopUp(tradeNo, paid); err != nil {
		return false, err
	}
	return model.RechargeEpay(tradeNo, actualPaymentMethod, callerIp)
}

// Recharge 结算 Stripe 充值回调。paid 为验签后的实付金额。
func Recharge(referenceId string, customerId string, callerIp string, paid *PaidAmount) error {
	if err := reconcileTopUp(referenceId, paid); err != nil {
		return err
	}
	return model.Recharge(referenceId, customerId, callerIp)
}

// RechargeCreem 结算 Creem 充值回调。
func RechargeCreem(referenceId string, customerEmail string, customerName string, callerIp string, paid *PaidAmount) error {
	if err := reconcileTopUp(referenceId, paid); err != nil {
		return err
	}
	return model.RechargeCreem(referenceId, customerEmail, customerName, callerIp)
}

// RechargeWaffo 结算 Waffo 充值回调。
func RechargeWaffo(tradeNo string, callerIp string, paid *PaidAmount) error {
	if err := reconcileTopUp(tradeNo, paid); err != nil {
		return err
	}
	return model.RechargeWaffo(tradeNo, callerIp)
}

// RechargeWaffoPancake 结算 Waffo Pancake 充值回调。
func RechargeWaffoPancake(tradeNo string, paid *PaidAmount) error {
	if err := reconcileTopUp(tradeNo, paid); err != nil {
		return err
	}
	return model.RechargeWaffoPancake(tradeNo)
}

// PurchaseSubscriptionWithBalance 使用钱包余额购买订阅，不经支付渠道，
// 因此不做渠道金额对账。
func PurchaseSubscriptionWithBalance(userId int, planId int) error {
	return model.PurchaseSubscriptionWithBalance(userId, planId)
}

// CompleteSubscriptionOrder 结算已支付的订阅订单。paid 为验签后的实付金额。
func CompleteSubscriptionOrder(tradeNo string, providerPayload string, expectedPaymentProvider string, actualPaymentMethod string, paid *PaidAmount) error {
	if err := reconcileSubscription(tradeNo, paid); err != nil {
		return err
	}
	return model.CompleteSubscriptionOrder(tradeNo, providerPayload, expectedPaymentProvider, actualPaymentMethod)
}
