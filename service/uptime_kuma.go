package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/console_setting"
	"golang.org/x/sync/errgroup"
)

// Uptime Kuma status-page aggregation. The HTTP fan-out, decoding and
// monitor/heartbeat projection live here; the controller only renders the
// result.
//
// 安全边界：状态页 URL 由管理员在控制台配置，属于 operator-managed 目标，
// 与渠道 BaseURL 同类，因此允许指向内网自建的 Uptime Kuma（不能一刀切套用
// AllowPrivateIp=false 的受保护客户端，否则会误伤正常部署）。但 /api/uptime/status
// 是匿名接口，任何人都可触发外发，所以必须禁止「重定向逃逸」：管理员配置的
// 公网 URL 若 302 到云元数据或内网地址，绝不能跟随。重定向校验复用系统统一的
// 受保护策略，初始目标本身仍由管理员负责。

const (
	requestTimeout   = 30 * time.Second
	httpTimeout      = 10 * time.Second
	uptimeKeySuffix  = "_24"
	apiStatusPath    = "/api/status-page/"
	apiHeartbeatPath = "/api/status-page/heartbeat/"
)

type Monitor struct {
	Name   string  `json:"name"`
	Uptime float64 `json:"uptime"`
	Status int     `json:"status"`
	Group  string  `json:"group,omitempty"`
}

type UptimeGroupResult struct {
	CategoryName string    `json:"categoryName"`
	Monitors     []Monitor `json:"monitors"`
}

func getAndDecode(ctx context.Context, client *http.Client, url string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("non-200 status")
	}

	return json.NewDecoder(resp.Body).Decode(dest)
}

func fetchGroupData(ctx context.Context, client *http.Client, groupConfig map[string]any) UptimeGroupResult {
	url, _ := groupConfig["url"].(string)
	slug, _ := groupConfig["slug"].(string)
	categoryName, _ := groupConfig["categoryName"].(string)

	result := UptimeGroupResult{
		CategoryName: categoryName,
		Monitors:     []Monitor{},
	}

	if url == "" || slug == "" {
		return result
	}

	baseURL := strings.TrimSuffix(url, "/")

	var statusData struct {
		PublicGroupList []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			MonitorList []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"monitorList"`
		} `json:"publicGroupList"`
	}

	var heartbeatData struct {
		HeartbeatList map[string][]struct {
			Status int `json:"status"`
		} `json:"heartbeatList"`
		UptimeList map[string]float64 `json:"uptimeList"`
	}

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return getAndDecode(gCtx, client, baseURL+apiStatusPath+slug, &statusData)
	})
	g.Go(func() error {
		return getAndDecode(gCtx, client, baseURL+apiHeartbeatPath+slug, &heartbeatData)
	})

	if g.Wait() != nil {
		return result
	}

	for _, pg := range statusData.PublicGroupList {
		if len(pg.MonitorList) == 0 {
			continue
		}

		for _, m := range pg.MonitorList {
			monitor := Monitor{
				Name:  m.Name,
				Group: pg.Name,
			}

			monitorID := strconv.Itoa(m.ID)

			if uptime, exists := heartbeatData.UptimeList[monitorID+uptimeKeySuffix]; exists {
				monitor.Uptime = uptime
			}

			if heartbeats, exists := heartbeatData.HeartbeatList[monitorID]; exists && len(heartbeats) > 0 {
				monitor.Status = heartbeats[0].Status
			}

			result.Monitors = append(result.Monitors, monitor)
		}
	}

	return result
}

// newUptimeKumaHTTPClient 构建状态页抓取客户端。
// 只禁止重定向逃逸，不拦截初始目标（见文件头安全边界说明）。
// 单独抽出构造逻辑，便于直接回归测试该安全约束。
func newUptimeKumaHTTPClient() *http.Client {
	return &http.Client{Timeout: httpTimeout, CheckRedirect: checkProtectedFetchRedirect}
}

// FetchUptimeKumaGroups fetches every configured Uptime Kuma status page in
// parallel and returns one aggregated result per group. A failed group keeps
// its category name with an empty monitor list rather than failing the whole
// response.
func FetchUptimeKumaGroups(ctx context.Context) []UptimeGroupResult {
	groups := console_setting.GetUptimeKumaGroups()
	if len(groups) == 0 {
		return []UptimeGroupResult{}
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	client := newUptimeKumaHTTPClient()
	results := make([]UptimeGroupResult, len(groups))

	g, gCtx := errgroup.WithContext(ctx)
	for i, group := range groups {
		g.Go(func() error {
			results[i] = fetchGroupData(gCtx, client, group)
			return nil
		})
	}

	g.Wait()
	return results
}
