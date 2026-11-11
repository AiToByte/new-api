package service

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func amount(s string) decimal.Decimal {
	return decimal.RequireFromString(s)
}

// TestReconcilePaidAmountRejectsZeroAmount 是本次修复的核心：渠道侧
// 100%-off 优惠券会产生 payment_status=paid 但 amount_total=0 的合法验签
// 事件，绝不能据此全额入账。
func TestReconcilePaidAmountRejectsZeroAmount(t *testing.T) {
	order := OrderAmount{ExpectedAmount: amount("100.00"), Currency: "USD"}
	for _, paidValue := range []string{"0", "-1", "0.00"} {
		_, err := ReconcilePaidAmount(PaidAmount{
			AmountKnown: true, Amount: amount(paidValue), Currency: "USD",
		}, order)
		if !errors.Is(err, ErrPaymentAmountInvalid) {
			t.Fatalf("实付 %s 元必须被拒绝，实际 err=%v", paidValue, err)
		}
	}
}

func TestReconcilePaidAmountRejectsMismatch(t *testing.T) {
	order := OrderAmount{ExpectedAmount: amount("100.00"), Currency: "USD", PriceID: "price_100"}

	tests := []struct {
		name string
		paid PaidAmount
		want error
	}{
		{
			name: "少付（优惠券折价）",
			paid: PaidAmount{AmountKnown: true, Amount: amount("1.00"), Currency: "USD", PriceID: "price_100"},
			want: ErrPaymentAmountMismatch,
		},
		{
			name: "币种不符",
			paid: PaidAmount{AmountKnown: true, Amount: amount("100.00"), Currency: "CNY", PriceID: "price_100"},
			want: ErrPaymentCurrencyMismatch,
		},
		{
			name: "用低价商品的付款结算高价订单",
			paid: PaidAmount{AmountKnown: true, Amount: amount("100.00"), Currency: "USD", PriceID: "price_1"},
			want: ErrPaymentPriceMismatch,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReconcilePaidAmount(tt.paid, order)
			if !errors.Is(err, tt.want) {
				t.Fatalf("期望 %v，实际 %v", tt.want, err)
			}
		})
	}
}

func TestReconcilePaidAmountAllowsValidPayment(t *testing.T) {
	order := OrderAmount{ExpectedAmount: amount("100.00"), Currency: "usd", PriceID: "price_100"}
	// 允许最小货币单位的取整差异（0.01），币种大小写不敏感。
	warn, err := ReconcilePaidAmount(PaidAmount{
		AmountKnown: true, Amount: amount("100.01"), Currency: "USD", PriceID: "PRICE_100",
	}, order)
	if err != nil {
		t.Fatalf("合法付款不应被拒绝：%v", err)
	}
	if warn != nil {
		t.Fatalf("有完整快照时不应产生告警：%v", warn)
	}
}

// TestReconcilePaidAmountBackwardCompatibility 保证升级不会让历史在途订单
// 或「回调不带金额字段」的合法事件被误拒，但仍拦截明确的 0 元结算。
func TestReconcilePaidAmountBackwardCompatibility(t *testing.T) {
	t.Run("历史订单无快照仍放行", func(t *testing.T) {
		warn, err := ReconcilePaidAmount(PaidAmount{
			AmountKnown: true, Amount: amount("58.00"), Currency: "USD",
		}, OrderAmount{})
		if err != nil {
			t.Fatalf("无快照订单不应被阻断：%v", err)
		}
		if warn == nil {
			t.Fatal("无快照订单应产生告警，便于运营发现")
		}
	})

	t.Run("回调无金额字段仍放行", func(t *testing.T) {
		warn, err := ReconcilePaidAmount(PaidAmount{
			AmountKnown: false, Currency: "USD",
		}, OrderAmount{ExpectedAmount: amount("100.00"), Currency: "USD"})
		if err != nil {
			t.Fatalf("缺金额字段不应被阻断：%v", err)
		}
		if warn == nil {
			t.Fatal("跳过金额校验时应产生告警")
		}
	})

	t.Run("无快照但明确 0 元仍拒绝", func(t *testing.T) {
		_, err := ReconcilePaidAmount(PaidAmount{
			AmountKnown: true, Amount: decimal.Zero, Currency: "USD",
		}, OrderAmount{})
		if !errors.Is(err, ErrPaymentAmountInvalid) {
			t.Fatalf("0 元结算无论有无快照都必须拒绝，实际 %v", err)
		}
	})
}

func TestIsPaymentAmountRejected(t *testing.T) {
	if !IsPaymentAmountRejected(ErrPaymentAmountInvalid) ||
		!IsPaymentAmountRejected(ErrPaymentAmountMismatch) ||
		!IsPaymentAmountRejected(ErrPaymentCurrencyMismatch) ||
		!IsPaymentAmountRejected(ErrPaymentPriceMismatch) {
		t.Fatal("四类金额对账错误都应被识别为拒绝")
	}
	if IsPaymentAmountRejected(errors.New("top-up order not found")) {
		t.Fatal("普通业务错误不应被误判为金额拒绝")
	}
}
