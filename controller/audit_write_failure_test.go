package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func auditTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/api/test", nil)
	return c
}

// TestMarkAuditLoggedOnSuccess 锁定 A2 的核心安全语义：
// 审计写失败时绝不能标记「已审计」，否则 middleware.finishAdminAudit
// 见到标记就跳过兜底，形成「精细日志失败 + 兜底被抑制」的零留痕。
func TestMarkAuditLoggedOnSuccess(t *testing.T) {
	t.Run("写入成功才标记", func(t *testing.T) {
		c := auditTestContext(t)
		markAuditLoggedOnSuccess(c, nil)
		require.True(t, common.GetContextKeyBool(c, constant.ContextKeyAuditLogged))
	})

	t.Run("写入失败不标记", func(t *testing.T) {
		c := auditTestContext(t)
		markAuditLoggedOnSuccess(c, errAuditWriteFailedForTest{})
		require.False(t, common.GetContextKeyBool(c, constant.ContextKeyAuditLogged),
			"审计写失败必须保留兜底审计链路")
	})
}

type errAuditWriteFailedForTest struct{}

func (errAuditWriteFailedForTest) Error() string { return "simulated audit write failure" }

// TestMarkAuditLoggedStillAvailableForReadOnly 保留只读查询（get_key_status）
// 主动跳过兜底的既有语义：这类端点本就不写审计，直接标记是刻意为之。
func TestMarkAuditLoggedStillAvailableForReadOnly(t *testing.T) {
	c := auditTestContext(t)
	markAuditLogged(c)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyAuditLogged))
}
