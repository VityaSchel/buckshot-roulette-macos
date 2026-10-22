package server

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/steam"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/upstream/upstreamtest"
)

const issuedCookieAttributes = "; Max-Age=604800; Path=/; HttpOnly; Secure; SameSite=Lax"

func steamUpstream(openIDStatus int, openIDBody string, openIDErr error, ownedGames string) upstreamtest.Respond {
	return func(call upstreamtest.Call) (int, string, error) {
		if call.Path == "/openid/login" {
			return openIDStatus, openIDBody, openIDErr
		}
		return http.StatusOK, ownedGames, nil
	}
}

func TestSteamCallback(t *testing.T) {
	callback := steamCallbackTarget(testCallbackURL)
	cases := []struct {
		name      string
		target    string
		upstream  upstreamtest.Respond
		status    int
		location  string
		message   string
		upstreams int
	}{
		{"owner", callback, steamUpstream(200, openIDValidAnswer, nil, ownsBuckshot), 302, testPublicURL + "/#success", "", 2},
		{"not owned", callback, steamUpstream(200, openIDValidAnswer, nil, `{"response":{"game_count":0}}`), 302, testPublicURL + "/#error-steam-no-license", "", 2},
		{"unknown", callback, steamUpstream(200, openIDValidAnswer, nil, `{"response":{}}`), 302, testPublicURL + "/#error-steam-license-unknown", "", 2},
		{"GetOwnedGames status ignored", callback, func(call upstreamtest.Call) (int, string, error) {
			if call.Path == "/openid/login" {
				return http.StatusOK, openIDValidAnswer, nil
			}
			return http.StatusInternalServerError, ownsBuckshot, nil
		}, 302, testPublicURL + "/#success", "", 2},
		{"no parameters", "/auth/steam/callback", upstreamtest.Unused(t), 400, "", steam.ErrCallback.Message, 0},
		{"rejected by Steam", callback, steamUpstream(200, "ns:http://specs.openid.net/auth/2.0\nis_valid:false\n", nil, ""), 401, "", steam.ErrState.Message, 1},
		{"Steam answers 500 with is_valid:true", callback, steamUpstream(500, openIDValidAnswer, nil, ""), 401, "", steam.ErrState.Message, 1},
		{"Steam unreachable", callback, steamUpstream(0, "", errors.New("connection refused"), ""), 502, "", steam.ErrUnavailable.Message, 1},
		{"forged return_to", steamCallbackTarget("https://evil.example/"), upstreamtest.Unused(t), 401, "", steam.ErrState.Message, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, transport := newTestApp(t, c.upstream)
			recorder := serve(a, http.MethodGet, c.target)
			if c.status == http.StatusFound {
				assertRedirect(t, recorder, c.location)
			} else {
				assertJSONError(t, recorder, c.status, c.message)
			}
			cookie := recorder.Header().Get("Set-Cookie")
			if granted := strings.HasSuffix(c.location, "#success"); granted != strings.HasSuffix(cookie, issuedCookieAttributes) {
				t.Errorf("Set-Cookie %q", cookie)
			}
			if got := len(transport.Calls()); got != c.upstreams {
				t.Errorf("%d upstream calls, want %d", got, c.upstreams)
			}
		})
	}
}

func TestSteamCacheComesAfterOpenID(t *testing.T) {
	answer := openIDValidAnswer
	a, transport := newTestApp(t, func(call upstreamtest.Call) (int, string, error) {
		if call.Path == "/openid/login" {
			return http.StatusOK, answer, nil
		}
		return http.StatusOK, ownsBuckshot, nil
	})
	target := steamCallbackTarget(testCallbackURL)
	serve(a, http.MethodGet, target)
	assertRedirect(t, serve(a, http.MethodGet, target), testPublicURL+"/#success")
	if got := len(transport.Calls()); got != 3 {
		t.Fatalf("%d upstream calls, want 3 (the second login is cached)", got)
	}
	answer = "is_valid:false\n"
	assertJSONError(t, serve(a, http.MethodGet, target), http.StatusUnauthorized, steam.ErrState.Message)
}

func TestItchCallback(t *testing.T) {
	itch := func(call upstreamtest.Call) (int, string, error) {
		switch {
		case call.Path == "/profile":
			return http.StatusOK, `{"user":{"id":1001}}`, nil
		case call.Authorization == "Bearer owner":
			return http.StatusOK, `{"owned_keys":[{"game_id":2443623}]}`, nil
		}
		return http.StatusOK, `{"owned_keys":[]}`, nil
	}
	cases := map[string]string{
		"token=owner":    testPublicURL + "/#success",
		"token=nonowner": testPublicURL + "/#error-itch.io-no-license",
		"token=a%0Ab":    testPublicURL + "/#error-itch.io-license-unknown",
	}
	for body, location := range cases {
		a, _ := newTestApp(t, itch)
		recorder := serveBody(a, http.MethodPost, "/auth/itch.io/callback", body, "Content-Type", "application/x-www-form-urlencoded")
		assertRedirect(t, recorder, location)
		cookie := recorder.Header().Get("Set-Cookie")
		if granted := strings.HasSuffix(location, "#success"); granted != strings.HasSuffix(cookie, issuedCookieAttributes) {
			t.Errorf("%s: Set-Cookie %q", body, cookie)
		}
	}
}

func TestItchCallbackBodyFailuresAreServerErrors(t *testing.T) {
	a, _ := newTestApp(t, upstreamtest.Unused(t))
	for _, contentType := range []string{"", "text/plain", "application/json"} {
		recorder := serveBody(a, http.MethodPost, "/auth/itch.io/callback", "token=x", "Content-Type", contentType)
		assertJSONError(t, recorder, http.StatusInternalServerError, "Internal server error")
	}
}
