package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 日志指纹用于在「不泄露可用凭据」的前提下关联同一笔回调。
// 这些用例锁定三条不变量：确定性、不回显原文、空值可区分。

func TestLogFingerprintIsStableAndDoesNotLeakSecret(t *testing.T) {
	secret := "whsec_live_abc123.body-with-sensitive-data"
	first := LogFingerprint(secret)
	second := LogFingerprint(secret)

	if first != second {
		t.Fatalf("同一敏感值必须得到同一指纹，便于日志关联：%q != %q", first, second)
	}
	if strings.Contains(first, secret) || strings.Contains(first, "abc123") {
		t.Fatalf("指纹不得回显原文片段：%q", first)
	}
	if len(first) != 16 {
		t.Fatalf("指纹长度应为 16 个十六进制字符，实际 %d：%q", len(first), first)
	}
	if LogFingerprint("other-value") == first {
		t.Fatal("不同敏感值不得产生同一指纹")
	}
}

func TestLogFingerprintEmptyValueIsDistinct(t *testing.T) {
	if LogFingerprint("") != "empty" {
		t.Fatal("空值必须返回固定标记，避免与「提供了空串」混淆")
	}
	if LogFingerprint("") == LogFingerprint("anything") {
		t.Fatal("空值标记不得与真实指纹相同")
	}
}

// TestLogSafeRequestURI 覆盖 Epay GET 回调场景：query 里含 sign 等可重放
// 凭据，日志必须保留路径（排障）但不得出现 query 原文。
func TestLogSafeRequestURI(t *testing.T) {
	const uri = "/api/user/epay/notify?pid=1001&sign=deadbeef&trade_status=TRADE_SUCCESS"

	got := LogSafeRequestURI(uri)
	if !strings.HasPrefix(got, "/api/user/epay/notify?") {
		t.Fatalf("应保留请求路径，实际 %q", got)
	}
	if strings.Contains(got, "sign=deadbeef") || strings.Contains(got, "pid=1001") {
		t.Fatalf("日志不得包含 query 原文：%q", got)
	}
	if got == uri {
		t.Fatal("query 必须被替换")
	}
	if got != LogSafeRequestURI(uri) {
		t.Fatal("同一请求应得到同一指纹，便于日志关联")
	}
	if LogSafeRequestURI("/api/user/epay/notify?x=1") == got {
		t.Fatal("不同 query 应产生不同指纹")
	}
}

func TestLogSafeRequestURINoQuery(t *testing.T) {
	if got := LogSafeRequestURI("/api/user/epay/notify"); got != "/api/user/epay/notify" {
		t.Fatalf("无 query 时应原样返回路径，实际 %q", got)
	}
	if got := LogSafeRequestURI("/api/user/epay/notify?"); got != "/api/user/epay/notify" {
		t.Fatalf("空 query 时应只返回路径，实际 %q", got)
	}
}

// TestPaymentWebhookLogsDoNotContainRawParams 静态守卫：日志格式串是中文
// 翻译表的 key，因此只能替换参数值。这里确认所有支付 webhook 的源码中
// 都不再有裸 RequestURI / 明文 params，防止后续重构悄悄退回明文。
func TestPaymentWebhookLogsDoNotContainRawParams(t *testing.T) {
	// 覆盖所有带 path=%q 日志的支付回调实现（Epay 上下单/订阅 + 其他渠道）。
	files := []string{
		"topup.go", "subscription_payment_epay.go",
		"topup_waffo.go", "topup_stripe.go", "topup_creem.go", "topup_waffo_pancake.go",
	}
	for _, path := range files {
		content, err := os.ReadFile(filepath.Join("..", "controller", path))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", path, err)
		}
		text := string(content)
		for _, line := range strings.Split(text, "\n") {
			if !strings.Contains(line, "LogText(") {
				continue
			}
			if strings.Contains(line, "c.Request.RequestURI, c.ClientIP()") {
				t.Errorf("%s 仍有未脱敏的 RequestURI 日志: %s", path, line)
			}
			if strings.Contains(line, "GetJsonString(params)") &&
				!strings.Contains(line, "LogFingerprint(common.GetJsonString(params))") {
				t.Errorf("%s 仍有未脱敏的 params 日志: %s", path, line)
			}
		}
	}
}
