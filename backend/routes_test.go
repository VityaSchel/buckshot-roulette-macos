package main

import (
	"net/http"
	"testing"
)

func TestRoutePath(t *testing.T) {
	cases := map[string]string{
		"/":                     "/",
		"":                      "/",
		"/download":             "/download",
		"/download/":            "/download",
		"/download//":           "/download/",
		"//download":            "//download",
		"/auth/../logout":       "/logout",
		"/auth/%2e%2E/logout":   "/logout",
		"/auth/.%2e/logout":     "/logout",
		"/auth/steam/.":         "/auth/steam",
		"/auth/steam/./":        "/auth/steam",
		"/auth/steam/%2e":       "/auth/steam",
		"/../../download":       "/download",
		"/auth/steam/callback/": "/auth/steam/callback",
		"/d%6Fwnload":           "/d%6Fwnload",
		"/download/..":          "/",
	}
	for path, want := range cases {
		if got := routePath(path); got != want {
			t.Errorf("routePath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestUnknownRoutesAndMethods(t *testing.T) {
	a, _ := newTestApp(t, noUpstream(t))
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

func TestBadLicenseSignatureClearsThroughTheAPI(t *testing.T) {
	a, _ := newTestApp(t, noUpstream(t))
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
	a, _ := newTestApp(t, noUpstream(t))
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
	a, _ := newTestApp(t, noUpstream(t))
	a.routes["/panic"] = route{get: func(http.ResponseWriter, *http.Request) {
		var values []int
		_ = values[1]
	}}
	assertJSONError(t, serve(a, http.MethodGet, "/panic"), http.StatusInternalServerError, "Internal server error")
}
