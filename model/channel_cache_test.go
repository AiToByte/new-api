package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openChannelCacheTestDB 用 sqlite 内存库初始化渠道缓存，返回原状态供恢复。
func openChannelCacheTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousMemoryCache := common.MemoryCacheEnabled
	previousGroups, previousIDM, previousIndex := group2model2channels, channelsIDM, channelGroupModelIndex

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))
	DB, LOG_DB = db, db
	common.MemoryCacheEnabled = true

	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.MemoryCacheEnabled = previousMemoryCache
		group2model2channels, channelsIDM, channelGroupModelIndex = previousGroups, previousIDM, previousIndex
		_ = sqlDB.Close()
	})
	return db
}

func seedCacheChannels(t *testing.T, db *gorm.DB) {
	t.Helper()
	rows := []*Channel{
		{Id: 1, Name: "a", Key: "k1", Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeOpenAI, Group: "default,vip", Models: "gpt-4,gpt-3", BaseURL: ptr("https://a"), Priority: ptr64(10)},
		{Id: 2, Name: "b", Key: "k2", Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeOpenAI, Group: "default", Models: "gpt-4", BaseURL: ptr("https://b"), Priority: ptr64(9)},
		{Id: 3, Name: "c", Key: "k3", Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeOpenAI, Group: "vip", Models: "gpt-3", BaseURL: ptr("https://c"), Priority: ptr64(8)},
	}
	for _, row := range rows {
		require.NoError(t, db.Create(row).Error)
	}
	// abilities 决定 group2model2channels 的 group 初始化；用渠道的 group 集合即可。
	for _, g := range []string{"default", "vip"} {
		require.NoError(t, db.Create(&Ability{Group: g, Model: "*", ChannelId: 1}).Error)
	}
	InitChannelCache()
}

func ptr(s string) *string { return &s }
func ptr64(v int64) *int64 { return &v }

func collectBucketIDs() map[int]bool {
	seen := make(map[int]bool)
	for _, m := range group2model2channels {
		for _, list := range m {
			for _, id := range list {
				seen[id] = true
			}
		}
	}
	return seen
}

// TestCacheUpdateStatusUsesIndex 验证 A6：按预建索引删除渠道，结果与
// 全表遍历完全一致（每个桶里都不再含被下线渠道，其余渠道不受影响）。
func TestCacheUpdateStatusUsesIndex(t *testing.T) {
	db := openChannelCacheTestDB(t)
	seedCacheChannels(t, db)

	require.NotEmpty(t, channelGroupModelIndex[1], "索引应记录渠道 1 的所有桶")
	seen := collectBucketIDs()
	for _, id := range []int{1, 2, 3} {
		require.True(t, seen[id], "初始缓存应包含渠道 %d", id)
	}

	CacheUpdateChannelStatus(1, 2) // 下线渠道 1

	seen = collectBucketIDs()
	require.False(t, seen[1], "下线渠道必须从所有桶移除")
	require.True(t, seen[2])
	require.True(t, seen[3])
	require.Len(t, seen, 2)
}

// TestCacheUpdateStatusDoesNotMutateCachedChannel 验证 A5：更新状态必须
// 浅拷贝后替换指针，旧读者持有的快照不能被原地改写。
func TestCacheUpdateStatusDoesNotMutateCachedChannel(t *testing.T) {
	db := openChannelCacheTestDB(t)
	seedCacheChannels(t, db)

	old := channelsIDM[1]
	require.NotNil(t, old)
	require.Equal(t, common.ChannelStatusEnabled, old.Status)

	CacheUpdateChannelStatus(1, 2)

	require.Equal(t, common.ChannelStatusEnabled, old.Status, "旧快照不得被原地改写")
	updated := channelsIDM[1]
	require.NotSame(t, old, updated, "必须替换为新指针")
	require.Equal(t, 2, updated.Status)
}

// TestCacheUpdateStatusConcurrentReaders 用并发读者验证 A5 的不变量：
// 状态更新期间，任何已取到的旧指针都不可能被原地改写。
// 本机无 gcc 无法启用 -race，因此用「读者持续校验快照 Status 恒定」
// 的确定性断言替代——旧实现直接写 channel.Status 必然让该断言失败。
func TestCacheUpdateStatusConcurrentReaders(t *testing.T) {
	db := openChannelCacheTestDB(t)
	seedCacheChannels(t, db)

	old := channelsIDM[1]
	require.Equal(t, common.ChannelStatusEnabled, old.Status)

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			// 读者持有的旧快照必须始终是初始状态。
			if old.Status != common.ChannelStatusEnabled {
				t.Errorf("旧快照被原地改写: status=%d", old.Status)
				return
			}
		}
	}()

	for range 100 {
		CacheUpdateChannelStatus(1, 2)
		CacheUpdateChannelStatus(1, common.ChannelStatusEnabled)
	}
	close(stop)
	<-done
}

// TestCacheUpdateStatusIndexMatchesBuckets 验证索引内容与桶成员一致，
// 防止后续重构只改一边导致下线渠道漏删（索引缺失会留下幽灵渠道）。
func TestCacheUpdateStatusIndexMatchesBuckets(t *testing.T) {
	db := openChannelCacheTestDB(t)
	seedCacheChannels(t, db)

	for id, locations := range channelGroupModelIndex {
		for _, loc := range locations {
			require.Contains(t, group2model2channels[loc.group][loc.model], id,
				"索引记录的桶必须包含该渠道 id")
		}
	}
}
