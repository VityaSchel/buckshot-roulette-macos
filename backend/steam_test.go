package main

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const (
	testSteamID       = "76561197960010001"
	testCallbackURL   = testAPIURL + "auth/steam/callback"
	validSignedFields = "signed,op_endpoint,claimed_id,identity,return_to,response_nonce,assoc_handle"
	openIDValidAnswer = "ns:http://specs.openid.net/auth/2.0\nis_valid:true\n"
	ownsBuckshot      = `{"response":{"game_count":1,"games":[{"appid":2835570,"playtime_forever":42}]}}`
)

func validAssertion() steamAssertion {
	claimedID := steamClaimedIDBase + testSteamID
	return steamAssertion{
		namespace:     openIDNamespace,
		mode:          "id_res",
		opEndpoint:    steamOpenIDEndpoint,
		claimedID:     claimedID,
		identity:      claimedID,
		returnTo:      testCallbackURL,
		responseNonce: "2026-10-20T12:00:00Zparity",
		assocHandle:   "1234567890",
		signed:        validSignedFields,
		signature:     "Kq3+/parity=",
	}
}

func (s steamAssertion) query() string {
	return encodeForm(
		queryPair{"openid.ns", s.namespace},
		queryPair{"openid.mode", s.mode},
		queryPair{"openid.op_endpoint", s.opEndpoint},
		queryPair{"openid.claimed_id", s.claimedID},
		queryPair{"openid.identity", s.identity},
		queryPair{"openid.return_to", s.returnTo},
		queryPair{"openid.response_nonce", s.responseNonce},
		queryPair{"openid.assoc_handle", s.assocHandle},
		queryPair{"openid.signed", s.signed},
		queryPair{"openid.sig", s.signature},
	)
}

func TestSteamAssertionValidation(t *testing.T) {
	cases := []struct {
		name   string
		change func(*steamAssertion)
		err    error
	}{
		{"valid", func(*steamAssertion) {}, nil},
		{"return_to with a query Steam appended", func(s *steamAssertion) { s.returnTo += "?steam=1" }, nil},
		{"extra signed fields", func(s *steamAssertion) { s.signed += ",ns,foo" }, nil},
		{"signed fields in another order", func(s *steamAssertion) {
			s.signed = "assoc_handle,response_nonce,return_to,identity,claimed_id,op_endpoint"
		}, nil},
		{"other namespace", func(s *steamAssertion) { s.namespace = "urn:other" }, errSteamState},
		{"setup mode", func(s *steamAssertion) { s.mode = "checkid_setup" }, errSteamState},
		{"cancel mode", func(s *steamAssertion) { s.mode = "cancel" }, errSteamState},
		{"other OP endpoint", func(s *steamAssertion) { s.opEndpoint = "https://evil.example/openid/login" }, errSteamState},
		{"return_to on another host", func(s *steamAssertion) { s.returnTo = "https://evil.example/api/auth/steam/callback" }, errSteamState},
		{"return_to on another path", func(s *steamAssertion) { s.returnTo = testAPIURL + "download" }, errSteamState},
		{"return_to with a longer path", func(s *steamAssertion) { s.returnTo = testCallbackURL + "x" }, errSteamState},
		{"return_to with a fragment", func(s *steamAssertion) { s.returnTo = testCallbackURL + "#x" }, errSteamState},
		{"claimed_id not signed", func(s *steamAssertion) {
			s.signed = "signed,op_endpoint,identity,return_to,response_nonce,assoc_handle"
		}, errSteamState},
		{"return_to not signed", func(s *steamAssertion) {
			s.signed = "signed,op_endpoint,claimed_id,identity,response_nonce,assoc_handle"
		}, errSteamState},
		{"signed list with spaces", func(s *steamAssertion) { s.signed = strings.ReplaceAll(validSignedFields, ",", ", ") }, errSteamState},
		{"identity differs", func(s *steamAssertion) { s.identity = steamClaimedIDBase + "76561197960030003" }, errSteamState},
		{"claimed_id on another host", func(s *steamAssertion) {
			s.claimedID = "https://evil.example/openid/id/" + testSteamID
			s.identity = s.claimedID
		}, errSteamState},
		{"claimed_id over http", func(s *steamAssertion) {
			s.claimedID = "http://steamcommunity.com/openid/id/" + testSteamID
			s.identity = s.claimedID
		}, errSteamState},
		{"claimed_id with a trailing slash", func(s *steamAssertion) {
			s.claimedID += "/"
			s.identity = s.claimedID
		}, errSteamID},
		{"claimed_id with letters", func(s *steamAssertion) {
			s.claimedID = steamClaimedIDBase + "notanumber"
			s.identity = s.claimedID
		}, errSteamID},
		{"claimed_id too short", func(s *steamAssertion) {
			s.claimedID = steamClaimedIDBase + "123"
			s.identity = s.claimedID
		}, errSteamID},
		{"claimed_id too long", func(s *steamAssertion) {
			s.claimedID = steamClaimedIDBase + testSteamID + "0"
			s.identity = s.claimedID
		}, errSteamID},
	}
	for _, c := range cases {
		assertion := validAssertion()
		c.change(&assertion)
		id, err := assertion.steamID(testCallbackURL)
		if !errors.Is(err, c.err) {
			t.Errorf("%s: error %v, want %v", c.name, err, c.err)
		}
		if c.err == nil && id != strings.TrimPrefix(assertion.claimedID, steamClaimedIDBase) {
			t.Errorf("%s: steam id %q", c.name, id)
		}
	}
}

