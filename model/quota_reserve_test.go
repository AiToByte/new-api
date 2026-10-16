package model

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func BenchmarkDecreaseTokenQuotaSQLite(b *testing.B) {
	token := createReserveTestTokenForBench(b, math.MaxInt32)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := decreaseTokenQuota(token.Id, 1); err != nil {
			b.Fatal(err)
		}
	}
}

func createReserveTestTokenForBench(b *testing.B, remainQuota int) Token {
	b.Helper()
	token := Token{
		UserId:      1,
		Key:         "reserve-bench-" + common.GetRandomString(8),
		Name:        "reserve-bench",
		Status:      common.TokenStatusEnabled,
		ExpiredTime: -1,
		RemainQuota: remainQuota,
	}
	require.NoError(b, token.Insert())
	b.Cleanup(func() {
		DB.Unscoped().Delete(&Token{}, token.Id)
	})
	return token
}

func createReserveTestUser(t *testing.T, quota int) User {
	user := User{
		Username:    "reserve-user-" + common.GetRandomString(6),
		Password:    "unused-password-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
		Quota:       quota,
		AffCode:     "reserve-aff-" + common.GetRandomString(8),
	}
	require.NoError(t, DB.Create(&user).Error)
	return user
}

func createReserveTestToken(t *testing.T, remainQuota int) Token {
	t.Helper()
	token := Token{
		UserId:      1,
		Key:         "reserve-token-" + common.GetRandomString(8),
		Name:        "reserve-test",
		Status:      common.TokenStatusEnabled,
		ExpiredTime: -1,
		RemainQuota: remainQuota,
	}
	require.NoError(t, token.Insert())
	return token
}

func getUserQuotaFromDB(t *testing.T, id int) int {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("quota").First(&user, id).Error)
	return user.Quota
}

func getTokenFromDB(t *testing.T, id int) Token {
	t.Helper()
	var token Token
	require.NoError(t, DB.First(&token, id).Error)
	return token
}

func resetBatchUpdateTestState(t *testing.T) {
	t.Helper()
	oldBatchEnabled := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = false
	for i := range BatchUpdateTypeCount {
		batchUpdateLocks[i].Lock()
		batchUpdateStores[i] = make(map[int]int)
		batchUpdateLocks[i].Unlock()
	}
	t.Cleanup(func() {
		common.BatchUpdateEnabled = oldBatchEnabled
		for i := range BatchUpdateTypeCount {
			batchUpdateLocks[i].Lock()
			batchUpdateStores[i] = make(map[int]int)
			batchUpdateLocks[i].Unlock()
		}
	})
}

func TestTryReserveQuotaWithoutRedis(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)

	user := createReserveTestUser(t, 100)
	reserved, err := TryReserveUserQuota(user.Id, 60)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, 40, getUserQuotaFromDB(t, user.Id))

	reserved, err = TryReserveUserQuota(user.Id, 41)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 40, getUserQuotaFromDB(t, user.Id))

	token := createReserveTestToken(t, 80)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 25, false)
	require.NoError(t, err)
	assert.True(t, reserved)
	reloaded := getTokenFromDB(t, token.Id)
	assert.Equal(t, 55, reloaded.RemainQuota)
	assert.Equal(t, 25, reloaded.UsedQuota)

	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 56, false)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 55, getTokenFromDB(t, token.Id).RemainQuota)
}

func TestRedisBatchReserveNeverFallsBackToStaleDatabaseBalance(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true

	user := createReserveTestUser(t, 10)
	reserved, err := TryReserveUserQuota(user.Id, 8)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, 10, getUserQuotaFromDB(t, user.Id), "batch delta is not flushed yet")

	reserved, err = TryReserveUserQuota(user.Id, 3)
	require.NoError(t, err)
	assert.False(t, reserved, "stale DB balance must not authorize a second spend")
	cachedUser, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 2, cachedUser.Quota)

	token := createReserveTestToken(t, 9)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 7, false)
	require.NoError(t, err)
	assert.True(t, reserved)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 3, false)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 9, getTokenFromDB(t, token.Id).RemainQuota)

	batchUpdate()
	assert.Equal(t, 2, getUserQuotaFromDB(t, user.Id))
	reloadedToken := getTokenFromDB(t, token.Id)
	assert.Equal(t, 2, reloadedToken.RemainQuota)
	assert.Equal(t, 7, reloadedToken.UsedQuota)
}

