package common

import (
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