func TestParseSteamAssertion(t *testing.T) {
	query := validAssertion().query()
	if _, err := parseSteamAssertion(query); err != nil {
		t.Fatalf("valid query: %v", err)
	}
	for _, field := range []string{"ns", "mode", "op_endpoint", "claimed_id", "identity", "return_to", "response_nonce", "assoc_handle", "signed", "sig"} {
		var kept []string
		for pair := range strings.SplitSeq(query, "&") {
			if !strings.HasPrefix(pair, "openid."+field+"=") {
				kept = append(kept, pair)
			}
		}
		if _, err := parseSteamAssertion(strings.Join(kept, "&")); !errors.Is(err, errSteamCallback) {
			t.Errorf("without openid.%s: %v", field, err)
		}
	}
	assertion, err := parseSteamAssertion(query + "&openid.sig=second&openid.sig=")
	if err != nil || assertion.signature != "" {
		t.Errorf("the last value must win, got %q, %v", assertion.signature, err)
	}
	assertion, _ = parseSteamAssertion(strings.Replace(query, "openid.sig=Kq3%2B%2Fparity%3D", "openid.sig=Kq3+/parity=", 1))
	if assertion.signature != "Kq3 /parity=" {
		t.Errorf("a raw '+' is a space: %q", assertion.signature)
	}
}

