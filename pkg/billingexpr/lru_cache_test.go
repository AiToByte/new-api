package billingexpr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// White-box test for the compile-cache eviction policy. The public compile
// path recompiles transparently on a miss, so eviction order is only
// observable through the cache itself (internal package test).
func TestExprCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := newExprCache(3)
	a, b, d := &cachedEntry{version: 1}, &cachedEntry{version: 2}, &cachedEntry{version: 4}
	c.add("a", a)
	c.add("b", b)
	c.add("c", &cachedEntry{version: 3})
	assert.Equal(t, 3, c.size())

	// Touch "a" so "b" becomes least-recently-used.
	got, ok := c.get("a")
	require.True(t, ok)
	assert.Same(t, a, got)

	c.add("d", d)
	assert.Equal(t, 3, c.size())
	_, ok = c.get("b")
	assert.False(t, ok, "least-recently-used entry must be evicted first")
	for _, key := range []string{"a", "c", "d"} {
		_, ok = c.get(key)
		assert.True(t, ok, "recently used entry %q must survive", key)
	}

	// Re-adding an existing key refreshes value and recency without growing.
	a2 := &cachedEntry{version: 11}
	c.add("a", a2)
	assert.Equal(t, 3, c.size())
	got, ok = c.get("a")
	require.True(t, ok)
	assert.Same(t, a2, got)

	c.clear()
	assert.Equal(t, 0, c.size())
	for _, key := range []string{"a", "c", "d"} {
		_, ok = c.get(key)
		assert.False(t, ok)
	}
}

func TestExprCacheNonPositiveCapacityHoldsOne(t *testing.T) {
	c := newExprCache(0)
	c.add("x", &cachedEntry{})
	c.add("y", &cachedEntry{})
	assert.Equal(t, 1, c.size())
	_, ok := c.get("x")
	assert.False(t, ok)
	_, ok = c.get("y")
	assert.True(t, ok)
}