func TestBatchUpdateAccumulatesTwoMaximumRequestCharges(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	common.BatchUpdateEnabled = true

	user := createReserveTestUser(t, common.MaxQuota*2+100)
	require.NoError(t, DecreaseUserQuota(user.Id, common.MaxQuota, false))
	require.NoError(t, DecreaseUserQuota(user.Id, common.MaxQuota, false))

	batchUpdate()
	assert.Equal(t, 100, getUserQuotaFromDB(t, user.Id))
}

func TestBatchUpdateAccumulatorSaturatesOverflow(t *testing.T) {
	resetBatchUpdateTestState(t)

	addNewRecord(BatchUpdateTypeUserQuota, 1, math.MaxInt)
	addNewRecord(BatchUpdateTypeUserQuota, 1, 1)
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	assert.Equal(t, math.MaxInt, batchUpdateStores[BatchUpdateTypeUserQuota][1])
	batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()

	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	batchUpdateStores[BatchUpdateTypeUserQuota] = make(map[int]int)
	batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	addNewRecord(BatchUpdateTypeUserQuota, 1, math.MinInt)
	addNewRecord(BatchUpdateTypeUserQuota, 1, -1)
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	assert.Equal(t, math.MinInt, batchUpdateStores[BatchUpdateTypeUserQuota][1])
	batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
}

func TestReserveFallsBackToDatabaseWhenRedisIsUnavailable(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)

	user := createReserveTestUser(t, 20)
	require.NoError(t, populateUserCache(user))
	server.Close()

	// Redis 故障时降级为数据库条件更新：服务保持可用且不会超扣。
	reserved, err := TryReserveUserQuota(user.Id, 5)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, 15, getUserQuotaFromDB(t, user.Id))

	reserved, err = TryReserveUserQuota(user.Id, 16)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 15, getUserQuotaFromDB(t, user.Id))
}

func TestSynchronousReserveCompensatesCacheWhenPersistenceFails(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)

	user := createReserveTestUser(t, 10)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, DB.Delete(&user).Error)

	reserved, err := TryReserveUserQuota(user.Id, 6)
	assert.False(t, reserved)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	cached, cacheErr := cacheGetUserBase(user.Id)
	require.NoError(t, cacheErr)
	assert.Equal(t, 10, cached.Quota)

	token := createReserveTestToken(t, 12)
	_, err = GetTokenByKey(token.Key, true)
	require.NoError(t, err)
	require.NoError(t, DB.Delete(&token).Error)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 7, false)
	assert.False(t, reserved)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	cachedToken, cacheErr := cacheGetTokenByKey(token.Key)
	require.NoError(t, cacheErr)
	assert.Equal(t, 12, cachedToken.RemainQuota)
	assert.Zero(t, cachedToken.UsedQuota)
}

func TestTokenCacheInitPreservesLiveQuotaAndFenceBlocksStaleSnapshot(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)

	token := createReserveTestToken(t, 100)
	loaded, err := GetTokenByKey(token.Key, true)
	require.NoError(t, err)
	stale := *loaded

	result, err := cacheApplyTokenQuotaDelta(token.Id, token.Key, -70)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, result)

	// 已存在的哈希只刷新 TTL：数据库快照不得覆盖已被原子预扣的余额。
	code, err := cacheInitToken(stale)
	require.NoError(t, err)
	assert.Equal(t, 2, code)
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 30, cached.RemainQuota)

	// 变更期间：fence 删除缓存并拦截并发读者手中的过期快照。
	require.NoError(t, invalidateTokenCacheForMutation(token.Key))
	code, err = cacheInitToken(stale)
	require.NoError(t, err)
	assert.Zero(t, code, "the pre-mutation snapshot must not be published while fenced")
	_, err = cacheGetTokenByKey(token.Key)
	assert.Error(t, err)

	// fence 过期后可重新从数据库水合。
	server.FastForward(time.Duration(tokenCacheFenceSeconds+1) * time.Second)
	fresh, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	assert.Equal(t, 100, fresh.RemainQuota)
	cached, err = cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 100, cached.RemainQuota)
}