func TestCheckAuthenticationForm(t *testing.T) {
	assertion := validAssertion()
	want := "openid.ns=http%3A%2F%2Fspecs.openid.net%2Fauth%2F2.0&openid.mode=check_authentication" +
		"&openid.op_endpoint=https%3A%2F%2Fsteamcommunity.com%2Fopenid%2Flogin" +
		"&openid.claimed_id=https%3A%2F%2Fsteamcommunity.com%2Fopenid%2Fid%2F76561197960010001" +
		"&openid.identity=https%3A%2F%2Fsteamcommunity.com%2Fopenid%2Fid%2F76561197960010001" +
		"&openid.return_to=https%3A%2F%2Fbuckshot-roulette-macos.hloth.dev%2Fapi%2Fauth%2Fsteam%2Fcallback" +
		"&openid.response_nonce=2026-10-20T12%3A00%3A00Zparity&openid.assoc_handle=1234567890" +
		"&openid.signed=signed%2Cop_endpoint%2Cclaimed_id%2Cidentity%2Creturn_to%2Cresponse_nonce%2Cassoc_handle" +
		"&openid.sig=Kq3%2B%2Fparity%3D"
	if got := assertion.checkAuthenticationForm(); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestConfirmsAssertion(t *testing.T) {
	cases := map[string]bool{
		openIDValidAnswer: true,
		"is_valid:true":   true,
		"ns:http://specs.openid.net/auth/2.0\nis_valid:false\n":                                  false,
		"ns:http://specs.openid.net/auth/2.0\nis_valid:false\ninvalidate_handle:is_valid:true\n": false,
		"ns:http://specs.openid.net/auth/2.0 is_valid:true\n":                                    false,
		"ns:http://specs.openid.net/auth/2.0\nis_valid:false\nis_valid:true\n":                   false,
		"ns:http://specs.openid.net/auth/2.0\nis_valid:true\nis_valid:true\n":                    false,
		"ns:http://specs.openid.net/auth/2.0\nis_valid: true\n":                                  false,
		"ns:http://specs.openid.net/auth/2.0\nis_valid:TRUE\n":                                   false,
		"ns:http://specs.openid.net/auth/2.0\r\nis_valid:true\r\n":                               true,
		"ns:http://specs.openid.net/auth/2.0\r\nis_valid:false\r\n":                              false,
		"ns:http://specs.openid.net/auth/2.0\nis_valid:true\r\r\n":                               false,
		"<html><body>is_valid:true</body></html>":                                                false,
		"\xEF\xBB\xBFis_valid:true\n":                                                            true,
		"":                                                                                       false,
	}
	for body, want := range cases {
		if got := confirmsAssertion([]byte(body)); got != want {
			t.Errorf("confirmsAssertion(%q) = %v, want %v", body, got, want)
		}
	}
}

func TestSteamOwnership(t *testing.T) {
	cases := map[string]licenseResult{
		ownsBuckshot: licenseOwned,
		`{"response":{"game_count":2,"games":[{"appid":730},{"appid":2835570}]}}`: licenseOwned,
		`{"response":{"games":[{"appid":2835570.0}]}}`:                            licenseOwned,
		`{"response":{"game_count":1,"games":[{"appid":2835570}]},"extra":true}`:  licenseOwned,
		`{"response":{"game_count":1,"games":[{"appid":730}]}}`:                   licenseNotOwned,
		`{"response":{"game_count":0}}`:                                           licenseNotOwned,
		`{"response":{"game_count":-0}}`:                                          licenseNotOwned,
		`{"response":{"game_count":0,"games":[]}}`:                                licenseNotOwned,
		`{"response":{"games":[{"appid":1}],"game_count":"x"}}`:                   licenseNotOwned,
		`{"response":{}}`:               licenseUnknown,
		`{"response":{"game_count":2}}`: licenseUnknown,
		`{"response":{"game_count":1,"games":[{"appid":"2835570"}]}}`: licenseUnknown,
		`{"response":{"games":[{"appid":2835570,"playtime":1e400}]}}`: licenseOwned,
		`{"response":{"games":[{"appid":1e400}],"game_count":0}}`:     licenseNotOwned,
		`{"response":{"games":[null]}}`:                               licenseUnknown,
		`{"response":[]}`:                                             licenseUnknown,
		`{"error":"nope"}`:                                            licenseUnknown,
		`[{"response":{"game_count":0}}]`:                             licenseUnknown,
		`{"response":{"game_count":0}} trailing`:                      licenseUnknown,
		"<html>Forbidden</html>":                                      licenseUnknown,
		"":                                                            licenseUnknown,
		"\xEF\xBB\xBF" + ownsBuckshot:                                 licenseOwned,
		"\xEF\xBB\xBF\xEF\xBB\xBF" + ownsBuckshot:                     licenseUnknown,
	}
	for body, want := range cases {
		if got := steamOwnership([]byte(body)); got != want {
			t.Errorf("steamOwnership(%s) = %v, want %v", body, got, want)
		}
	}
}

func TestSteamLoginURL(t *testing.T) {
	a, _ := newTestApp(t, noUpstream(t))
	want := "https://steamcommunity.com/openid/login?openid.ns=http://specs.openid.net/auth/2.0&openid.mode=checkid_setup&openid.return_to=https%3A%2F%2Fbuckshot-roulette-macos.hloth.dev%2Fapi%2Fauth%2Fsteam%2Fcallback&openid.realm=https://buckshot-roulette-macos.hloth.dev/api/&openid.identity=http://specs.openid.net/auth/2.0/identifier_select&openid.claimed_id=http://specs.openid.net/auth/2.0/identifier_select"
	assertRedirect(t, serve(a, http.MethodGet, "/auth/steam/?openid.mode=cancel"), want)
}

func steamUpstream(openIDStatus int, openIDBody string, openIDErr error, ownedGames string) func(upstreamCall) (int, string, error) {
	return func(call upstreamCall) (int, string, error) {
		if call.path == "/openid/login" {
			return openIDStatus, openIDBody, openIDErr
		}
		return http.StatusOK, ownedGames, nil
	}
}

func TestSteamCallback(t *testing.T) {
	callback := "/auth/steam/callback?" + validAssertion().query()
	cases := []struct {
		name      string
		target    string
		upstream  func(upstreamCall) (int, string, error)
		status    int
		location  string
		message   string
		upstreams int
	}{
		{"owner", callback, steamUpstream(200, openIDValidAnswer, nil, ownsBuckshot), 302, testPublicURL + "/#success", "", 2},
		{"not owned", callback, steamUpstream(200, openIDValidAnswer, nil, `{"response":{"game_count":0}}`), 302, testPublicURL + "/#error-steam-no-license", "", 2},
		{"unknown", callback, steamUpstream(200, openIDValidAnswer, nil, `{"response":{}}`), 302, testPublicURL + "/#error-steam-license-unknown", "", 2},
		{"GetOwnedGames status ignored", callback, func(call upstreamCall) (int, string, error) {
			if call.path == "/openid/login" {
				return http.StatusOK, openIDValidAnswer, nil
			}
			return http.StatusInternalServerError, ownsBuckshot, nil
		}, 302, testPublicURL + "/#success", "", 2},
		{"no parameters", "/auth/steam/callback", noUpstream(t), 400, "", errSteamCallback.message, 0},
		{"rejected by Steam", callback, steamUpstream(200, "ns:http://specs.openid.net/auth/2.0\nis_valid:false\n", nil, ""), 401, "", errSteamState.message, 1},
		{"Steam answers 500 with is_valid:true", callback, steamUpstream(500, openIDValidAnswer, nil, ""), 401, "", errSteamState.message, 1},
		{"Steam unreachable", callback, steamUpstream(0, "", errors.New("connection refused"), ""), 502, "", errSteamUnavailable.message, 1},
		{"forged return_to", "/auth/steam/callback?" + func() string {
			s := validAssertion()
			s.returnTo = "https://evil.example/"
			return s.query()
		}(), noUpstream(t), 401, "", errSteamState.message, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, upstream := newTestApp(t, c.upstream)
			recorder := serve(a, http.MethodGet, c.target)
			if c.status == http.StatusFound {
				assertRedirect(t, recorder, c.location)
			} else {
				assertJSONError(t, recorder, c.status, c.message)
			}
			if got := upstream.callCount(); got != c.upstreams {
				t.Errorf("%d upstream calls, want %d", got, c.upstreams)
			}
		})
	}
}

