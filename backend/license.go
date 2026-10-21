package main

import (
	"context"
	"net/http"
	"sync"
	"time"
)

const (
	ownerCacheLifetime    = 7 * 24 * time.Hour
	nonOwnerCacheLifetime = time.Minute
	maxCachedLicenses     = 100_000
)

type licenseResult int

const (
	licenseUnknown licenseResult = iota
	licenseOwned
	licenseNotOwned
)

type cachedLicense struct {
	owned     bool
	expiresAt time.Time
}

type pendingCheck struct {
	done   chan struct{}
	result licenseResult
	// The fetching request went away without an answer, so the waiters fetch for themselves.
	abandoned bool
}

type licenseCache[K comparable] struct {
	mu      sync.Mutex
	entries map[K]cachedLicense
	pending map[K]*pendingCheck
	limit   int
	now     func() time.Time
}

func newLicenseCache[K comparable](limit int, now func() time.Time) *licenseCache[K] {
	return &licenseCache[K]{entries: map[K]cachedLicense{}, pending: map[K]*pendingCheck{}, limit: limit, now: now}
}

// Unknown results are never cached. Overlapping checks of one key share one fetch, so a burst of
// logins for one account costs one round of upstream calls.
func (c *licenseCache[K]) check(ctx context.Context, key K, fetch func() licenseResult) licenseResult {
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
			return licenseUnknown
		}
		if !pending.abandoned {
			return pending.result
		}
	}
}

func (c *licenseCache[K]) join(key K) (licenseResult, *pendingCheck, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if result, ok := c.lookupLocked(key); ok {
		return result, nil, false
	}
	if pending, ok := c.pending[key]; ok {
		return licenseUnknown, pending, false
	}
	pending := &pendingCheck{done: make(chan struct{})}
	c.pending[key] = pending
	return licenseUnknown, pending, true
}

// The deferred settle also runs when fetch panics, so the waiters are never left hanging.
func (c *licenseCache[K]) fetchFor(ctx context.Context, key K, pending *pendingCheck, fetch func() licenseResult) (result licenseResult) {
	result = licenseUnknown
	defer func() { c.settle(key, pending, result, ctx.Err() != nil) }()
	return fetch()
}

func (c *licenseCache[K]) settle(key K, pending *pendingCheck, result licenseResult, canceled bool) {
	c.mu.Lock()
	if result != licenseUnknown {
		c.storeLocked(key, result == licenseOwned)
	}
	delete(c.pending, key)
	pending.result = result
	pending.abandoned = canceled && result == licenseUnknown
	c.mu.Unlock()
	close(pending.done)
}

func (c *licenseCache[K]) lookup(key K) (licenseResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lookupLocked(key)
}

func (c *licenseCache[K]) lookupLocked(key K) (licenseResult, bool) {
	entry, ok := c.entries[key]
	if !ok {
		return licenseUnknown, false
	}
	if !c.now().Before(entry.expiresAt) {
		delete(c.entries, key)
		return licenseUnknown, false
	}
	if entry.owned {
		return licenseOwned, true
	}
	return licenseNotOwned, true
}

func (c *licenseCache[K]) store(key K, owned bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.storeLocked(key, owned)
}

func (c *licenseCache[K]) storeLocked(key K, owned bool) {
	lifetime := nonOwnerCacheLifetime
	if owned {
		lifetime = ownerCacheLifetime
	}
	now := c.now()
	if _, replacing := c.entries[key]; !replacing && len(c.entries) >= c.limit {
		c.makeRoom(now)
	}
	c.entries[key] = cachedLicense{owned: owned, expiresAt: now.Add(lifetime)}
}

// makeRoom drops expired entries, then the entry closest to expiring if the cache is still full.
func (c *licenseCache[K]) makeRoom(now time.Time) {
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

func (c *licenseCache[K]) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// finishLicenseCheck is Elysia's checkLicense macro: every result becomes a redirect to the frontend.
func (a *app) finishLicenseCheck(w http.ResponseWriter, service string, result licenseResult) {
	switch result {
	case licenseOwned:
		w.Header().Add("Set-Cookie", issueLicenseCookie(a.now(), a.cfg.jwtSecret))
		redirect(w, a.cfg.frontendURL("success"))
	case licenseNotOwned:
		redirect(w, a.cfg.frontendURL("error-"+service+"-no-license"))
	default:
		redirect(w, a.cfg.frontendURL("error-"+service+"-license-unknown"))
	}
}