// TestFlushBatchUpdatesPersistsPendingDeltas 验证优雅关闭落盘语义：
// 内存增量必须一滴不剩进数据库，且落完后再次刷新是空操作。
func TestFlushBatchUpdatesPersistsPendingDeltas(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	common.BatchUpdateEnabled = true

	user := createReserveTestUser(t, 1000)
	token := createReserveTestToken(t, 1000)
	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -100)
	addNewRecord(BatchUpdateTypeTokenQuota, token.Id, -50)

	flushed := FlushBatchUpdates()
	assert.GreaterOrEqual(t, flushed, 2)
	assert.Equal(t, 900, getUserQuotaFromDB(t, user.Id))
	assert.Equal(t, 950, getTokenFromDB(t, token.Id).RemainQuota)
	assert.Zero(t, FlushBatchUpdates(), "second flush must be a no-op")
}

// TestFlushBatchUpdatesNoopWhenDisabled 验证开关关闭时刷新零开销、
// 不打日志不碰库（main.go 关机路径依赖此语义）。
func TestFlushBatchUpdatesNoopWhenDisabled(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	assert.Zero(t, FlushBatchUpdates())
}

func enableReconcileTestEnv(t *testing.T) {
	t.Helper()
	useUserCacheMiniRedis(t)
	oldBatchEnabled := common.BatchUpdateEnabled
	oldMaster := common.IsMasterNode
	common.BatchUpdateEnabled = true
	common.IsMasterNode = true
	t.Cleanup(func() {
		common.BatchUpdateEnabled = oldBatchEnabled
		common.IsMasterNode = oldMaster
	})
}

// TestReconcileQuotaCacheOnBootRecoversLostWindow 模拟崩溃：
// Redis 已扣、DB 未刷（绕开批量队列直接写 Redis，正如崩溃瞬间），
// 对账必须把差值补回数据库。
func TestReconcileQuotaCacheOnBootRecoversLostWindow(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openTopUpPaginationTestDB(t, dialect)
			require.NoError(t, db.AutoMigrate(&Token{}, &User{}))
			require.NoError(t, db.Exec("DELETE FROM tokens").Error)
			require.NoError(t, db.Exec("DELETE FROM users").Error)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })

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

			truncateTables(t)
			resetBatchUpdateTestState(t)
			enableReconcileTestEnv(t)

			user := createReserveTestUser(t, 1000)
			_, err = GetUserCache(user.Id)
			require.NoError(t, err)
			require.NoError(t, cacheDecrUserQuota(user.Id, 200))

			token := createReserveTestToken(t, 1000)
			_, err = GetTokenByKey(token.Key, true)
			require.NoError(t, err)
			_, err = cacheApplyTokenQuotaDelta(token.Id, token.Key, -300)
			require.NoError(t, err)

			// 崩溃前 DB 纹丝不动。
			assert.Equal(t, 1000, getUserQuotaFromDB(t, user.Id))
			assert.Equal(t, 1000, getTokenFromDB(t, token.Id).RemainQuota)

			stats, err := ReconcileQuotaCacheOnBoot()
			require.NoError(t, err)
			assert.Equal(t, 1, stats.UsersAdjusted)
			assert.Equal(t, 1, stats.TokensAdjusted)

			assert.Equal(t, 800, getUserQuotaFromDB(t, user.Id))
			reloaded := getTokenFromDB(t, token.Id)
			assert.Equal(t, 700, reloaded.RemainQuota)
			assert.Equal(t, 300, reloaded.UsedQuota)

			// 已对平后再次对账必须是空操作。
			stats, err = ReconcileQuotaCacheOnBoot()
			require.NoError(t, err)
			assert.Zero(t, stats.UsersAdjusted)
			assert.Zero(t, stats.TokensAdjusted)
		})
	}
}

