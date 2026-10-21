package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSignCookieValueMatchesElysia(t *testing.T) {
	vectors := []string{
		`{"expiresAt":1760000000000}.ht79A1OPWRICD1CZcN6eLd6aRRbzf4vysRxfVHy4wmY`,
		`{"expiresAt":1760604800000}.XJsBsY48oxqTWPIArIDZhV79Mr/GaQUw4dqyMbB9XRc`,
		`hello.JW6SvEBoHOrFvg4o+tWpxN5OLeWET1RvW0LkqJyAmZc`,
	}
	for _, vector := range vectors {
		value := vector[:strings.LastIndexByte(vector, '.')]
		if got := signCookieValue(value, []byte(testSecret)); got != vector {
			t.Errorf("signCookieValue(%q) = %q, want %q", value, got, vector)
		}
		if got, ok := unsignCookieValue(vector, []byte(testSecret)); !ok || got != value {
			t.Errorf("unsignCookieValue(%q) = %q, %v", vector, got, ok)
		}
	}
}

func TestUnsignCookieValueRejects(t *testing.T) {
	cases := map[string]string{
		"lenient base64 spelling of the same bytes": "hello.JW6SvEBoHOrFvg4o+tWpxN5OLeWET1RvW0LkqJyAmZd",
		"padding kept":               "hello.JW6SvEBoHOrFvg4o+tWpxN5OLeWET1RvW0LkqJyAmZc=",
		"base64url alphabet":         "hello.JW6SvEBoHOrFvg4o-tWpxN5OLeWET1RvW0LkqJyAmZc",
		"plus turned into a space":   "hello.JW6SvEBoHOrFvg4o tWpxN5OLeWET1RvW0LkqJyAmZc",
		"tampered":                   "hello.JW6SvEBoHOrFvg4o+tWpxN5OLeWET1RvW0LkqJyAmZA",
		"no dot":                     "hello",
		"empty":                      "",
		"signed with another secret": signCookieValue("hello", []byte("another-secret")),
		"value changed":              "hellO.JW6SvEBoHOrFvg4o+tWpxN5OLeWET1RvW0LkqJyAmZc",
	}
	for name, signed := range cases {
		if value, ok := unsignCookieValue(signed, []byte(testSecret)); ok {
			t.Errorf("%s: accepted as %q", name, value)
		}
	}
}

func TestUnsignCookieValueSplitsAtLastDot(t *testing.T) {
	signed := signCookieValue("a.b.c", []byte(testSecret))
	if value, ok := unsignCookieValue(signed, []byte(testSecret)); !ok || value != "a.b.c" {
		t.Fatalf("got %q, %v", value, ok)
	}
}

func TestRequestCookie(t *testing.T) {
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
		value, found := requestCookie(request, licenseCookieName)
		if value != c.value || found != c.found {
			t.Errorf("%s: got %q, %v; want %q, %v", c.name, value, found, c.value, c.found)
		}
	}
}

