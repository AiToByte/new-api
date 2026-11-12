package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openSearchChannelsTestDB 建一个仅含 channels 表的 sqlite 内存库。
// 与 model 层测试分开，避免跨包共享全局状态。
func openSearchChannelsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	model.DB, model.LOG_DB = db, db

	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		_ = sqlDB.Close()
	})
	return db
}

func seedHandlerChannels(t *testing.T, db *gorm.DB) {
	t.Helper()
	tag := func(s string) *string { return &s }
	priority := func(v int64) *int64 { return &v }
	url := func(s string) *string { return &s }
	rows := []*model.Channel{
		{Id: 1, Name: "alpha", Key: "sk-1", Status: common.ChannelStatusEnabled, Type: 1, Group: "default", Models: "gpt-4", BaseURL: url("https://a.example"), Tag: tag("t1"), Priority: priority(10)},
		{Id: 2, Name: "beta", Key: "sk-2", Status: common.ChannelStatusEnabled, Type: 2, Group: "default", Models: "gpt-4", BaseURL: url("https://b.example"), Tag: tag("t1"), Priority: priority(9)},
		{Id: 3, Name: "gamma", Key: "sk-3", Status: 2, Type: 1, Group: "default", Models: "gpt-4", BaseURL: url("https://c.example"), Tag: tag("t2"), Priority: priority(8)},
		{Id: 4, Name: "delta", Key: "sk-4", Status: common.ChannelStatusEnabled, Type: 2, Group: "default", Models: "gpt-4", BaseURL: url("https://d.example"), Tag: tag("t2"), Priority: priority(7)},
		{Id: 5, Name: "epsilon", Key: "sk-5", Status: 3, Type: 3, Group: "default", Models: "gpt-4", BaseURL: url("https://e.example"), Tag: tag("t1"), Priority: priority(6)},
	}
	for _, row := range rows {
		require.NoError(t, db.Create(row).Error)
	}
}

type searchResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Items []struct {
			Id   int    `json:"id"`
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"items"`
		Total      int64            `json:"total"`
		TypeCounts map[string]int64 `json:"type_counts"`
	} `json:"data"`
}

func doSearchChannels(t *testing.T, query string) searchResponse {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/channel/search?"+query, nil)
	SearchChannels(c)
	require.Equal(t, http.StatusOK, recorder.Code)

	var resp searchResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success, "响应应成功: %s", recorder.Body.String())
	return resp
}

// TestSearchChannelsSQLPagination 验证 SQL 下推后的分页/总数与旧内存实现一致：
// 5 条渠道按 priority desc，page_size=2 时应逐页返回，total 恒为 5。
func TestSearchChannelsSQLPagination(t *testing.T) {
	db := openSearchChannelsTestDB(t)
	seedHandlerChannels(t, db)

	pages := [][]string{{"alpha", "beta"}, {"gamma", "delta"}, {"epsilon"}}
	for i, want := range pages {
		resp := doSearchChannels(t, "p="+strconv.Itoa(i+1)+"&page_size=2")
		require.Equal(t, int64(5), resp.Data.Total, "total 应为过滤前的总数")
		require.Len(t, resp.Data.Items, len(want))
		for j, name := range want {
			require.Equal(t, name, resp.Data.Items[j].Name)
		}
	}

	// 超出范围的页返回空 items，但 total 不变（旧实现 startIdx 取 min 的行为）。
	resp := doSearchChannels(t, "p=99&page_size=2")
	require.Empty(t, resp.Data.Items)
	require.Equal(t, int64(5), resp.Data.Total)
}

// TestSearchChannelsStatusAndTypeFilters 验证 status/type 过滤下推后结果一致，
// 且 type_counts 仍基于「status 过滤后、type 过滤前」的集合（切换 type 不跳变）。
func TestSearchChannelsStatusAndTypeFilters(t *testing.T) {
	db := openSearchChannelsTestDB(t)
	seedHandlerChannels(t, db)

	enabled := doSearchChannels(t, "status=enabled")
	require.Equal(t, int64(3), enabled.Data.Total, "alpha/beta/delta 为启用")
	require.Equal(t, int64(1), enabled.Data.TypeCounts["1"], "启用集中 type=1 仅 alpha")
	require.Equal(t, int64(2), enabled.Data.TypeCounts["2"], "启用集中 type=2 为 beta/delta")
	require.Zero(t, enabled.Data.TypeCounts["3"])

	disabled := doSearchChannels(t, "status=disabled")
	require.Equal(t, int64(2), disabled.Data.Total, "gamma/epsilon 为停用")

	typeTwo := doSearchChannels(t, "type=2")
	require.Equal(t, int64(2), typeTwo.Data.Total, "beta/delta")
	// type_counts 不受 type 过滤影响，仍是全量各类型计数。
	require.Equal(t, int64(2), typeTwo.Data.TypeCounts["1"])
	require.Equal(t, int64(2), typeTwo.Data.TypeCounts["2"])
	require.Equal(t, int64(1), typeTwo.Data.TypeCounts["3"])

	combined := doSearchChannels(t, "status=enabled&type=2")
	require.Equal(t, int64(2), combined.Data.Total)
	require.Len(t, combined.Data.Items, 2)
}

// TestSearchChannelsNeverLeaksKey 锁定 Omit("key") 安全约束。
func TestSearchChannelsNeverLeaksKey(t *testing.T) {
	db := openSearchChannelsTestDB(t)
	seedHandlerChannels(t, db)

	resp := doSearchChannels(t, "page_size=50")
	require.Len(t, resp.Data.Items, 5)
	for _, item := range resp.Data.Items {
		require.Empty(t, item.Key, "渠道密钥不得出现在搜索响应中")
	}
}

// TestSearchChannelsKeywordSearch 验证关键词搜索在下推后仍按 name/id 命中。
func TestSearchChannelsKeywordSearch(t *testing.T) {
	db := openSearchChannelsTestDB(t)
	seedHandlerChannels(t, db)

	resp := doSearchChannels(t, "keyword=beta&model=gpt-4")
	require.Equal(t, int64(1), resp.Data.Total)
	require.Len(t, resp.Data.Items, 1)
	require.Equal(t, "beta", resp.Data.Items[0].Name)
}

// TestSearchChannelsTagModeKeepsGrouping 验证 tag 模式保留「按 tag 序重组」，
// 且过滤下推后 total/items 仍与旧内存实现一致。
func TestSearchChannelsTagModeKeepsGrouping(t *testing.T) {
	db := openSearchChannelsTestDB(t)
	seedHandlerChannels(t, db)

	resp := doSearchChannels(t, "tag_mode=true&page_size=50")
	require.Equal(t, int64(5), resp.Data.Total)
	require.Len(t, resp.Data.Items, 5)
	// t1 组（alpha/beta/epsilon）应排在 t2 组（gamma/delta）之前。
	require.Equal(t, "t1", tagOf(t, db, resp.Data.Items[0].Id))
	for _, item := range resp.Data.Items {
		require.Empty(t, item.Key)
	}

	filtered := doSearchChannels(t, "tag_mode=true&status=enabled&page_size=50")
	require.Equal(t, int64(3), filtered.Data.Total, "tag 模式的 status 过滤也应生效")
}

func tagOf(t *testing.T, db *gorm.DB, id int) string {
	t.Helper()
	var ch model.Channel
	require.NoError(t, db.Select("tag").First(&ch, id).Error)
	if ch.Tag == nil {
		return ""
	}
	return *ch.Tag
}
