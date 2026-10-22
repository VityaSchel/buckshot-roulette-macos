package license

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestOverlappingChecksShareOneFetch(t *testing.T) {
	cache := NewCache[string](time.Now)
	release := make(chan struct{})
	var fetches atomic.Int32
	fetch := func() Result {
		fetches.Add(1)
		<-release
		return Owned
	}
	results := make(chan Result)
	for range 5 {
		go func() { results <- cache.Check(context.Background(), "owner", fetch) }()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	for range 5 {
		if result := <-results; result != Owned {
			t.Fatalf("result %v", result)
		}
	}
	if fetches.Load() != 1 {
		t.Fatalf("%d fetches for overlapping checks, want 1", fetches.Load())
	}
}

func TestWaiterFetchesWhenTheFetchingRequestLeaves(t *testing.T) {
	cache := NewCache[string](time.Now)
	leaving, leave := context.WithCancel(context.Background())
	started := make(chan struct{})
	leaderResult := make(chan Result)
	go func() {
		leaderResult <- cache.Check(leaving, "owner", func() Result {
			close(started)
			<-leaving.Done()
			return Unknown
		})
	}()
	<-started
	waiterResult := make(chan Result)
	go func() {
		waiterResult <- cache.Check(context.Background(), "owner", func() Result { return Owned })
	}()
	time.Sleep(50 * time.Millisecond)
	leave()
	if result := <-leaderResult; result != Unknown {
		t.Fatalf("the request that left got %v", result)
	}
	if result := <-waiterResult; result != Owned {
		t.Fatalf("the waiting request got %v instead of its own answer", result)
	}
}

func TestWaiterThatLeavesGetsUnknown(t *testing.T) {
	cache := NewCache[string](time.Now)
	started, release := make(chan struct{}), make(chan struct{})
	go cache.Check(context.Background(), "owner", func() Result {
		close(started)
		<-release
		return Owned
	})
	<-started
	leaving, leave := context.WithCancel(context.Background())
	leave()
	if result := cache.Check(leaving, "owner", func() Result { return Owned }); result != Unknown {
		t.Fatalf("a waiter whose request left got %v", result)
	}
	close(release)
}

func TestPanickingFetchReleasesTheKey(t *testing.T) {
	cache := NewCache[string](time.Now)
	func() {
		defer func() { recover() }()
		cache.Check(context.Background(), "owner", func() Result { panic("upstream parser bug") })
	}()
	done := make(chan Result)
	go func() {
		done <- cache.Check(context.Background(), "owner", func() Result { return Owned })
	}()
	select {
	case result := <-done:
		if result != Owned {
			t.Fatalf("result %v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the key stayed pending after the fetch panicked")
	}
}

type testClock struct{ now time.Time }

func (c *testClock) read() time.Time            { return c.now }
func (c *testClock) advance(step time.Duration) { c.now = c.now.Add(step) }

func countingFetch(result Result, calls *int) func() Result {
	return func() Result {
		*calls++
		return result
	}
}

func TestCacheLifetimes(t *testing.T) {
	clock := &testClock{now: time.UnixMilli(testNowMs)}
	cache := NewCache[string](clock.read)
	calls := 0

	cache.Check(context.Background(), "owner", countingFetch(Owned, &calls))
	clock.advance(ownerLifetime - time.Millisecond)
	if got := cache.Check(context.Background(), "owner", countingFetch(NotOwned, &calls)); got != Owned || calls != 1 {
		t.Fatalf("owner within 7 days: %v after %d fetches", got, calls)
	}
	clock.advance(time.Millisecond)
	if got := cache.Check(context.Background(), "owner", countingFetch(NotOwned, &calls)); got != NotOwned || calls != 2 {
		t.Fatalf("owner after 7 days: %v after %d fetches", got, calls)
	}

	calls = 0
	cache.Check(context.Background(), "visitor", countingFetch(NotOwned, &calls))
	clock.advance(nonOwnerLifetime - time.Millisecond)
	cache.Check(context.Background(), "visitor", countingFetch(Owned, &calls))
	if calls != 1 {
		t.Fatalf("non-owner fetched %d times within a minute", calls)
	}
	clock.advance(time.Millisecond)
	if got := cache.Check(context.Background(), "visitor", countingFetch(Owned, &calls)); got != Owned || calls != 2 {
		t.Fatalf("non-owner after a minute: %v after %d fetches", got, calls)
	}

	calls = 0
	cache.Check(context.Background(), "flaky", countingFetch(Unknown, &calls))
	cache.Check(context.Background(), "flaky", countingFetch(Unknown, &calls))
	if calls != 2 {
		t.Fatalf("unknown results must not be cached, fetched %d times", calls)
	}
}

func TestCacheCap(t *testing.T) {
	clock := &testClock{now: time.UnixMilli(testNowMs)}
	cache := NewCache[int64](clock.read)
	cache.limit = 3
	check := func(key int64, result Result) {
		cache.Check(context.Background(), key, func() Result { return result })
	}
	cached := func(key int64) (Result, bool) {
		fetched := false
		result := cache.Check(context.Background(), key, func() Result {
			fetched = true
			return Unknown
		})
		return result, !fetched
	}
	expect := func(step string, present []int64, absent []int64) {
		t.Helper()
		if len(cache.entries) != cache.limit {
			t.Fatalf("%s: %d entries, want %d", step, len(cache.entries), cache.limit)
		}
		for _, key := range present {
			if _, ok := cached(key); !ok {
				t.Fatalf("%s: entry %d lost", step, key)
			}
		}
		for _, key := range absent {
			if _, ok := cached(key); ok {
				t.Fatalf("%s: entry %d kept", step, key)
			}
		}
	}

	check(1, NotOwned)
	check(2, Owned)
	check(3, Owned)
	clock.advance(nonOwnerLifetime)
	check(4, NotOwned)
	expect("the expired entry is pruned first", []int64{2, 3, 4}, []int64{1})

	check(5, Owned)
	expect("the entry closest to expiring makes room", []int64{2, 3, 5}, []int64{4})
	if result, _ := cached(5); result != Owned {
		t.Fatalf("entry 5 is %v", result)
	}

	clock.advance(ownerLifetime)
	check(2, NotOwned)
	if result, ok := cached(2); !ok || result != NotOwned || len(cache.entries) != 3 {
		t.Fatalf("a refetched entry: %v, %v with %d entries", result, ok, len(cache.entries))
	}
}
