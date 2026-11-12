package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openChannelSearchTestDB 建一个仅含 channels 表的 sqlite 内存库。
// initCol 负责按方言初始化 commonKeyCol 等列名，必须在查询构造前调用。
func openChannelSearchTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	initCol()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	// 内存库每个连接各自独立，必须限制为单连接，否则分页查询会读到空库。
	sqlDB.SetMaxOpenConns(1)
	DB, LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&Channel{}))

	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		initCol()
		_ = sqlDB.Close()
	})
	return db
}

func strPtr(s string) *string { return &s }

// seedSearchChannels 插入覆盖多种 status / type / group / tag 的渠道，
// 每个渠道的 models 都包含 "gpt-4"，便于用关键字 "%" 命中全部。
func seedSearchChannels(t *testing.T, db *gorm.DB) {
	t.Helper()
	rows := []*Channel{
		{Id: 1, Name: "alpha", Key: "sk-alpha", Status: common.ChannelStatusEnabled, Type: 1, Group: "default", Models: "gpt-4", BaseURL: strPtr("https://a.example"), Tag: strPtr("t1"), Priority: ptrInt64(10)},
		{Id: 2, Name: "beta", Key: "sk-beta", Status: common.ChannelStatusEnabled, Type: 2, Group: "default", Models: "gpt-4,gpt-3", BaseURL: strPtr("https://b.example"), Tag: strPtr("t1"), Priority: ptrInt64(9)},
		{Id: 3, Name: "gamma", Key: "sk-gamma", Status: 2, Type: 1, Group: "default", Models: "gpt-4", BaseURL: strPtr("https://c.example"), Tag: strPtr("t2"), Priority: ptrInt64(8)},
		{Id: 4, Name: "delta", Key: "sk-delta", Status: common.ChannelStatusEnabled, Type: 2, Group: "vip", Models: "gpt-4", BaseURL: strPtr("https://d.example"), Tag: strPtr("t2"), Priority: ptrInt64(7)},
		{Id: 5, Name: "epsilon", Key: "sk-epsilon", Status: 3, Type: 3, Group: "default", Models: "claude", BaseURL: strPtr("https://e.example"), Tag: strPtr("t1"), Priority: ptrInt64(6)},
	}
	for _, row := range rows {
		require.NoError(t, db.Create(row).Error)
	}
}

func ptrInt64(v int64) *int64 { return &v }

func countSearchRows(t *testing.T, statusFilter int, typeFilter int) int64 {
	t.Helper()
	var total int64
	require.NoError(t, NewChannelSearchQuery("", "default", "", statusFilter, typeFilter).Count(&total).Error)
	return total
}

// TestChannelSearchQueryFiltersAndCounts 验证 status/type/group/model 谓词
// 下推后的计数，与旧内存过滤结果一致。
func TestChannelSearchQueryFiltersAndCounts(t *testing.T) {
	db := openChannelSearchTestDB(t)
	seedSearchChannels(t, db)

	// group=default 时排除 vip 的 delta，且 model 关键字为空会命中 "%"，
	// 因此 default 组共 4 条（epsilon 的 models=claude 不含 gpt-4 但 "%"
	// 关键字为空时 LIKE %% 仍命中）。
	require.Equal(t, int64(4), countSearchRows(t, -1, -1), "全部 default 渠道")
	require.Equal(t, int64(2), countSearchRows(t, common.ChannelStatusEnabled, -1), "仅启用")
	require.Equal(t, int64(2), countSearchRows(t, 0, -1), "仅停用（status != enabled）")
	require.Equal(t, int64(2), countSearchRows(t, -1, 1), "type=1（alpha + gamma）")
	require.Equal(t, int64(1), countSearchRows(t, -1, 2), "type=2（仅 beta，delta 属 vip 组）")
	require.Equal(t, int64(1), countSearchRows(t, common.ChannelStatusEnabled, 2), "启用且 type=2")
}

// TestChannelSearchQueryPaginates 验证 Limit/Offset 下推后只返回本页数据，
// 且按 priority desc 排序，不发生全表加载后的切片错位。
func TestChannelSearchQueryPaginates(t *testing.T) {
	db := openChannelSearchTestDB(t)
	seedSearchChannels(t, db)

	sortOptions := NewChannelSortOptions("priority", "desc", false)
	readPage := func(pageSize int, offset int) []*Channel {
		var rows []*Channel
		require.NoError(t, sortOptions.Apply(
			NewChannelSearchQuery("", "default", "", -1, -1)).
			Limit(pageSize).Offset(offset).Find(&rows).Error)
		return rows
	}

	first := readPage(2, 0)
	require.Len(t, first, 2)
	require.Equal(t, "alpha", first[0].Name)
	require.Equal(t, "beta", first[1].Name)

	second := readPage(2, 2)
	require.Len(t, second, 2)
	require.Equal(t, "gamma", second[0].Name)
	require.Equal(t, "epsilon", second[1].Name)

	beyond := readPage(2, 4)
	require.Empty(t, beyond)
}

// TestChannelSearchQueryOmitsKey 锁定 Omit("key")：渠道密钥不应被读入内存，
// 更不能随 API 响应泄露。
func TestChannelSearchQueryOmitsKey(t *testing.T) {
	db := openChannelSearchTestDB(t)
	seedSearchChannels(t, db)

	var rows []*Channel
	require.NoError(t, NewChannelSearchQuery("", "default", "", -1, -1).Find(&rows).Error)
	require.NotEmpty(t, rows)
	for _, row := range rows {
		require.Empty(t, row.Key, "查询结果不应包含渠道 key")
	}
}

// TestChannelTypeCountsIgnoresTypeFilter 验证 type_counts 的既有语义：
// 传入不含 type 过滤的查询，各类型计数在切换 type 筛选时不跳变。
func TestChannelTypeCountsIgnoresTypeFilter(t *testing.T) {
	db := openChannelSearchTestDB(t)
	seedSearchChannels(t, db)

	var counts []ChannelTypeCount
	require.NoError(t, ChannelTypeCounts(
		NewChannelSearchQuery("", "default", "", -1, -1), &counts))

	got := make(map[int64]int64, len(counts))
	for _, r := range counts {
		got[r.Type] = r.Count
	}
	require.Equal(t, int64(2), got[1])
	require.Equal(t, int64(1), got[2])
	require.Equal(t, int64(1), got[3])

	// 加上 status 过滤后，type=1 只剩 gamma，type=2 只剩 beta。
	var enabledCounts []ChannelTypeCount
	require.NoError(t, ChannelTypeCounts(
		NewChannelSearchQuery("", "default", "", common.ChannelStatusEnabled, -1), &enabledCounts))
	got = make(map[int64]int64, len(enabledCounts))
	for _, r := range enabledCounts {
		got[r.Type] = r.Count
	}
	require.Equal(t, int64(1), got[1])
	require.Equal(t, int64(1), got[2])
	require.Zero(t, got[3])
}

// TestChannelSearchQueryKeywordPredicate 验证 id/name/key/base_url 的搜索
// 谓词与 model 关键字条件在下推后仍然生效。
func TestChannelSearchQueryKeywordPredicate(t *testing.T) {
	db := openChannelSearchTestDB(t)
	seedSearchChannels(t, db)

	var rows []*Channel
	require.NoError(t, NewChannelSearchQuery("beta", "default", "gpt-4", -1, -1).Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "beta", rows[0].Name)

	// model 关键字不匹配时应为空。
	var none []*Channel
	require.NoError(t, NewChannelSearchQuery("beta", "default", "nonexistent-model", -1, -1).Find(&none).Error)
	require.Empty(t, none)
}
