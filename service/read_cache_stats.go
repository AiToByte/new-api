package service

import (
	"github.com/QuantumNous/new-api/model"
)

// ReadCacheStats exposes per-request read-path hit counters for operators.
// See model.NoteReadCacheHit for counting semantics.
func ReadCacheStats() map[model.ReadCacheScope]model.ReadCacheStat {
	return model.ReadCacheStatsSnapshot()
}
