package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openSubscriptionPlanTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&SubscriptionPlan{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		_ = sqlDB.Close()
	})
	return db
}

// Admin plan edits must roundtrip every field, including explicit zero values
// (Enabled=false must persist, not be skipped) while nil-able pointer fields
// stay untouched when nil. This guards the updateMap field list against
// drifting from the struct.
func TestSubscriptionPlanAdminCRUD(t *testing.T) {
	db := openSubscriptionPlanTestDB(t)

	plan := &SubscriptionPlan{
		Title:                   "Pro monthly",
		Subtitle:                "subtitle",
		PriceAmount:             29,
		Currency:                "USD",
		DurationUnit:            SubscriptionDurationMonth,
		DurationValue:           1,
		CustomSeconds:           0,
		Enabled:                 true,
		SortOrder:               3,
		StripePriceId:           "stripe-1",
		CreemProductId:          "creem-1",
		WaffoPancakeProductId:   "waffo-1",
		MaxPurchasePerUser:      2,
		TotalAmount:             1000,
		UpgradeGroup:            "pro",
		DowngradeGroup:          "default",
		QuotaResetPeriod:        SubscriptionResetMonthly,
		QuotaResetCustomSeconds: 0,
	}
	require.NoError(t, CreateSubscriptionPlan(plan))
	require.NotZero(t, plan.Id)
	assert.NotZero(t, plan.CreatedAt, "BeforeCreate must stamp timestamps")

	updated := *plan
	updated.Title = "Pro monthly v2"
	updated.PriceAmount = 39.5
	updated.Enabled = false
	updated.SortOrder = 0
	updated.MaxPurchasePerUser = 0
	updated.AllowBalancePay = common.GetPointer(false)
	updated.AllowWalletOverflow = nil
	require.NoError(t, UpdateSubscriptionPlan(&updated))

	var stored SubscriptionPlan
	require.NoError(t, db.Where("id = ?", plan.Id).First(&stored).Error)
	assert.Equal(t, "Pro monthly v2", stored.Title)
	assert.Equal(t, 39.5, stored.PriceAmount)
	assert.False(t, stored.Enabled, "explicit false must be written, not skipped")
	assert.Zero(t, stored.SortOrder)
	assert.Zero(t, stored.MaxPurchasePerUser)
	require.NotNil(t, stored.AllowBalancePay)
	assert.False(t, *stored.AllowBalancePay)
	assert.Nil(t, stored.AllowWalletOverflow, "nil pointer fields must stay untouched")

	require.NoError(t, UpdateSubscriptionPlanStatus(plan.Id, true))
	require.NoError(t, db.Where("id = ?", plan.Id).First(&stored).Error)
	assert.True(t, stored.Enabled)

	require.Error(t, UpdateSubscriptionPlan(nil))
	require.Error(t, UpdateSubscriptionPlan(&SubscriptionPlan{}))
	require.Error(t, UpdateSubscriptionPlanStatus(0, true))
	require.Error(t, CreateSubscriptionPlan(nil))
}