func TestReadLicenseCookie(t *testing.T) {
	signed := signCookieValue(validCookieRaw, []byte(testSecret))
	encoded := encodeURIComponent(signed)
	cases := []struct {
		name   string
		cookie string
		err    error
		active bool
	}{
		{"absent", "foo=bar", nil, false},
		{"encoded", "license=" + encoded, nil, true},
		{"unencoded", "license=" + signed, nil, true},
		{"double encoded", "license=" + encodeURIComponent(encoded), nil, true},
		{"triple encoded", "license=" + encodeURIComponent(encodeURIComponent(encoded)), errLicenseSignature, false},
		{"quoted", `license="` + encoded + `"`, errLicenseSignature, false},
		{"malformed escape", "license=%E0%A4%A", errLicenseSignature, false},
		{"malformed escape in another cookie", "foo=%E0%A4%A; license=" + encoded, nil, true},
		{"empty", "license=", errLicenseSignature, false},
		{"unsigned", "license=" + encodeURIComponent(validCookieRaw), errLicenseSignature, false},
		{"tampered", "license=" + encoded[:len(encoded)-1] + "A", errLicenseSignature, false},
		{"other secret", "license=" + encodeURIComponent(signCookieValue(validCookieRaw, []byte("another-secret"))), errLicenseSignature, false},
		{"expired", licenseHeader(`{"expiresAt":1000000000000}`), nil, false},
		{"string expiresAt", licenseHeader(`{"expiresAt":"4102444800000"}`), nil, true},
		{"padded string expiresAt", licenseHeader(`{"expiresAt":" 4102444800000 "}`), nil, true},
		{"float expiresAt", licenseHeader(`{"expiresAt":4102444800000.5}`), nil, true},
		{"extra fields", licenseHeader(`{"expiresAt":4102444800000,"steamId":"76561197960000000"}`), nil, true},
		{"spaced JSON", licenseHeader(`{ "expiresAt" : 4102444800000 }`), nil, true},
		{"duplicate key, last wins", licenseHeader(`{"expiresAt":1,"expiresAt":4102444800000}`), nil, true},
		{"non-numeric string", licenseHeader(`{"expiresAt":"soon"}`), errLicensePayload, false},
		{"empty string", licenseHeader(`{"expiresAt":""}`), errLicensePayload, false},
		{"null", licenseHeader(`{"expiresAt":null}`), errLicensePayload, false},
		{"boolean", licenseHeader(`{"expiresAt":true}`), errLicensePayload, false},
		{"out of float range", licenseHeader(`{"expiresAt":1e400}`), errLicensePayload, false},
		{"wrong key case", licenseHeader(`{"EXPIRESAT":4102444800000}`), errLicensePayload, false},
		{"empty object", licenseHeader(`{}`), errLicensePayload, false},
		{"array", licenseHeader(`[4102444800000]`), errLicensePayload, false},
		{"plain string", licenseHeader(`hello`), errLicensePayload, false},
		{"number", licenseHeader(`4102444800000`), errLicensePayload, false},
		{"bad JSON", licenseHeader(`{expiresAt:4102444800000}`), errLicensePayload, false},
		{"signed empty value", licenseHeader(``), errLicensePayload, false},
	}
	now := time.UnixMilli(testNowMs)
	for _, c := range cases {
		request := httptest.NewRequest(http.MethodGet, "/download", nil)
		request.Header.Set("Cookie", c.cookie)
		license, err := readLicenseCookie(request, []byte(testSecret))
		if !errors.Is(err, c.err) {
			t.Errorf("%s: error %v, want %v", c.name, err, c.err)
			continue
		}
		if active := license.activeAt(now); active != c.active {
			t.Errorf("%s: active %v, want %v", c.name, active, c.active)
		}
	}
}

func TestParseLicensePayloadValues(t *testing.T) {
	cases := map[string]float64{
		`{"expiresAt":1760604800000}`:   1760604800000,
		`{"expiresAt":"+.5e3"}`:         500,
		`{"expiresAt":-0}`:              0,
		`{"expiresAt":1e-400}`:          0,
		`{"expiresAt":"00012"}`:         12,
		`{"expiresAt":"1760604800000"}`: 1760604800000,
	}
	for payload, want := range cases {
		got, err := parseLicensePayload(payload)
		if err != nil || got != want {
			t.Errorf("%s: got %v, %v; want %v", payload, got, err, want)
		}
	}
}

func TestIssueLicenseCookie(t *testing.T) {
	got := issueLicenseCookie(time.UnixMilli(testNowMs), []byte(testSecret))
	want := "license=%7B%22expiresAt%22%3A1760604800000%7D.XJsBsY48oxqTWPIArIDZhV79Mr%2FGaQUw4dqyMbB9XRc; Max-Age=604800; Path=/; HttpOnly; Secure; SameSite=Lax"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
