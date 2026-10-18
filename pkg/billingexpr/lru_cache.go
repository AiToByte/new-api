package billingexpr

import (
	"container/list"
	"sync"
)

// exprCache is a mutex-guarded LRU cache for compiled expressions, keyed by
// expression hash. It replaces the previous plain map whose overflow handling
// dropped the entire cache, turning one overflow into a recompile storm for
// every tiered model. Eviction now removes only the least-recently-used entry.
//
// All operations are O(1) map/list steps held under a single mutex; lookups
// sit on the relay hot path (pre-consume + settlement per request) but each
// critical section is microseconds, so read/write splitting would add
// complexity without measurable gain.
type exprCache struct {
	mu      sync.Mutex
	ll      *list.List
	items   map[string]*list.Element
	maxSize int
}

type exprCacheItem struct {
	key   string
	entry *cachedEntry
}

func newExprCache(maxSize int) *exprCache {
	if maxSize <= 0 {
		maxSize = 1
	}
	return &exprCache{
		ll:      list.New(),
		items:   make(map[string]*list.Element, 64),
		maxSize: maxSize,
	}
}

// get returns the cached entry and promotes it to most-recently-used.
// The boolean reports whether the key was present.
func (c *exprCache) get(key string) (*cachedEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*exprCacheItem).entry, true
	}
	return nil, false
}

// add stores entry under key, evicting least-recently-used entries while over
// capacity. Re-adding an existing key refreshes its value and recency.
func (c *exprCache) add(key string, entry *cachedEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*exprCacheItem).entry = entry
		c.ll.MoveToFront(el)
		return
	}
	c.items[key] = c.ll.PushFront(&exprCacheItem{key: key, entry: entry})
	for c.ll.Len() > c.maxSize {
		tail := c.ll.Back()
		if tail == nil {
			break
		}
		delete(c.items, tail.Value.(*exprCacheItem).key)
		c.ll.Remove(tail)
	}
}

// clear drops every entry. Called when billing rules are updated.
func (c *exprCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll.Init()
	c.items = make(map[string]*list.Element, 64)
}

// size reports the entry count. Test-only introspection; callers must not
// branch billing logic on it.
func (c *exprCache) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}
