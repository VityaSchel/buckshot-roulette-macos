package itchio

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/config"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/license"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/upstream/upstreamtest"
)

func newTestClient(respond upstreamtest.Respond) (*Client, *upstreamtest.Transport) {
	cfg := config.Config{
		ItchCallbackURL: "https://buckshot-roulette-macos.hloth.dev/api/auth/itch.io/callback",
		Origins:         config.Origins{ItchAPI: "http://itch.test"},
	}
	httpClient, transport := upstreamtest.NewClient(respond)
	return New(cfg, httpClient, time.Now), transport
}

// itchUpstream serves a profile for user 1001 and owned keys that hold the game on ownedPage (0: never).
func itchUpstream(ownedPage, lastPage int) upstreamtest.Respond {
	return func(call upstreamtest.Call) (int, string, error) {
		if call.Path == "/profile" {
			return http.StatusOK, `{"user":{"id":1001}}`, nil
		}
		var page int
		fmt.Sscanf(call.RawQuery, "page=%d", &page)
		switch {
		case page == ownedPage:
			return http.StatusOK, `{"owned_keys":[{"game_id":1},{"game_id":2443623}]}`, nil
		case lastPage == 0 || page <= lastPage:
			return http.StatusOK, fmt.Sprintf(`{"owned_keys":[{"game_id":%d}]}`, 1000+page), nil
		}
		return http.StatusOK, `{"owned_keys":[]}`, nil
	}
}

func TestBearerAuthorization(t *testing.T) {
	cases := []struct {
		token string
		want  string
		ok    bool
	}{
		{"abc", "Bearer abc", true},
		{"nonowner.1108 a+b c", "Bearer nonowner.1108 a+b c", true},
		{"nonowner.1110é", "Bearer nonowner.1110\xe9", true},
		{"trailing \t", "Bearer trailing", true},
		{" ", "Bearer", true},
		{"line\nbreak", "", false},
		{"carriage\rreturn", "", false},
		{"nul\x00", "", false},
		{"euro€", "", false},
		{"emoji😀", "", false},
		{"invalid\xff", "", false},
	}
	for _, c := range cases {
		got, ok := bearerAuthorization(c.token)
		if got != c.want || ok != c.ok {
			t.Errorf("bearerAuthorization(%q) = %q, %v; want %q, %v", c.token, got, ok, c.want, c.ok)
		}
	}
}

func TestProfileUserID(t *testing.T) {
	cases := []struct {
		body string
		id   int64
		ok   bool
	}{
		{`{"user":{"id":1001,"username":"parity"}}`, 1001, true},
		{`{"user":{"id":5.0}}`, 5, true},
		{`{"user":{"id":-0}}`, 0, true},
		{`{"user":{"id":9007199254740991}}`, 9007199254740991, true},
		{`{"user":{"id":5},"user":{"id":7}}`, 7, true},
		{`{"user":{"id":9007199254740992}}`, 0, false},
		{`{"user":{"id":1e300}}`, 0, false},
		{`{"user":{"id":1.5}}`, 0, false},
		{`{"user":{"id":-5}}`, 0, false},
		{`{"user":{"id":"1017"}}`, 0, false},
		{`{"user":[]}`, 0, false},
		{`[{"user":{"id":5}}]`, 0, false},
		{`{"errors":["invalid key"]}`, 0, false},
		{`<html></html>`, 0, false},
		{"\xEF\xBB\xBF" + `{"user":{"id":1001}}`, 1001, true},
	}
	for _, c := range cases {
		id, ok := profileUserID([]byte(c.body))
		if id != c.id || ok != c.ok {
			t.Errorf("profileUserID(%s) = %d, %v; want %d, %v", c.body, id, ok, c.id, c.ok)
		}
	}
}

func TestOwnedGameIDs(t *testing.T) {
	cases := []struct {
		body string
		ids  []int64
		ok   bool
	}{
		{`{"page":1,"owned_keys":[{"id":1,"game_id":1},{"id":2,"game_id":2443623}]}`, []int64{1, 2443623}, true},
		{`{"owned_keys":[]}`, []int64{}, true},
		{`{"owned_keys":{}}`, nil, true},
		{`{"owned_keys":{"0":{"game_id":2443623}}}`, nil, true},
		{`{"owned_keys":[{"id":5}]}`, nil, false},
		{`{"owned_keys":[{"game_id":"2443623"}]}`, nil, false},
		{`{"owned_keys":[{"game_id":-1}]}`, nil, false},
		{`{"owned_keys":[null]}`, nil, false},
		{`{"owned_keys":[[1]]}`, nil, false},
		{`{"owned_keys":null}`, nil, false},
		{`{"errors":["invalid key"]}`, nil, false},
		{`<html></html>`, nil, false},
		{"\xEF\xBB\xBF" + `{"owned_keys":[{"game_id":2443623}]}`, []int64{2443623}, true},
	}
	for _, c := range cases {
		ids, ok := ownedGameIDs([]byte(c.body))
		if !slices.Equal(ids, c.ids) || ok != c.ok {
			t.Errorf("ownedGameIDs(%s) = %v, %v; want %v, %v", c.body, ids, ok, c.ids, c.ok)
		}
	}
}

