package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useTokenCacheMiniRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	previousEnabled, previousRDB := common.RedisEnabled, common.RDB
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	require.NoError(t, client.Ping(context.Background()).Err())
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		common.RedisEnabled = previousEnabled
		common.RDB = previousRDB
	})
	return server
}

// TestCacheGetTokenByKeyRespectsFence 覆盖 OWASP 要求：令牌撤销后，
// 任何旧快照都不得再被接受用于认证。即使 Redis 里仍有取消前的
// Status=Enabled hash，只要 fence 存在（DEL 失败或并发读者窗口），
// 缓存读取必须拒绝，让调用方回退数据库权威状态。
func TestCacheGetTokenByKeyRespectsFence(t *testing.T) {
	server := useTokenCacheMiniRedis(t)
	const key = "fenced-token-key"

	// 模拟「旧快照仍在缓存中」：管理员刚停用令牌，但 DEL hash 失败。
	stale := Token{Id: 42, Key: key, Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100}
	require.NoError(t, common.RDB.HSet(context.Background(), getTokenCacheKey(key), map[string]any{
		"Id": stale.Id, "Status": stale.Status, "RemainQuota": stale.RemainQuota,
	}).Err())

	// 没有 fence 时旧快照可读（这里验证测试夹具本身有效）。
	got, err := cacheGetTokenByKey(key)
	require.NoError(t, err)
	require.Equal(t, common.TokenStatusEnabled, got.Status)

	// 写库前升起 fence（DEL 失败的等价状态）。
	require.NoError(t, common.RDB.Set(context.Background(), getTokenCacheFenceKey(key), 1,
		time.Duration(tokenCacheFenceSeconds)*time.Second).Err())

	got, err = cacheGetTokenByKey(key)
	require.Error(t, err, "fence 存在时必须拒绝缓存快照")
	require.Nil(t, got)

	// fence 过期后恢复可读，验证不是永久拒绝。
	server.FastForward(tokenCacheFenceSeconds*time.Second + time.Second)
	got, err = cacheGetTokenByKey(key)
	require.NoError(t, err)
	require.Equal(t, common.TokenStatusEnabled, got.Status)
}

// TestCacheGetTokenByKeyFailsClosedOnRedisError 验证 Redis 故障时
// 缓存读取返回 error 而不是放行（调用方会回退数据库）。
func TestCacheGetTokenByKeyFailsClosedOnRedisError(t *testing.T) {
	useTokenCacheMiniRedis(t)
	// 把 client 指向一个不存在的地址，制造 EXISTS 失败。
	previous := common.RDB
	common.RDB = redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB = previous
	})

	got, err := cacheGetTokenByKey("any-key")
	require.Error(t, err)
	require.Nil(t, got)
}

// TestInvalidateTokenCacheForMutationReportsFailure 验证缓存失效失败时
// 返回 error，供写路径决定是否中止（fail-closed）。
func TestInvalidateTokenCacheForMutationReportsFailure(t *testing.T) {
	useTokenCacheMiniRedis(t)
	previous := common.RDB
	common.RDB = redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB = previous
	})

	require.Error(t, invalidateTokenCacheForMutation("some-token"))
}

// TestTokenMutationFailsClosedWhenCacheInvalidationFails 验证 OWASP 的
// fail-closed 语义：缓存失效失败时不得继续写库。否则令牌在库里已被停用/删除，
// 但 Redis 旧快照仍可通过鉴权，形成「撤销已生效却仍可用」的窗口。
func TestTokenMutationFailsClosedWhenCacheInvalidationFails(t *testing.T) {
	useTokenCacheMiniRedis(t)
	// 指向不可达 Redis，使 fence SET 失败。
	previous := common.RDB
	common.RDB = redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB = previous
	})

	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Token{}))
	DB = db
	t.Cleanup(func() { DB = previousDB })

	token := Token{
		UserId: 1, Key: "fail-closed-key", Name: "original",
		Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100,
	}
	require.NoError(t, db.Create(&token).Error)

	token.Name = "should-not-be-written"
	err = token.Update()
	require.Error(t, err, "缓存失效失败必须中止令牌更新")

	var stored Token
	require.NoError(t, db.First(&stored, token.Id).Error)
	require.Equal(t, "original", stored.Name, "失败后数据库必须保持原状态")
}
