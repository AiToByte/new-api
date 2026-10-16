package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openTokenStatusTestDB 为令牌状态机测试按方言打开隔离数据库，
// MySQL/PostgreSQL 走仓库约定的 loopback DSN，未配置时跳过。
func openTokenStatusTestDB(t *testing.T, dialect string) *gorm.DB {
	t.Helper()

	switch dialect {
	case "sqlite":
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		return db
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		t.Setenv("TOKEN_STATUS_TEST_DSN", dsn)
		db, _, err := chooseDB("TOKEN_STATUS_TEST_DSN", false)
		require.NoError(t, err)
		return db
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		t.Setenv("TOKEN_STATUS_TEST_DSN", dsn)
		db, _, err := chooseDB("TOKEN_STATUS_TEST_DSN", false)
		require.NoError(t, err)
		return db
	default:
		t.Fatalf("unknown dialect %q", dialect)
		return nil
	}
}

// TestValidateUserTokenExhaustedHealing 锁定令牌状态机的自愈语义：
// 只有“耗尽且已有额度”可以自动恢复，管理员停用与过期永不自愈。
// 三种方言逐一验证，因为改动涉及真实的 UPDATE 写路径。
func TestValidateUserTokenExhaustedHealing(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		remainQuota    int
		unlimitedQuota bool
		wantValid      bool
		wantStatus     int
	}{
		{
			name:        "耗尽且已补额则恢复可用",
			status:      common.TokenStatusExhausted,
			remainQuota: 100,
			wantValid:   true,
			wantStatus:  common.TokenStatusEnabled,
		},
		{
			name:           "耗尽但改为不限额则恢复可用",
			status:         common.TokenStatusExhausted,
			remainQuota:    0,
			unlimitedQuota: true,
			wantValid:      true,
			wantStatus:     common.TokenStatusEnabled,
		},
		{
			name:        "耗尽且仍无额度则继续拒绝",
			status:      common.TokenStatusExhausted,
			remainQuota: 0,
			wantValid:   false,
			wantStatus:  common.TokenStatusExhausted,
		},
		{
			name:        "管理员停用即使有额度也不恢复",
			status:      common.TokenStatusDisabled,
			remainQuota: 100,
			wantValid:   false,
			wantStatus:  common.TokenStatusDisabled,
		},
		{
			name:        "过期即使有额度也不恢复",
			status:      common.TokenStatusExpired,
			remainQuota: 100,
			wantValid:   false,
			wantStatus:  common.TokenStatusExpired,
		},
		{
			name:        "正常令牌不受影响",
			status:      common.TokenStatusEnabled,
			remainQuota: 100,
			wantValid:   true,
			wantStatus:  common.TokenStatusEnabled,
		},
	}

	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openTokenStatusTestDB(t, dialect)
			require.NoError(t, db.AutoMigrate(&Token{}))
			require.NoError(t, db.Exec("DELETE FROM tokens").Error)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })

			// 列引用转义（反引号/MySQL/SQLite vs 双引号/PG）跟随主库类型，
			// 换 DB 必须同步切换并重算，否则跨方言 SQL 直接语法错误。
			previousDB := DB
			previousMainType := common.MainDatabaseType()
			previousLogType := common.LogDatabaseType()
			DB = db
			dialectType := common.DatabaseTypeSQLite
			if dialect == "mysql" {
				dialectType = common.DatabaseTypeMySQL
			}
			if dialect == "postgres" {
				dialectType = common.DatabaseTypePostgreSQL
			}
			common.SetDatabaseTypes(dialectType, dialectType)
			initCol()
			t.Cleanup(func() {
				DB = previousDB
				common.SetDatabaseTypes(previousMainType, previousLogType)
				initCol()
			})

			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					token := Token{
						UserId:         1,
						Key:            "heal-token-" + common.GetRandomString(8),
						Name:           "heal-test",
						Status:         tc.status,
						ExpiredTime:    -1,
						RemainQuota:    tc.remainQuota,
						UnlimitedQuota: tc.unlimitedQuota,
					}
					require.NoError(t, DB.Create(&token).Error)
					t.Cleanup(func() {
						DB.Unscoped().Delete(&Token{}, token.Id)
					})

					got, err := ValidateUserToken(token.Key)
					if tc.wantValid {
						require.NoError(t, err)
						require.NotNil(t, got)
					} else {
						require.ErrorIs(t, err, ErrTokenInvalid)
					}

					var persisted Token
					require.NoError(t, DB.First(&persisted, token.Id).Error)
					assert.Equal(t, tc.wantStatus, persisted.Status)
				})
			}
		})
	}
}

// TestReadCacheStatsTracksTokenLookups 验证读计数器跟随真实校验路径：
// 单测环境无 Redis，所有 GetTokenByKey 走数据库回退，miss 累加、hits 不动。
func TestReadCacheStatsTracksTokenLookups(t *testing.T) {
	before := ReadCacheStatsSnapshot()

	token := Token{
		UserId:      1,
		Key:         "stats-token-" + common.GetRandomString(8),
		Name:        "stats-test",
		Status:      common.TokenStatusEnabled,
		ExpiredTime: -1,
		RemainQuota: 100,
	}
	require.NoError(t, DB.Create(&token).Error)
	t.Cleanup(func() {
		DB.Unscoped().Delete(&Token{}, token.Id)
	})

	_, err := ValidateUserToken(token.Key)
	require.NoError(t, err)
	_, err = ValidateUserToken("nonexistent-key")
	require.ErrorIs(t, err, ErrTokenInvalid)

	after := ReadCacheStatsSnapshot()
	assert.Equal(t, before[ReadCacheScopeToken].Hits, after[ReadCacheScopeToken].Hits)
	assert.Greater(t, after[ReadCacheScopeToken].Misses, before[ReadCacheScopeToken].Misses)
}
