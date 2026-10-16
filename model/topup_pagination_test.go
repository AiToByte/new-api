package model

import (
	"fmt"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openTopUpPaginationTestDB opens an isolated database per dialect. MySQL and
// PostgreSQL follow the repo convention of reading loopback DSNs from the
// environment and skip when unconfigured.
func openTopUpPaginationTestDB(t *testing.T, dialect string) *gorm.DB {
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
		t.Setenv("TOPUP_PAGINATION_TEST_DSN", dsn)
		db, _, err := chooseDB("TOPUP_PAGINATION_TEST_DSN", false)
		require.NoError(t, err)
		return db
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		t.Setenv("TOPUP_PAGINATION_TEST_DSN", dsn)
		db, _, err := chooseDB("TOPUP_PAGINATION_TEST_DSN", false)
		require.NoError(t, err)
		return db
	default:
		t.Fatalf("unknown dialect %q", dialect)
		return nil
	}
}

func insertTopUpsForPaginationTest(t *testing.T, userID int, prefix string, total int) {
	t.Helper()
	now := common.GetTimestamp()
	for i := 1; i <= total; i++ {
		topup := &TopUp{
			UserId:     userID,
			Amount:     int64(i * 100),
			Money:      float64(i),
			TradeNo:    fmt.Sprintf("%s-%03d", prefix, i),
			CreateTime: now,
			Status:     "completed",
		}
		require.NoError(t, DB.Create(topup).Error)
	}
}

func collectTopUpTradeNos(topups []*TopUp) []string {
	tradeNos := make([]string, 0, len(topups))
	for _, topup := range topups {
		tradeNos = append(tradeNos, topup.TradeNo)
	}
	return tradeNos
}

// TestTopUpPaginationReadPaths guards the read-only pagination queries after
// their explicit transactions were removed: totals and id-desc page contents
// must stay identical on SQLite, MySQL and PostgreSQL.
func TestTopUpPaginationReadPaths(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openTopUpPaginationTestDB(t, dialect)
			require.NoError(t, db.AutoMigrate(&TopUp{}))
			require.NoError(t, db.Exec("DELETE FROM top_ups").Error)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() {
				_ = db.Migrator().DropTable(&TopUp{})
				_ = sqlDB.Close()
			})

			previousDB := DB
			DB = db
			t.Cleanup(func() { DB = previousDB })

			insertTopUpsForPaginationTest(t, 7, "order", 25)
			insertTopUpsForPaginationTest(t, 8, "guest", 3)

			pageOne, total, err := GetUserTopUps(7, &common.PageInfo{Page: 1, PageSize: 10})
			require.NoError(t, err)
			assert.Equal(t, int64(25), total)
			assert.Equal(t, []string{
				"order-025", "order-024", "order-023", "order-022", "order-021",
				"order-020", "order-019", "order-018", "order-017", "order-016",
			}, collectTopUpTradeNos(pageOne))

			pageThree, total, err := GetUserTopUps(7, &common.PageInfo{Page: 3, PageSize: 10})
			require.NoError(t, err)
			assert.Equal(t, int64(25), total)
			assert.Equal(t, []string{
				"order-005", "order-004", "order-003", "order-002", "order-001",
			}, collectTopUpTradeNos(pageThree))

			all, total, err := GetAllTopUps(&common.PageInfo{Page: 1, PageSize: 10})
			require.NoError(t, err)
			assert.Equal(t, int64(28), total)
			assert.Len(t, all, 10)

			found, total, err := SearchUserTopUps(7, "order-007", &common.PageInfo{Page: 1, PageSize: 10})
			require.NoError(t, err)
			assert.Equal(t, int64(1), total)
			require.Len(t, found, 1)
			assert.Equal(t, "order-007", found[0].TradeNo)

			guests, total, err := SearchAllTopUps("%guest%", &common.PageInfo{Page: 1, PageSize: 10})
			require.NoError(t, err)
			assert.Equal(t, int64(3), total)
			assert.Equal(t, []string{"guest-003", "guest-002", "guest-001"}, collectTopUpTradeNos(guests))
		})
	}
}
