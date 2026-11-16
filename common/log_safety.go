package common

import "strings"

// 日志脱敏工具。
//
// 支付回调日志里出现过 webhook 签名、完整回调 body、兑换码原文等可用凭据：
// 任何能读日志的人都可以拿签名重放回调，或直接消费兑换码。这些值必须
// 从日志中彻底移除，但排障时又需要区分「同一个请求是否重复」「两个回调
// 是否为同一笔」，因此用不可逆指纹代替明文。
//
// 选择 HMAC-SHA256 而非裸 SHA-256：签名/兑换码虽有一定随机性，裸哈希仍可能
// 被字典或彩虹表反推；加上服务端 CryptoSecret 后，没有密钥的攻击者无法
// 从指纹反推原文。取前 16 个十六进制字符（64 bit）足以在日志窗口内唯一
// 关联同一值，碰撞概率可忽略。
//
// 注意：LogText 的格式串同时是中文日志翻译表的 key（见 log_text.zh-CN.json）。
// 脱敏只替换传入的参数值，不改动格式串，因此既有翻译条目继续生效。

// LogFingerprint 把敏感值转换为可用于日志关联的不可逆短指纹。
// 空值返回固定标记，避免把「未提供」与「提供了空串」混淆成同一指纹。
func LogFingerprint(value string) string {
	if value == "" {
		return "empty"
	}
	return HmacSha256(value, CryptoSecret)[:16]
}

// LogSafeRequestURI 保留请求路径、把查询串替换为不可逆指纹。
//
// 支付回调（尤其是 Epay 的 GET 回调）会把签名与交易参数放在 query string
// 中，直接记录 c.Request.RequestURI 等于把可重放的凭据写进日志；支付网关
// 返回的跳转地址同理。路径本身不含凭据，保留它便于定位是哪个回调入口；
// query 用指纹代替，既能关联同一笔请求，又不泄露原文。
//
// 与 LogFingerprint 一样，调用方只替换日志参数值，不改动 LogText 格式串，
// 因此既有中文翻译条目继续生效。
func LogSafeRequestURI(requestURI string) string {
	path, query, found := strings.Cut(requestURI, "?")
	if !found || query == "" {
		return path
	}
	return path + "?" + LogFingerprint(query)
}
