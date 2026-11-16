package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestLoadOptionsFromDatabaseReportsReadFailure 锁定 A4 的核心不变量：
// DB 读失败必须向调用方返回错误，绝不能带着空数据集继续覆盖内存——
// 旧实现会在 options 为 nil 时执行 applyPasskeyDomainOptions(empty)，
// 把已有的 passkey 信任域名配置静默清空。
func TestLoadOptionsFromDatabaseReportsReadFailure(t *testing.T) {
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	// 故意不建 options 表，使 AllOption() 失败。
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		_ = sqlDB.Close()
	})

	previousMap := common.OptionMap
	common.OptionMap = map[string]string{
		"passkey.rp_id":   "example.com",
		"passkey.origins": "https://example.com",
	}
	previousSettings := *system_setting.GetPasskeySettings()
	t.Cleanup(func() {
		common.OptionMap = previousMap
		*system_setting.GetPasskeySettings() = previousSettings
	})

	err = loadOptionsFromDatabase()
	require.Error(t, err, "DB 读失败必须返回错误")

	// 读失败时不能清空既有的 passkey 配置。
	common.OptionMapRWMutex.RLock()
	rpID := common.OptionMap["passkey.rp_id"]
	common.OptionMapRWMutex.RUnlock()
	require.Equal(t, "example.com", rpID, "DB 读失败不得清空 passkey 信任域名")
}

// TestLoadOptionsFromDatabaseAppliesValidRows 验证成功路径仍正常加载，
// 确保上面的失败测试不是因为前置条件提前返回。
func TestLoadOptionsFromDatabaseAppliesValidRows(t *testing.T) {
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&Option{}))
	require.NoError(t, db.Create(&Option{Key: "A4TestLoadedOption", Value: "loaded"}).Error)
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		_ = sqlDB.Close()
	})

	previousMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previousMap })

	require.NoError(t, loadOptionsFromDatabase())
	common.OptionMapRWMutex.RLock()
	require.Equal(t, "loaded", common.OptionMap["A4TestLoadedOption"])
	common.OptionMapRWMutex.RUnlock()
}
