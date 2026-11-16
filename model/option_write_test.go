package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openOptionWriteFailureTestDB 提供一个故意不建 options 表的内存库，
// 使 FirstOrCreate / Save 必定失败，用于验证写错误不会被吞掉。
func initOptionMapForTest(t *testing.T) {
	t.Helper()
	previous := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previous })
}

func openOptionWriteFailureTestDB(t *testing.T) {
	t.Helper()
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		_ = sqlDB.Close()
	})
}

// TestUpdateOptionReportsDatabaseWriteFailure 锁定 A3：
// 数据库写失败时 UpdateOption 必须返回 error，且不能把未落库的值写进
// OptionMap（否则内存生效、重启后配置静默回滚）。
func TestUpdateOptionReportsDatabaseWriteFailure(t *testing.T) {
	openOptionWriteFailureTestDB(t)
	initOptionMapForTest(t)

	const key = "A3TestWriteFailureOption"
	const value = "should-not-be-committed"

	common.OptionMapRWMutex.Lock()
	previous, existed := common.OptionMap[key]
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		if existed {
			common.OptionMap[key] = previous
		} else {
			delete(common.OptionMap, key)
		}
		common.OptionMapRWMutex.Unlock()
	})

	err := UpdateOption(key, value)
	require.Error(t, err, "数据库写失败必须向调用方返回错误")

	common.OptionMapRWMutex.RLock()
	got, present := common.OptionMap[key]
	common.OptionMapRWMutex.RUnlock()
	if present {
		require.NotEqual(t, value, got, "写库失败时不得更新内存 OptionMap")
	}
}

// TestUpdateOptionPersistsBeforeUpdatingMemory 验证成功路径：
// 建表后写入应返回 nil 且内存同步，确保上面的失败测试不是因为
// validateOptionValue 之类前置校验提前拦截。
func TestUpdateOptionPersistsBeforeUpdatingMemory(t *testing.T) {
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&Option{}))
	DB = db
	initOptionMapForTest(t)
	t.Cleanup(func() {
		DB = previousDB
		_ = sqlDB.Close()
	})

	const key = "A3TestSuccessOption"
	const value = "persisted"
	common.OptionMapRWMutex.Lock()
	previous, existed := common.OptionMap[key]
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		if existed {
			common.OptionMap[key] = previous
		} else {
			delete(common.OptionMap, key)
		}
		common.OptionMapRWMutex.Unlock()
	})

	require.NoError(t, UpdateOption(key, value))

	var stored Option
	require.NoError(t, db.Where("key = ?", key).First(&stored).Error)
	require.Equal(t, value, stored.Value)
	common.OptionMapRWMutex.RLock()
	require.Equal(t, value, common.OptionMap[key])
	common.OptionMapRWMutex.RUnlock()
}
