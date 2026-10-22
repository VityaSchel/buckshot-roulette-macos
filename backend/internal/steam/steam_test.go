package steam

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/config"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/license"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/upstream/upstreamtest"
)

const ownsBuckshot = `{"response":{"game_count":1,"games":[{"appid":2835570,"playtime_forever":42}]}}`

func newTestClient(respond upstreamtest.Respond) (*Client, *upstreamtest.Transport) {
	cfg := config.Config{
		SteamAPIKey:      "test-steam-key",
		PublicAPIURL:     testAPIURL,
		SteamCallbackURL: testCallbackURL,
		Origins:          config.Origins{SteamCommunity: "http://community.test", SteamAPI: "http://api.test"},
	}
	httpClient, transport := upstreamtest.NewClient(respond)
	return New(cfg, httpClient, time.Now), transport
}

func steamUpstream(openIDStatus int, openIDBody string, openIDErr error, ownedGames string) upstreamtest.Respond {
	return func(call upstreamtest.Call) (int, string, error) {
		if call.Path == "/openid/login" {
			return openIDStatus, openIDBody, openIDErr
		}
		return http.StatusOK, ownedGames, nil
	}
}

func TestOwnership(t *testing.T) {
	cases := map[string]license.Result{
		ownsBuckshot: license.Owned,
		`{"response":{"game_count":2,"games":[{"appid":730},{"appid":2835570}]}}`: license.Owned,
		`{"response":{"games":[{"appid":2835570.0}]}}`:                            license.Owned,
		`{"response":{"game_count":1,"games":[{"appid":2835570}]},"extra":true}`:  license.Owned,
		`{"response":{"game_count":1,"games":[{"appid":730}]}}`:                   license.NotOwned,
		`{"response":{"game_count":0}}`:                                           license.NotOwned,
		`{"response":{"game_count":-0}}`:                                          license.NotOwned,
		`{"response":{"game_count":0,"games":[]}}`:                                license.NotOwned,
		`{"response":{"games":[{"appid":1}],"game_count":"x"}}`:                   license.NotOwned,
		`{"response":{}}`:               license.Unknown,
		`{"response":{"game_count":2}}`: license.Unknown,
		`{"response":{"game_count":1,"games":[{"appid":"2835570"}]}}`: license.Unknown,
		`{"response":{"games":[{"appid":2835570,"playtime":1e400}]}}`: license.Owned,
		`{"response":{"games":[{"appid":1e400}],"game_count":0}}`:     license.NotOwned,
		`{"response":{"games":[null]}}`:                               license.Unknown,
		`{"response":[]}`:                                             license.Unknown,
		`{"error":"nope"}`:                                            license.Unknown,
		`[{"response":{"game_count":0}}]`:                             license.Unknown,
		`{"response":{"game_count":0}} trailing`:                      license.Unknown,
		"<html>Forbidden</html>":                                      license.Unknown,
		"":                                                            license.Unknown,
		"\xEF\xBB\xBF" + ownsBuckshot:                                 license.Owned,
		"\xEF\xBB\xBF\xEF\xBB\xBF" + ownsBuckshot:                     license.Unknown,
	}
	for body, want := range cases {
		if got, ok := ownership([]byte(body)); got != want || ok != (want != license.Unknown) {
			t.Errorf("ownership(%s) = %v, %v; want %v", body, got, ok, want)
		}
	}
}