// TestReconcileQuotaCacheOnBootAcceptsDbTrueRace 记录 db=true 直写授信的已知
// 残留竞态并锁定其语义：IncreaseUserQuota(..., db=true) 先同步落库、再异步刷
// 缓存；若崩溃恰落在两者之间（微秒级），Redis 落后数据库，对账仍以 Redis
// 为准减账，该笔授信在两侧一致地消失。损失以单笔授信为界，每次调整有审计
// 日志，可凭 TopUp/兑换记录重补（运维流程见 docs/production-checklist.md）。
func TestReconcileQuotaCacheOnBootAcceptsDbTrueRace(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openTopUpPaginationTestDB(t, dialect)
			require.NoError(t, db.AutoMigrate(&Token{}, &User{}))
			require.NoError(t, db.Exec("DELETE FROM tokens").Error)
			require.NoError(t, db.Exec("DELETE FROM users").Error)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })

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

			truncateTables(t)
			resetBatchUpdateTestState(t)
			enableReconcileTestEnv(t)

			user := createReserveTestUser(t, 1000)
			_, err = GetUserCache(user.Id)
			require.NoError(t, err)

			// 模拟 db=true 崩溃：DB 已 +200，异步缓存刷新没来得及跑。
			require.NoError(t, db.Exec("UPDATE users SET quota = quota + ? WHERE id = ?", 200, user.Id).Error)
			assert.Equal(t, 1200, getUserQuotaFromDB(t, user.Id))

			stats, err := ReconcileQuotaCacheOnBoot()
			require.NoError(t, err)
			assert.Equal(t, 1, stats.UsersAdjusted)
			// 对账信任 Redis：DB 被拉回 1000，+200 授信一致消失，需凭业务记录重补。
			assert.Equal(t, 1000, getUserQuotaFromDB(t, user.Id))

			// 已对平后再次对账必须是空操作。
			stats, err = ReconcileQuotaCacheOnBoot()
			require.NoError(t, err)
			assert.Zero(t, stats.UsersAdjusted)
		})
	}
}

// TestReconcileQuotaCacheOnBootSkipsFencedKeys fence 生效中的键正在变更，
// 跳过（下一轮定时刷会收敛），绝不能按过期快照回写。
func TestReconcileQuotaCacheOnBootSkipsFencedKeys(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	enableReconcileTestEnv(t)

	token := createReserveTestToken(t, 1000)
	_, err := cacheApplyTokenQuotaDelta(token.Id, token.Key, -100)
	require.NoError(t, err)
	// 手动立起 fence 但保留哈希，模拟“变更在途、快照仍在”：
	// invalidateTokenCacheForMutation 会连哈希一起删，不适用这里。
	_, err = GetTokenByKey(token.Key, true)
	require.NoError(t, err)
	require.NoError(t, common.RDB.Set(context.Background(), getTokenCacheFenceKey(token.Key), 1, time.Minute).Err())

	stats, err := ReconcileQuotaCacheOnBoot()
	require.NoError(t, err)
	assert.Equal(t, 1000, getTokenFromDB(t, token.Id).RemainQuota)
	assert.GreaterOrEqual(t, stats.SkippedFenced, 1)
}

// TestReconcileQuotaCacheOnBootGates 未启用批量、Redis 不可用、非 master
// 时必须是零开销的空操作，绝不碰数据库。
func TestReconcileQuotaCacheOnBootGates(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	oldMaster := common.IsMasterNode
	t.Cleanup(func() { common.IsMasterNode = oldMaster })

	// 批量关闭。
	stats, err := ReconcileQuotaCacheOnBoot()
	require.NoError(t, err)
	assert.Zero(t, stats)

	// 批量开但非 master（默认即非 master）。
	common.BatchUpdateEnabled = true
	stats, err = ReconcileQuotaCacheOnBoot()
	require.NoError(t, err)
	assert.Zero(t, stats)

	// master 但 Redis 不可用。
	common.IsMasterNode = true
	common.RedisEnabled = false
	stats, err = ReconcileQuotaCacheOnBoot()
	require.NoError(t, err)
	assert.Zero(t, stats)
}
