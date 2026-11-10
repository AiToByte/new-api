package service

import (
	"strings"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// Payment channel availability rules. These predicates decide whether a top-up
// channel and its webhook are usable based on compliance confirmation and the
// configured credentials. They read only setting state, so they belong in the
// service layer.

func IsPaymentComplianceConfirmed() bool {
	return operation_setting.IsPaymentComplianceConfirmed()
}

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

func IsStripeWebhookEnabled() bool {
	return IsStripeTopUpEnabled()
}

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

func IsCreemWebhookEnabled() bool {
	return IsCreemTopUpEnabled() && IsCreemWebhookConfigured()
}

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

func IsWaffoWebhookEnabled() bool {
	return IsWaffoTopUpEnabled()
}

func IsWaffoPancakeTopUpEnabled() bool {
	if !IsPaymentComplianceConfirmed() {
		return false
	}
	// Presence-of-credentials = enabled. Webhook public keys ship inside
	// the SDK; mode (test/prod) is read from each event.
	return strings.TrimSpace(setting.WaffoPancakeMerchantID) != "" &&
		strings.TrimSpace(setting.WaffoPancakePrivateKey) != "" &&
		strings.TrimSpace(setting.WaffoPancakeProductID) != ""
}

func IsWaffoPancakeWebhookConfigured() bool {
	return IsWaffoPancakeTopUpEnabled()
}

func IsWaffoPancakeWebhookEnabled() bool {
	return IsWaffoPancakeTopUpEnabled()
}

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

func IsEpayWebhookEnabled() bool {
	return IsEpayTopUpEnabled()
}
