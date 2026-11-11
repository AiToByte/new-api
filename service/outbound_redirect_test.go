package service

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestProtectedRedirectRejectsInternalTargets 验证统一的重定向策略：
// 管理员配置的公网目标若被 302 到云元数据或内网地址，绝不能跟随。
// ratio_sync 与 uptime_kuma 的外发客户端都复用了这个策略。
func TestProtectedRedirectRejectsInternalTargets(t *testing.T) {
	configureSSRFTestFetchSetting(t)

	for _, rawURL := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://127.0.0.1/admin",
		"http://192.168.1.1/",
	} {
		t.Run(rawURL, func(t *testing.T) {
			parsed, err := url.Parse(rawURL)
			require.NoError(t, err)
			req := &http.Request{URL: parsed}
			require.Error(t, checkProtectedFetchRedirect(req, []*http.Request{{}}))
		})
	}
}

func TestProtectedRedirectAllowsPublicTarget(t *testing.T) {
	configureSSRFTestFetchSetting(t)
	parsed, err := url.Parse("https://example.com/api/v1/models")
	require.NoError(t, err)
	req := &http.Request{URL: parsed}
	require.NoError(t, checkProtectedFetchRedirect(req, []*http.Request{{}}))
}

// TestOutboundClientsDisableRedirectEscape 锁定两处外发客户端都挂了重定向
// 防护，防止后续重构时悄悄退回「默认跟随 302」的裸 client。
func TestOutboundClientsDisableRedirectEscape(t *testing.T) {
	configureSSRFTestFetchSetting(t)

	parsed, err := url.Parse("http://169.254.169.254/latest/meta-data/")
	require.NoError(t, err)
	req := &http.Request{URL: parsed}

	for name, client := range map[string]*http.Client{
		"uptime_kuma": newUptimeKumaHTTPClient(),
		"ratio_sync":  newRatioSyncHTTPClient(),
	} {
		t.Run(name, func(t *testing.T) {
			require.NotNil(t, client)
			require.NotNil(t, client.CheckRedirect, "外发客户端必须显式设置 CheckRedirect")
			require.Error(t, client.CheckRedirect(req, []*http.Request{{}}))
		})
	}
}
