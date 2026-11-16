package controller

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBatchAddChannelNeverLeaksKeyPrefix 静态守卫：批量导入时把 key 前缀
// 拼进 Name 会让密钥片段随 ChannelRead 与 channel.key_view 审计外泄。
// 现在改用不可逆指纹，确保后续重构不会退回明文前缀。
func TestBatchAddChannelNeverLeaksKeyPrefix(t *testing.T) {
	content, err := os.ReadFile("channel.go")
	require.NoError(t, err)
	text := string(content)

	require.NotContains(t, text, "localChannel.Key[:8]",
		"不得再从 key 取明文前缀写入 Name")
	require.Contains(t, text, "common.LogFingerprint(localChannel.Key)",
		"批量导入的 Name 后缀必须使用不可逆指纹")
}
