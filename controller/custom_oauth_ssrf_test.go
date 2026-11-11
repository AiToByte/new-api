package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// setFetchPolicyForTest 覆盖系统 fetch 策略并在测试结束后恢复现场。
// 参数 allowPrivate 控制是否允许访问本机 httptest 服务。
func setFetchPolicyForTest(t *testing.T, allowPrivate bool) {
	t.Helper()
	setting := system_setting.GetFetchSetting()
	original := *setting
	t.Cleanup(func() { *setting = original })

	setting.EnableSSRFProtection = true
	setting.AllowPrivateIp = allowPrivate
	setting.DomainFilterMode = false
	setting.IpFilterMode = false
	setting.DomainList = nil
	setting.IpList = nil
	setting.AllowedPorts = []string{"1-65535"}
	setting.ApplyIPFilterForDomain = true
}

func discoveryRequest(t *testing.T, rawURL string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/custom-oauth-provider/discovery",
		strings.NewReader(`{"well_known_url":"`+rawURL+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	FetchCustomOAuthDiscovery(c)
	return recorder
}

// TestCustomOAuthDiscoveryRejectsInternalTargets 覆盖本次 SSRF 修复：
// root 会话曾可通过 discovery 接口让服务器请求云元数据/内网地址，并把上游
// 错误体与 JSON 原样回显。现在校验必须在建立连接之前拒绝。
func TestCustomOAuthDiscoveryRejectsInternalTargets(t *testing.T) {
	setFetchPolicyForTest(t, false)

	for _, rawURL := range []string{
		"http://169.254.169.254/latest/meta-data/iam/security-credentials/",
		"http://127.0.0.1:80/admin",
		"http://192.168.1.1/",
		"http://10.0.0.1/",
		"http://localhost/.well-known/openid-configuration",
	} {
		t.Run(rawURL, func(t *testing.T) {
			recorder := discoveryRequest(t, rawURL)
			require.Contains(t, recorder.Body.String(), "not allowed by the server's fetch policy",
				"内网/元数据目标必须在发请求前被拒绝")
		})
	}
}

// TestCustomOAuthDiscoveryFetchesAllowedTarget 验证放行路径仍然可用：
// 显式允许内网 IP 后，本机 httptest 服务返回的 discovery JSON 应被正常解析。
// 全程离线，不依赖公网或 Docker。
func TestCustomOAuthDiscoveryFetchesAllowedTarget(t *testing.T) {
	setFetchPolicyForTest(t, true)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issuer":"https://issuer.example","jwks_uri":"https://issuer.example/keys"}`))
	}))
	defer server.Close()

	recorder := discoveryRequest(t, server.URL+"/.well-known/openid-configuration")
	require.NotContains(t, recorder.Body.String(), "not allowed by the server's fetch policy")
	require.Contains(t, recorder.Body.String(), `"issuer":"https://issuer.example"`,
		"放行后应能正常取回并解析 discovery 文档")
}

// TestCustomOAuthDiscoveryRejectsUnsupportedScheme 保留原有的协议白名单。
func TestCustomOAuthDiscoveryRejectsUnsupportedScheme(t *testing.T) {
	setFetchPolicyForTest(t, false)
	recorder := discoveryRequest(t, "file:///etc/passwd")
	require.Contains(t, recorder.Body.String(), "Only http and https are supported")
}
