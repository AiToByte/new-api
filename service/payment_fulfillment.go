package service

import (
	"github.com/QuantumNous/new-api/model"
)

// Payment fulfillment entry points. Controllers (payment webhooks, manual
// top-up confirmation, subscription purchase) reach the money-moving logic in
// model/ through here so the controller → service → model direction holds and
// future cross-cutting concerns (idempotency audits, fulfillment hooks) have a
// single seam. The accounting itself stays with the tables it transacts on;
// these wrappers intentionally add no behavior. See docs/architecture-debt.md
// Phase 3.5.

// RechargeEpay settles an Epay top-up callback. alreadyDone reports a
// duplicate (already settled) callback.
func RechargeEpay(tradeNo string, actualPaymentMethod string, callerIp string) (alreadyDone bool, err error) {
	return model.RechargeEpay(tradeNo, actualPaymentMethod, callerIp)
}

// RechargeCreem settles a Creem top-up callback.
func RechargeCreem(referenceId string, customerEmail string, customerName string, callerIp string) (err error) {
	return model.RechargeCreem(referenceId, customerEmail, customerName, callerIp)
}

// RechargeWaffo settles a Waffo top-up callback.
func RechargeWaffo(tradeNo string, callerIp string) (err error) {
	return model.RechargeWaffo(tradeNo, callerIp)
}

// RechargeWaffoPancake settles a Waffo Pancake top-up callback.
func RechargeWaffoPancake(tradeNo string) (err error) {
	return model.RechargeWaffoPancake(tradeNo)
}

// PurchaseSubscriptionWithBalance buys a subscription plan with wallet balance.
func PurchaseSubscriptionWithBalance(userId int, planId int) error {
	return model.PurchaseSubscriptionWithBalance(userId, planId)
}

// CompleteSubscriptionOrder fulfills a paid subscription order from a payment
// provider callback.
func CompleteSubscriptionOrder(tradeNo string, providerPayload string, expectedPaymentProvider string, actualPaymentMethod string) error {
	return model.CompleteSubscriptionOrder(tradeNo, providerPayload, expectedPaymentProvider, actualPaymentMethod)
}
