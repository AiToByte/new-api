package model

import (
	"sync/atomic"
)

// ReadCacheScope 标识一次请求内值得观察的读路径：
// token（鉴权）、user（鉴权/归属）、channel（渠道选择）。
// 定价与分组倍率常驻内存映射，无 DB 回退概念，不在统计之列。
type ReadCacheScope string

const (
	ReadCacheScopeToken   ReadCacheScope = "token"
	ReadCacheScopeUser    ReadCacheScope = "user"
	ReadCacheScopeChannel ReadCacheScope = "channel"
)

// ReadCacheStat 是单个读路径的命中计数快照。
// 约定：hit = 未触碰数据库即返回；miss = 未命中（含回退数据库与映射缺失
// 直接失败）。缓存总开关关闭时，所有读取都记为 miss，如实反映无缓存代价。
type ReadCacheStat struct {
	Hits   uint64 `json:"hits"`
	Misses uint64 `json:"misses"`
}

type readCacheCounter struct {
	hits   atomic.Uint64
	misses atomic.Uint64
}

var readCacheCounters = map[ReadCacheScope]*readCacheCounter{
	ReadCacheScopeToken:   {},
	ReadCacheScopeUser:    {},
	ReadCacheScopeChannel: {},
}

// NoteReadCacheHit 记录一次缓存命中。调用方保证只在真正省掉 DB
// 读取时调用；fence 拦截等两边都没走到的路径不计数。
func NoteReadCacheHit(scope ReadCacheScope) {
	if counter, ok := readCacheCounters[scope]; ok {
		counter.hits.Add(1)
	}
}

// NoteReadCacheMiss 记录一次数据库回退。
func NoteReadCacheMiss(scope ReadCacheScope) {
	if counter, ok := readCacheCounters[scope]; ok {
		counter.misses.Add(1)
	}
}

// ReadCacheStatsSnapshot 返回各读路径累计命中快照（进程级，不重置）。
func ReadCacheStatsSnapshot() map[ReadCacheScope]ReadCacheStat {
	snapshot := make(map[ReadCacheScope]ReadCacheStat, len(readCacheCounters))
	for scope, counter := range readCacheCounters {
		snapshot[scope] = ReadCacheStat{
			Hits:   counter.hits.Load(),
			Misses: counter.misses.Load(),
		}
	}
	return snapshot
}
