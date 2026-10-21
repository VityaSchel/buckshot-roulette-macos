package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestOverlappingChecksShareOneFetch(t *testing.T) {
	cache := newLicenseCache[string](10, time.Now)
	release := make(chan struct{})
	var fetches atomic.Int32
	fetch := func() licenseResult {
		fetches.Add(1)
		<-release
		return licenseOwned
	}
	results := make(chan licenseResult)
	for range 5 {
		go func() { results <- cache.check(context.Background(), "owner", fetch) }()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	for range 5 {
		if result := <-results; result != licenseOwned {
			t.Fatalf("result %v", result)
		}
	}
	if fetches.Load() != 1 {
		t.Fatalf("%d fetches for overlapping checks, want 1", fetches.Load())
	}
}

func TestWaiterFetchesWhenTheFetchingRequestLeaves(t *testing.T) {
	cache := newLicenseCache[string](10, time.Now)
	leaving, leave := context.WithCancel(context.Background())
	started := make(chan struct{})
	leaderResult := make(chan licenseResult)
	go func() {
		leaderResult <- cache.check(leaving, "owner", func() licenseResult {
			close(started)
			<-leaving.Done()
			return licenseUnknown
		})
	}()
	<-started
	waiterResult := make(chan licenseResult)
	go func() {
		waiterResult <- cache.check(context.Background(), "owner", func() licenseResult { return licenseOwned })
	}()
	time.Sleep(50 * time.Millisecond)
	leave()
	if result := <-leaderResult; result != licenseUnknown {
		t.Fatalf("the request that left got %v", result)
	}
	if result := <-waiterResult; result != licenseOwned {
		t.Fatalf("the waiting request got %v instead of its own answer", result)
	}
}

func TestPanickingFetchReleasesTheKey(t *testing.T) {
	cache := newLicenseCache[string](10, time.Now)
	func() {
		defer func() { recover() }()
		cache.check(context.Background(), "owner", func() licenseResult { panic("upstream parser bug") })
	}()
	done := make(chan licenseResult)
	go func() {
		done <- cache.check(context.Background(), "owner", func() licenseResult { return licenseOwned })
	}()
	select {
	case result := <-done:
		if result != licenseOwned {
			t.Fatalf("result %v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the key stayed pending after the fetch panicked")
	}
}

type testClock struct{ now time.Time }

func (c *testClock) read() time.Time            { return c.now }
func (c *testClock) advance(step time.Duration) { c.now = c.now.Add(step) }

func countingFetch(result licenseResult, calls *int) func() licenseResult {
	return func() licenseResult {
		*calls++
		return result
	}
}

func TestLicenseCacheLifetimes(t *testing.T) {
	clock := &testClock{now: time.UnixMilli(testNowMs)}
	cache := newLicenseCache[string](10, clock.read)
	calls := 0

	cache.check(context.Background(), "owner", countingFetch(licenseOwned, &calls))
	clock.advance(ownerCacheLifetime - time.Millisecond)
	if got := cache.check(context.Background(), "owner", countingFetch(licenseNotOwned, &calls)); got != licenseOwned || calls != 1 {
		t.Fatalf("owner within 7 days: %v after %d fetches", got, calls)
	}
	clock.advance(time.Millisecond)
	if got := cache.check(context.Background(), "owner", countingFetch(licenseNotOwned, &calls)); got != licenseNotOwned || calls != 2 {
		t.Fatalf("owner after 7 days: %v after %d fetches", got, calls)
	}

	calls = 0
	cache.check(context.Background(), "visitor", countingFetch(licenseNotOwned, &calls))
	clock.advance(nonOwnerCacheLifetime - time.Millisecond)
	cache.check(context.Background(), "visitor", countingFetch(licenseOwned, &calls))
	if calls != 1 {
		t.Fatalf("non-owner fetched %d times within a minute", calls)
	}
	clock.advance(time.Millisecond)
	if got := cache.check(context.Background(), "visitor", countingFetch(licenseOwned, &calls)); got != licenseOwned || calls != 2 {
		t.Fatalf("non-owner after a minute: %v after %d fetches", got, calls)
	}

	calls = 0
	cache.check(context.Background(), "flaky", countingFetch(licenseUnknown, &calls))
	cache.check(context.Background(), "flaky", countingFetch(licenseUnknown, &calls))
	if calls != 2 {
		t.Fatalf("unknown results must not be cached, fetched %d times", calls)
	}
}

func TestLicenseCacheCap(t *testing.T) {
	clock := &testClock{now: time.UnixMilli(testNowMs)}
	cache := newLicenseCache[int64](3, clock.read)
	cache.store(1, true)
	cache.store(2, false)
	cache.store(3, true)
	cache.store(4, true)
	if cache.len() != 3 {
		t.Fatalf("%d entries, want 3", cache.len())
	}
	if _, ok := cache.lookup(2); ok {
		t.Fatal("the entry closest to expiring must make room")
	}
	for _, key := range []int64{1, 3, 4} {
		if result, ok := cache.lookup(key); !ok || result != licenseOwned {
			t.Fatalf("entry %d lost", key)
		}
	}

	cache.store(3, false)
	if cache.len() != 3 {
		t.Fatalf("replacing an entry changed the size to %d", cache.len())
	}
	clock.advance(nonOwnerCacheLifetime)
	cache.store(5, false)
	cache.store(6, false)
	if cache.len() != 3 {
		t.Fatalf("%d entries, want 3", cache.len())
	}
	if _, ok := cache.lookup(3); ok {
		t.Fatal("the expired entry must be pruned first")
	}
}
