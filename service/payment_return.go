package service

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/system_setting"
)

// PaymentReturnPath builds an absolute dashboard URL from the configured
// server address, trimming any trailing slash so suffix always starts cleanly.
func PaymentReturnPath(suffix string) string {
	base := strings.TrimRight(system_setting.ServerAddress, "/")
	return base + suffix
}
