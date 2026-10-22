package license

import (
	"context"
	"sync"
	"time"
)

type Result int

const (
	Unknown Result = iota
	Owned
	NotOwned
)

const (
	ownerLifetime    = 7 * 24 * time.Hour
	nonOwnerLifetime = time.Minute
	maxCached        = 100_000
)

type cached struct {
	result    Result
	expiresAt time.Time
}

type pendingCheck struct {
	done   chan struct{}
	result Result
	// The fetching request went away without an answer, so the waiters fetch for themselves.
	abandoned bool
}

type Cache[K comparable] struct {
	mu      sync.Mutex
	entries map[K]cached
	pending map[K]*pendingCheck
	limit   int
	now     func() time.Time
}

func NewCache[K comparable](now func() time.Time) *Cache[K] {
	return &Cache[K]{entries: map[K]cached{}, pending: map[K]*pendingCheck{}, limit: maxCached, now: now}
}

// Unknown results are never cached. Overlapping checks of one key share one fetch, so a burst of
// logins for one account costs one round of upstream calls.
func (c *Cache[K]) Check(ctx context.Context, key K, fetch func() Result) Result {
	for {
		result, pending, fetching := c.join(key)
		switch {
		case pending == nil:
			return result
		case fetching:
			return c.fetchFor(ctx, key, pending, fetch)
		}
		select {
		case <-pending.done:
		case <-ctx.Done():
			return Unknown
		}
		if !pending.abandoned {
			return pending.result
		}
	}
}

func (c *Cache[K]) join(key K) (Result, *pendingCheck, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if result, ok := c.lookupLocked(key); ok {
		return result, nil, false
	}
	if pending, ok := c.pending[key]; ok {
		return Unknown, pending, false
	}
	pending := &pendingCheck{done: make(chan struct{})}
	c.pending[key] = pending
	return Unknown, pending, true
}

// The deferred settle also runs when fetch panics, so the waiters are never left hanging.
func (c *Cache[K]) fetchFor(ctx context.Context, key K, pending *pendingCheck, fetch func() Result) (result Result) {
	result = Unknown
	defer func() { c.settle(key, pending, result, ctx.Err() != nil) }()
	return fetch()
}

func (c *Cache[K]) settle(key K, pending *pendingCheck, result Result, canceled bool) {
	c.mu.Lock()
	if result != Unknown {
		c.storeLocked(key, result)
	}
	delete(c.pending, key)
	pending.result = result
	pending.abandoned = canceled && result == Unknown
	c.mu.Unlock()
	close(pending.done)
}

func (c *Cache[K]) lookupLocked(key K) (Result, bool) {
	entry, ok := c.entries[key]
	if !ok {
		return Unknown, false
	}
	if !c.now().Before(entry.expiresAt) {
		delete(c.entries, key)
		return Unknown, false
	}
	return entry.result, true
}

func (c *Cache[K]) storeLocked(key K, result Result) {
	lifetime := nonOwnerLifetime
	if result == Owned {
		lifetime = ownerLifetime
	}
	now := c.now()
	if _, replacing := c.entries[key]; !replacing && len(c.entries) >= c.limit {
		c.makeRoom(now)
	}
	c.entries[key] = cached{result: result, expiresAt: now.Add(lifetime)}
}

// makeRoom drops expired entries, then the entry closest to expiring if the cache is still full.
func (c *Cache[K]) makeRoom(now time.Time) {
	var soonestKey K
	var soonest time.Time
	for key, entry := range c.entries {
		if !now.Before(entry.expiresAt) {
			delete(c.entries, key)
			continue
		}
		if soonest.IsZero() || entry.expiresAt.Before(soonest) {
			soonestKey, soonest = key, entry.expiresAt
		}
	}
	if len(c.entries) >= c.limit {
		delete(c.entries, soonestKey)
	}
}
