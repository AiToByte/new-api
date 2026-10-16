package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestReadCacheStatsTracksScopes 分别断言三个读路径的命中计数独立累加。
// 只用增量断言，因为计数器是进程级累计，其他测试也会经过这些读路径。
func TestReadCacheStatsTracksScopes(t *testing.T) {
	before := ReadCacheStatsSnapshot()

	NoteReadCacheHit(ReadCacheScopeToken)
	NoteReadCacheHit(ReadCacheScopeToken)
	NoteReadCacheMiss(ReadCacheScopeToken)
	NoteReadCacheHit(ReadCacheScopeUser)
	NoteReadCacheMiss(ReadCacheScopeChannel)
	NoteReadCacheMiss(ReadCacheScopeChannel)
	NoteReadCacheMiss(ReadCacheScopeChannel)

	after := ReadCacheStatsSnapshot()
	assert.Equal(t, uint64(2), after[ReadCacheScopeToken].Hits-before[ReadCacheScopeToken].Hits)
	assert.Equal(t, uint64(1), after[ReadCacheScopeToken].Misses-before[ReadCacheScopeToken].Misses)
	assert.Equal(t, uint64(1), after[ReadCacheScopeUser].Hits-before[ReadCacheScopeUser].Hits)
	assert.Equal(t, uint64(0), after[ReadCacheScopeUser].Misses-before[ReadCacheScopeUser].Misses)
	assert.Equal(t, uint64(0), after[ReadCacheScopeChannel].Hits-before[ReadCacheScopeChannel].Hits)
	assert.Equal(t, uint64(3), after[ReadCacheScopeChannel].Misses-before[ReadCacheScopeChannel].Misses)
}

// TestReadCacheStatsSnapshotShape 锁定快照结构：三个 scope 齐备，
// 新增读路径漏登记时在这里第一时间暴露。
func TestReadCacheStatsSnapshotShape(t *testing.T) {
	snapshot := ReadCacheStatsSnapshot()
	assert.Len(t, snapshot, 3)
	for _, scope := range []ReadCacheScope{
		ReadCacheScopeToken,
		ReadCacheScopeUser,
		ReadCacheScopeChannel,
	} {
		_, ok := snapshot[scope]
		assert.True(t, ok, "missing scope %q in snapshot", scope)
	}
}
