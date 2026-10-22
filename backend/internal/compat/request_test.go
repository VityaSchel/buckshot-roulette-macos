package compat

import (
	"maps"
	"net/http"
	"net/http/httptest"
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
		"/auth/%2E./logout":     "/logout",
		"/auth/steam/.":         "/auth/steam",
		"/auth/steam/./":        "/auth/steam",
		"/auth/steam/%2e":       "/auth/steam",
		"/auth/steam/%2E/x":     "/auth/steam/x",
		"/../../download":       "/download",
		"/auth/steam/callback/": "/auth/steam/callback",
		"/d%6Fwnload":           "/d%6Fwnload",
		"/download/..":          "/",
		"/download/...":         "/download/...",
		"/download/%2e%2e%2e":   "/download/%2e%2e%2e",
	}
	for path, want := range cases {
		if got := RoutePath(path); got != want {
			t.Errorf("RoutePath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestCookie(t *testing.T) {
	cases := []struct {
		name    string
		headers []string
		value   string
		found   bool
	}{
		{"absent", nil, "", false},
		{"empty header", []string{""}, "", false},
		{"only another cookie", []string{"foo=bar"}, "", false},
		{"other case", []string{"License=x"}, "", false},
		{"empty value counts", []string{"license="}, "", true},
		{"first occurrence wins", []string{"license=a; license=b"}, "a", true},
		{"among others", []string{"a=1; license=x; b=2"}, "x", true},
		{"spaces and tabs trimmed", []string{" \tlicense = x \t;"}, "x", true},
		{"pair without equals skipped", []string{"license; license=y"}, "y", true},
		{"value keeps equals signs", []string{"license=a=b"}, "a=b", true},
		{"quotes kept", []string{`license="x"`}, `"x"`, true},
		{"plus kept", []string{"license=a+b"}, "a+b", true},
		{"headers joined with semicolons", []string{"foo=bar", "license=x"}, "x", true},
		{"first header wins", []string{"license=a", "license=b"}, "a", true},
	}
	for _, c := range cases {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, header := range c.headers {
			request.Header.Add("Cookie", header)
		}
		value, found := Cookie(request, "license")
		if value != c.value || found != c.found {
			t.Errorf("%s: got %q, %v; want %q, %v", c.name, value, found, c.value, c.found)
		}
	}
}

func TestMediaType(t *testing.T) {
	cases := []struct {
		header    string
		mediaType string
		params    map[string]string
	}{
		{"application/json", "application/json", map[string]string{}},
		{"Application/JSON", "application/json", map[string]string{}},
		{"multipart/form-data; boundary=b", "multipart/form-data", map[string]string{"boundary": "b"}},
		{"multipart/form-data; boundary=b; charset", "multipart/form-data", map[string]string{"boundary": "b"}},
		{"application/json; charset", "application/json", map[string]string{}},
		{"application/json, text/plain", "application/json", map[string]string{}},
		{"text/plain, application/json", "text/plain", map[string]string{}},
		{"application/json garbage", "application/json", map[string]string{}},
		{" \tapplication/json;charset=utf-8;broken", "application/json", map[string]string{"charset": "utf-8"}},
		{"multipart/form-data; boundary=a; boundary=b", "multipart/form-data", map[string]string{"boundary": "a"}},
		{"", "", map[string]string{}},
	}
	for _, c := range cases {
		mediaType, params := MediaType(c.header)
		if mediaType != c.mediaType || !maps.Equal(params, c.params) {
			t.Errorf("MediaType(%q) = %q, %v; want %q, %v", c.header, mediaType, params, c.mediaType, c.params)
		}
	}
}