func TestLoginURL(t *testing.T) {
	client, _ := newTestClient(upstreamtest.Unused(t))
	want := "https://steamcommunity.com/openid/login?openid.ns=http://specs.openid.net/auth/2.0&openid.mode=checkid_setup&openid.return_to=https%3A%2F%2Fbuckshot-roulette-macos.hloth.dev%2Fapi%2Fauth%2Fsteam%2Fcallback&openid.realm=https://buckshot-roulette-macos.hloth.dev/api/&openid.identity=http://specs.openid.net/auth/2.0/identifier_select&openid.claimed_id=http://specs.openid.net/auth/2.0/identifier_select"
	if got := client.LoginURL(); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestVerify(t *testing.T) {
	cases := []struct {
		name      string
		upstream  upstreamtest.Respond
		err       error
		upstreams int
	}{
		{"confirmed", steamUpstream(200, openIDValidAnswer, nil, ""), nil, 1},
		{"rejected by Steam", steamUpstream(200, "ns:http://specs.openid.net/auth/2.0\nis_valid:false\n", nil, ""), ErrState, 1},
		{"Steam answers 500 with is_valid:true", steamUpstream(500, openIDValidAnswer, nil, ""), ErrState, 1},
		{"Steam unreachable", steamUpstream(0, "", errors.New("connection refused"), ""), ErrUnavailable, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client, transport := newTestClient(c.upstream)
			steamID, err := client.Verify(context.Background(), validAssertion().query())
			if err != c.err || err == nil && steamID != testSteamID {
				t.Errorf("got %q, %v; want %v", steamID, err, c.err)
			}
			if got := len(transport.Calls()); got != c.upstreams {
				t.Errorf("%d upstream calls, want %d", got, c.upstreams)
			}
		})
	}
	client, _ := newTestClient(upstreamtest.Unused(t))
	forged := validAssertion()
	forged.returnTo = "https://evil.example/"
	if _, err := client.Verify(context.Background(), forged.query()); !errors.Is(err, ErrState) {
		t.Errorf("forged return_to: %v", err)
	}
	if _, err := client.Verify(context.Background(), ""); err != ErrCallback {
		t.Errorf("no parameters: %v", err)
	}
}

func TestUpstreamRequests(t *testing.T) {
	client, transport := newTestClient(steamUpstream(200, openIDValidAnswer, nil, ownsBuckshot))
	steamID, err := client.Verify(context.Background(), validAssertion().query())
	if err != nil {
		t.Fatal(err)
	}
	if result := client.License(context.Background(), steamID); result != license.Owned {
		t.Fatalf("result %v", result)
	}
	wantQuery := "key=test-steam-key&input_json=" + url.QueryEscape(`{"steamid":"76561197960010001","appids_filter":[2835570],"include_appinfo":false,"include_played_free_games":false}`)
	want := []upstreamtest.Call{
		{Method: http.MethodPost, Path: "/openid/login", ContentType: "application/x-www-form-urlencoded", Body: validAssertion().checkAuthenticationForm()},
		{Method: http.MethodGet, Path: "/IPlayerService/GetOwnedGames/v1/", RawQuery: wantQuery},
	}
	if calls := transport.Calls(); !slices.Equal(calls, want) {
		t.Fatalf("calls %+v\nwant  %+v", calls, want)
	}
}

func TestLicense(t *testing.T) {
	cases := []struct {
		name       string
		ownedGames string
		status     int
		result     license.Result
	}{
		{"owner", ownsBuckshot, 200, license.Owned},
		{"not owned", `{"response":{"game_count":0}}`, 200, license.NotOwned},
		{"unknown", `{"response":{}}`, 200, license.Unknown},
		{"status ignored", ownsBuckshot, 500, license.Owned},
	}
	for _, c := range cases {
		client, _ := newTestClient(func(upstreamtest.Call) (int, string, error) { return c.status, c.ownedGames, nil })
		if result := client.License(context.Background(), testSteamID); result != c.result {
			t.Errorf("%s: %v, want %v", c.name, result, c.result)
		}
	}
	client, _ := newTestClient(func(upstreamtest.Call) (int, string, error) { return 0, "", errors.New("connection refused") })
	if result := client.License(context.Background(), testSteamID); result != license.Unknown {
		t.Errorf("unreachable: %v", result)
	}
}

func TestTestIDSkipsGetOwnedGames(t *testing.T) {
	client, transport := newTestClient(steamUpstream(200, openIDValidAnswer, nil, `{"response":{"game_count":0}}`))
	tester := validAssertion()
	tester.claimedID = claimedIDBase + testID
	tester.identity = tester.claimedID
	steamID, err := client.Verify(context.Background(), tester.query())
	if err != nil || client.License(context.Background(), steamID) != license.Owned {
		t.Fatalf("the test account: %q, %v", steamID, err)
	}
	if calls := transport.Calls(); len(calls) != 1 {
		t.Fatalf("%d upstream calls, want only check_authentication", len(calls))
	}
}