func TestLicense(t *testing.T) {
	failing := func(path string) upstreamtest.Respond {
		return func(call upstreamtest.Call) (int, string, error) {
			if call.Path == path {
				return http.StatusUnauthorized, `{"errors":["invalid key"]}`, nil
			}
			return itchUpstream(1, 0)(call)
		}
	}
	cases := []struct {
		name      string
		upstream  upstreamtest.Respond
		token     string
		result    license.Result
		upstreams int
	}{
		{"owner on page 3", itchUpstream(3, 50), "owner", license.Owned, 4},
		{"owner on page 50", itchUpstream(50, 50), "owner", license.Owned, 51},
		{"never owned, never empty", itchUpstream(0, 0), "endless", license.NotOwned, 51},
		{"other games only", itchUpstream(0, 1), "nonowner", license.NotOwned, 3},
		{"profile refused", failing("/profile"), "owner", license.Unknown, 1},
		{"owned keys refused", failing("/profile/owned-keys"), "owner", license.Unknown, 2},
		{"token with a newline", upstreamtest.Unused(t), "a\nb", license.Unknown, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client, transport := newTestClient(c.upstream)
			if result := client.License(context.Background(), c.token); result != c.result {
				t.Errorf("result %v, want %v", result, c.result)
			}
			if got := len(transport.Calls()); got != c.upstreams {
				t.Errorf("%d upstream calls, want %d", got, c.upstreams)
			}
		})
	}
}

func TestUpstreamRequests(t *testing.T) {
	client, transport := newTestClient(itchUpstream(1, 0))
	client.License(context.Background(), "nonowner.1110é")
	want := []upstreamtest.Call{
		{Method: http.MethodGet, Path: "/profile", Authorization: "Bearer nonowner.1110\xe9"},
		{Method: http.MethodGet, Path: "/profile/owned-keys", RawQuery: "page=1", Authorization: "Bearer nonowner.1110\xe9"},
	}
	if calls := transport.Calls(); !slices.Equal(calls, want) {
		t.Fatalf("calls %+v\nwant  %+v", calls, want)
	}
}

func TestCacheIsKeyedByUserID(t *testing.T) {
	client, transport := newTestClient(itchUpstream(1, 0))
	client.License(context.Background(), "first")
	client.License(context.Background(), "another-token-same-user")
	var paths []string
	for _, call := range transport.Calls() {
		paths = append(paths, call.Path)
	}
	if want := "/profile,/profile/owned-keys,/profile"; strings.Join(paths, ",") != want {
		t.Fatalf("calls %v, want %s", paths, want)
	}
}

func TestConcurrentLoginsShareOneScan(t *testing.T) {
	const logins = 10
	var profiles, ownedKeyPages atomic.Int32
	allProfiled := make(chan struct{})
	scan := itchUpstream(0, maxOwnedKeyPages+10)
	client, _ := newTestClient(func(call upstreamtest.Call) (int, string, error) {
		if call.Path == "/profile" {
			if profiles.Add(1) == logins {
				close(allProfiled)
			}
		} else {
			ownedKeyPages.Add(1)
			<-allProfiled
		}
		return scan(call)
	})
	var requests sync.WaitGroup
	for range logins {
		requests.Go(func() {
			if result := client.License(context.Background(), "same-account"); result != license.NotOwned {
				t.Errorf("result %v", result)
			}
		})
	}
	requests.Wait()
	if got := ownedKeyPages.Load(); got != maxOwnedKeyPages {
		t.Fatalf("%d owned-keys requests for %d overlapping logins of one account, want %d", got, logins, maxOwnedKeyPages)
	}
}

func TestLoginURLAndCallbackPage(t *testing.T) {
	client, _ := newTestClient(upstreamtest.Unused(t))
	if got, want := client.LoginURL(), "https://itch.io/user/oauth?client_id=3ebb49fdae8199285348d6bbbbf77af1&scope=profile:owned%20profile:me&response_type=token&redirect_uri=https%3A%2F%2Fbuckshot-roulette-macos.hloth.dev%2Fapi%2Fauth%2Fitch.io%2Fcallback"; got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	page := httptest.NewRecorder()
	ServeCallbackPage(page, httptest.NewRequest(http.MethodGet, "/auth/itch.io/callback?access_token=abc", nil))
	if page.Code != http.StatusOK || page.Header().Get("Content-Type") != "text/html;charset=utf-8" || page.Header().Get("Content-Length") != "635" {
		t.Fatalf("page: %d %v", page.Code, page.Header())
	}
	if !strings.Contains(page.Body.String(), `params.get("access_token")`) {
		t.Fatal("page body is not the callback page")
	}
}
