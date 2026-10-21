package main

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

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

// itchUpstream serves a profile for user 1001 and owned keys that hold the game on ownedPage (0: never).
func itchUpstream(ownedPage, lastPage int) func(upstreamCall) (int, string, error) {
	return func(call upstreamCall) (int, string, error) {
		if call.path == "/profile" {
			return http.StatusOK, `{"user":{"id":1001}}`, nil
		}
		var page int
		fmt.Sscanf(call.rawQuery, "page=%d", &page)
		switch {
		case page == ownedPage:
			return http.StatusOK, `{"owned_keys":[{"game_id":1},{"game_id":2443623}]}`, nil
		case lastPage == 0 || page <= lastPage:
			return http.StatusOK, fmt.Sprintf(`{"owned_keys":[{"game_id":%d}]}`, 1000+page), nil
		}
		return http.StatusOK, `{"owned_keys":[]}`, nil
	}
}

func TestItchCallback(t *testing.T) {
	cases := []struct {
		name      string
		upstream  func(upstreamCall) (int, string, error)
		body      string
		location  string
		upstreams int
	}{
		{"owner on page 3", itchUpstream(3, 50), "token=owner", testPublicURL + "/#success", 4},
		{"owner on page 50", itchUpstream(50, 50), "token=owner", testPublicURL + "/#success", 51},
		{"never owned, never empty", itchUpstream(0, 0), "token=endless", testPublicURL + "/#error-itch.io-no-license", 51},
		{"other games only", itchUpstream(0, 1), "token=nonowner", testPublicURL + "/#error-itch.io-no-license", 3},
		{"token with a newline", noUpstream(t), "token=a%0Ab", testPublicURL + "/#error-itch.io-license-unknown", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, upstream := newTestApp(t, c.upstream)
			recorder := serveBody(a, http.MethodPost, "/auth/itch.io/callback", c.body, "Content-Type", "application/x-www-form-urlencoded")
			assertRedirect(t, recorder, c.location)
			if got := upstream.callCount(); got != c.upstreams {
				t.Errorf("%d upstream calls, want %d", got, c.upstreams)
			}
		})
	}
}

func TestItchUpstreamRequests(t *testing.T) {
	a, upstream := newTestApp(t, itchUpstream(1, 0))
	serveBody(a, http.MethodPost, "/auth/itch.io/callback", "token=nonowner.1110%C3%A9", "Content-Type", "application/x-www-form-urlencoded")
	want := []upstreamCall{
		{method: http.MethodGet, path: "/profile", authorization: "Bearer nonowner.1110\xe9"},
		{method: http.MethodGet, path: "/profile/owned-keys", rawQuery: "page=1", authorization: "Bearer nonowner.1110\xe9"},
	}
	if !slices.Equal(upstream.calls, want) {
		t.Fatalf("calls %+v\nwant  %+v", upstream.calls, want)
	}
}

func TestItchCacheIsKeyedByUserID(t *testing.T) {
	a, upstream := newTestApp(t, itchUpstream(1, 0))
	post := func(token string) {
		serveBody(a, http.MethodPost, "/auth/itch.io/callback", "token="+token, "Content-Type", "application/x-www-form-urlencoded")
	}
	post("first")
	post("another-token-same-user")
	paths := make([]string, 0, len(upstream.calls))
	for _, call := range upstream.calls {
		paths = append(paths, call.path)
	}
	if want := "/profile,/profile/owned-keys,/profile"; strings.Join(paths, ",") != want {
		t.Fatalf("calls %v, want %s", paths, want)
	}
}

func TestConcurrentItchLoginsShareOneScan(t *testing.T) {
	const logins = 10
	var profiles, ownedKeyPages atomic.Int32
	allProfiled := make(chan struct{})
	scan := itchUpstream(0, maxOwnedKeyPages+10)
	a, _ := newTestApp(t, func(call upstreamCall) (int, string, error) {
		if call.path == "/profile" {
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
			recorder := serveBody(a, http.MethodPost, "/auth/itch.io/callback", "token=same-account", "Content-Type", "application/x-www-form-urlencoded")
			if got := recorder.Header().Get("Location"); got != testPublicURL+"/#error-itch.io-no-license" {
				t.Errorf("Location %q", got)
			}
		})
	}
	requests.Wait()
	if got := ownedKeyPages.Load(); got != maxOwnedKeyPages {
		t.Fatalf("%d owned-keys requests for %d overlapping logins of one account, want %d", got, logins, maxOwnedKeyPages)
	}
}

func TestItchLoginAndPage(t *testing.T) {
	a, _ := newTestApp(t, noUpstream(t))
	assertRedirect(t, serve(a, http.MethodGet, "/auth/itch.io/"), "https://itch.io/user/oauth?client_id=3ebb49fdae8199285348d6bbbbf77af1&scope=profile:owned%20profile:me&response_type=token&redirect_uri=https%3A%2F%2Fbuckshot-roulette-macos.hloth.dev%2Fapi%2Fauth%2Fitch.io%2Fcallback")
	page := serve(a, http.MethodGet, "/auth/itch.io/callback?access_token=abc", "Cookie", "license=garbage")
	if page.Code != http.StatusOK || page.Header().Get("Content-Type") != "text/html;charset=utf-8" || page.Header().Get("Content-Length") != "635" {
		t.Fatalf("page: %d %v", page.Code, page.Header())
	}
	if !strings.Contains(page.Body.String(), `params.get("access_token")`) {
		t.Fatal("page body is not the callback page")
	}
}
