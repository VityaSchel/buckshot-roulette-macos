package server

import (
	"net/http"
	"strings"
	"testing"

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/upstream/upstreamtest"
)

func TestUnknownRoutesAndMethods(t *testing.T) {
	a, _ := newTestApp(t, upstreamtest.Unused(t))
	cases := []struct{ method, target string }{
		{http.MethodGet, "/"},
		{http.MethodGet, "/nope"},
		{http.MethodGet, "/Download"},
		{http.MethodGet, "/d%6Fwnload"},
		{http.MethodGet, "/download//"},
		{http.MethodGet, "/api/download"},
		{http.MethodGet, "/auth"},
		{http.MethodPost, "/download"},
		{http.MethodPost, "/auth/steam"},
		{http.MethodPost, "/logout"},
		{http.MethodOptions, "/download"},
		{http.MethodDelete, "/download"},
		{http.MethodPut, "/auth/itch.io/callback"},
		{"FOO", "/download"},
		{"get", "/logout"},
	}
	for _, c := range cases {
		recorder := serve(a, c.method, c.target, "Cookie", "license=tampered.x")
		if recorder.Code != http.StatusNotFound || recorder.Body.String() != `{"ok":false,"error":"Not found"}` {
			t.Errorf("%s %s: %d %q", c.method, c.target, recorder.Code, recorder.Body.String())
		}
		expectHeaders(t, recorder.Header(), map[string]string{"Content-Type": "application/json", "Content-Length": "32"})
	}
}

func TestLoginRedirects(t *testing.T) {
	a, _ := newTestApp(t, upstreamtest.Unused(t))
	assertRedirect(t, serve(a, http.MethodGet, "/auth/steam/?openid.mode=cancel"), "https://steamcommunity.com/openid/login?openid.ns=http://specs.openid.net/auth/2.0&openid.mode=checkid_setup&openid.return_to=https%3A%2F%2Fbuckshot-roulette-macos.hloth.dev%2Fapi%2Fauth%2Fsteam%2Fcallback&openid.realm=https://buckshot-roulette-macos.hloth.dev/api/&openid.identity=http://specs.openid.net/auth/2.0/identifier_select&openid.claimed_id=http://specs.openid.net/auth/2.0/identifier_select")
	assertRedirect(t, serve(a, http.MethodGet, "/auth/itch.io/"), "https://itch.io/user/oauth?client_id=3ebb49fdae8199285348d6bbbbf77af1&scope=profile:owned%20profile:me&response_type=token&redirect_uri=https%3A%2F%2Fbuckshot-roulette-macos.hloth.dev%2Fapi%2Fauth%2Fitch.io%2Fcallback")
}

func TestItchCallbackPageSkipsTheLicense(t *testing.T) {
	a, _ := newTestApp(t, upstreamtest.Unused(t))
	page := serve(a, http.MethodGet, "/auth/itch.io/callback?access_token=abc", "Cookie", "license=garbage")
	if page.Code != http.StatusOK || page.Header().Get("Content-Type") != "text/html;charset=utf-8" || page.Header().Get("Content-Length") != "635" {
		t.Fatalf("page: %d %v", page.Code, page.Header())
	}
	if !strings.Contains(page.Body.String(), `params.get("access_token")`) {
		t.Fatal("page body is not the callback page")
	}
}

func TestBadLicenseSignatureClearsThroughTheAPI(t *testing.T) {
	a, _ := newTestApp(t, upstreamtest.Unused(t))
	for _, request := range []struct{ method, target, body string }{
		{http.MethodGet, "/download", ""},
		{http.MethodHead, "/download", ""},
		{http.MethodGet, "/auth/steam/callback", ""},
		{http.MethodPost, "/auth/itch.io/callback", "token=owner"},
	} {
		recorder := serveBody(a, request.method, request.target, request.body, "Cookie", "license=%7B%7D.forged", "Content-Type", "application/x-www-form-urlencoded")
		assertRedirect(t, recorder, "https://buckshot-roulette-macos.hloth.dev/api/logout")
		if recorder.Header().Get("Set-Cookie") != "" {
			t.Errorf("%s %s sets a cookie", request.method, request.target)
		}
	}
}

func TestLogout(t *testing.T) {
	a, _ := newTestApp(t, upstreamtest.Unused(t))
	const cleared = "license=; Max-Age=0; Path=/; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Secure; SameSite=Lax"
	cases := map[string]string{
		"":                            "",
		"foo=bar":                     "",
		"license=":                    cleared,
		"license=whatever":            cleared,
		"license=%E0%A4%A":            cleared,
		licenseHeader(validCookieRaw): cleared,
	}
	for cookie, want := range cases {
		recorder := serve(a, http.MethodGet, "/logout/", "Cookie", cookie)
		assertRedirect(t, recorder, testPublicURL+"/#try-again")
		if got := recorder.Header().Get("Set-Cookie"); got != want {
			t.Errorf("Cookie %q: Set-Cookie %q, want %q", cookie, got, want)
		}
	}
	assertRedirect(t, serve(a, http.MethodGet, "/auth/../logout"), testPublicURL+"/#try-again")
}

func TestPanicsBecomeServerErrors(t *testing.T) {
	a, _ := newTestApp(t, upstreamtest.Unused(t))
	a.routes["/panic"] = route{get: func(http.ResponseWriter, *http.Request) {
		var values []int
		_ = values[1]
	}}
	assertJSONError(t, serve(a, http.MethodGet, "/panic"), http.StatusInternalServerError, "Internal server error")
}
