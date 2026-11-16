package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// TestRecordAuditLogReportsUnavailableDatabase 验证审计库不可用时
// RecordAuditLog 会向调用方返回错误，而不是只打印日志后静默返回。
// 这是 A2 修复的基础：controller 必须能据此决定是否标记「已审计」。
func TestRecordAuditLogReportsUnavailableDatabase(t *testing.T) {
	previous := LOG_DB
	LOG_DB = nil
	t.Cleanup(func() { LOG_DB = previous })

	err := RecordAuditLog(nil, AuditLog{
		UserId:    1,
		Username:  "tester",
		ActorRole: common.RoleCommonUser,
		Category:  AuditCategorySecurity,
		Success:   true,
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrAuditLogUnavailable))
}