func TestSteamCallbackUpstreamRequests(t *testing.T) {
	a, upstream := newTestApp(t, steamUpstream(200, openIDValidAnswer, nil, ownsBuckshot))
	recorder := serve(a, http.MethodGet, "/auth/steam/callback?"+validAssertion().query())
	assertRedirect(t, recorder, testPublicURL+"/#success")
	if got := recorder.Header().Get("Set-Cookie"); !strings.HasSuffix(got, "; Max-Age=604800; Path=/; HttpOnly; Secure; SameSite=Lax") {
		t.Errorf("Set-Cookie %q", got)
	}
	check, owned := upstream.calls[0], upstream.calls[1]
	if check.method != http.MethodPost || check.contentType != "application/x-www-form-urlencoded" || check.body != validAssertion().checkAuthenticationForm() {
		t.Errorf("check_authentication request %+v", check)
	}
	wantQuery := "key=test-steam-key&input_json=" + url.QueryEscape(`{"steamid":"76561197960010001","appids_filter":[2835570],"include_appinfo":false,"include_played_free_games":false}`)
	if owned.path != "/IPlayerService/GetOwnedGames/v1/" || owned.rawQuery != wantQuery {
		t.Errorf("GetOwnedGames request %s?%s", owned.path, owned.rawQuery)
	}
}

func TestSteamTestIDSkipsGetOwnedGames(t *testing.T) {
	a, upstream := newTestApp(t, steamUpstream(200, openIDValidAnswer, nil, `{"response":{"game_count":0}}`))
	assertion := validAssertion()
	assertion.claimedID = steamClaimedIDBase + steamTestID
	assertion.identity = assertion.claimedID
	assertRedirect(t, serve(a, http.MethodGet, "/auth/steam/callback?"+assertion.query()), testPublicURL+"/#success")
	if upstream.callCount() != 1 {
		t.Fatalf("%d upstream calls, want only check_authentication", upstream.callCount())
	}
}

func TestSteamCacheComesAfterOpenID(t *testing.T) {
	answer := openIDValidAnswer
	a, upstream := newTestApp(t, func(call upstreamCall) (int, string, error) {
		if call.path == "/openid/login" {
			return http.StatusOK, answer, nil
		}
		return http.StatusOK, ownsBuckshot, nil
	})
	target := "/auth/steam/callback?" + validAssertion().query()
	serve(a, http.MethodGet, target)
	assertRedirect(t, serve(a, http.MethodGet, target), testPublicURL+"/#success")
	if upstream.callCount() != 3 {
		t.Fatalf("%d upstream calls, want 3 (the second login is cached)", upstream.callCount())
	}
	answer = "is_valid:false\n"
	assertJSONError(t, serve(a, http.MethodGet, target), http.StatusUnauthorized, errSteamState.message)
}
