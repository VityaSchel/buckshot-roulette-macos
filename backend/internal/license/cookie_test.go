package license

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/compat"
)

const (
	testSecret     = "test-secret-for-parity"
	testNowMs      = 1760000000000
	validCookieRaw = `{"expiresAt":4102444800000}`
)

func licenseHeader(payload string) string {
	return "license=" + compat.EncodeURIComponent(sign(payload, []byte(testSecret)))
}

func TestSignMatchesElysia(t *testing.T) {
	vectors := []string{
		`{"expiresAt":1760000000000}.ht79A1OPWRICD1CZcN6eLd6aRRbzf4vysRxfVHy4wmY`,
		`{"expiresAt":1760604800000}.XJsBsY48oxqTWPIArIDZhV79Mr/GaQUw4dqyMbB9XRc`,
		`hello.JW6SvEBoHOrFvg4o+tWpxN5OLeWET1RvW0LkqJyAmZc`,
	}
	for _, vector := range vectors {
		value := vector[:strings.LastIndexByte(vector, '.')]
		if got := sign(value, []byte(testSecret)); got != vector {
			t.Errorf("sign(%q) = %q, want %q", value, got, vector)
		}
		if got, ok := unsign(vector, []byte(testSecret)); !ok || got != value {
			t.Errorf("unsign(%q) = %q, %v", vector, got, ok)
		}
	}
}

func TestUnsignRejects(t *testing.T) {
	cases := map[string]string{
		"lenient base64 spelling of the same bytes": "hello.JW6SvEBoHOrFvg4o+tWpxN5OLeWET1RvW0LkqJyAmZd",
		"padding kept":               "hello.JW6SvEBoHOrFvg4o+tWpxN5OLeWET1RvW0LkqJyAmZc=",
		"base64url alphabet":         "hello.JW6SvEBoHOrFvg4o-tWpxN5OLeWET1RvW0LkqJyAmZc",
		"plus turned into a space":   "hello.JW6SvEBoHOrFvg4o tWpxN5OLeWET1RvW0LkqJyAmZc",
		"tampered":                   "hello.JW6SvEBoHOrFvg4o+tWpxN5OLeWET1RvW0LkqJyAmZA",
		"no dot":                     "hello",
		"empty":                      "",
		"signed with another secret": sign("hello", []byte("another-secret")),
		"value changed":              "hellO.JW6SvEBoHOrFvg4o+tWpxN5OLeWET1RvW0LkqJyAmZc",
	}
	for name, signed := range cases {
		if value, ok := unsign(signed, []byte(testSecret)); ok {
			t.Errorf("%s: accepted as %q", name, value)
		}
	}
}

func TestUnsignSplitsAtLastDot(t *testing.T) {
	signed := sign("a.b.c", []byte(testSecret))
	if value, ok := unsign(signed, []byte(testSecret)); !ok || value != "a.b.c" {
		t.Fatalf("got %q, %v", value, ok)
	}
}

func TestRead(t *testing.T) {
	signed := sign(validCookieRaw, []byte(testSecret))
	encoded := compat.EncodeURIComponent(signed)
	cases := []struct {
		name   string
		cookie string
		err    error
		active bool
	}{
		{"absent", "foo=bar", nil, false},
		{"encoded", "license=" + encoded, nil, true},
		{"unencoded", "license=" + signed, nil, true},
		{"double encoded", "license=" + compat.EncodeURIComponent(encoded), nil, true},
		{"triple encoded", "license=" + compat.EncodeURIComponent(compat.EncodeURIComponent(encoded)), ErrSignature, false},
		{"quoted", `license="` + encoded + `"`, ErrSignature, false},
		{"malformed escape", "license=%E0%A4%A", ErrSignature, false},
		{"malformed escape in another cookie", "foo=%E0%A4%A; license=" + encoded, nil, true},
		{"empty", "license=", ErrSignature, false},
		{"unsigned", "license=" + compat.EncodeURIComponent(validCookieRaw), ErrSignature, false},
		{"tampered", "license=" + encoded[:len(encoded)-1] + "A", ErrSignature, false},
		{"other secret", "license=" + compat.EncodeURIComponent(sign(validCookieRaw, []byte("another-secret"))), ErrSignature, false},
		{"expired", licenseHeader(`{"expiresAt":1000000000000}`), nil, false},
		{"string expiresAt", licenseHeader(`{"expiresAt":"4102444800000"}`), nil, true},
		{"padded string expiresAt", licenseHeader(`{"expiresAt":" 4102444800000 "}`), nil, true},
		{"float expiresAt", licenseHeader(`{"expiresAt":4102444800000.5}`), nil, true},
		{"extra fields", licenseHeader(`{"expiresAt":4102444800000,"steamId":"76561197960000000"}`), nil, true},
		{"spaced JSON", licenseHeader(`{ "expiresAt" : 4102444800000 }`), nil, true},
		{"duplicate key, last wins", licenseHeader(`{"expiresAt":1,"expiresAt":4102444800000}`), nil, true},
		{"non-numeric string", licenseHeader(`{"expiresAt":"soon"}`), ErrPayload, false},
		{"empty string", licenseHeader(`{"expiresAt":""}`), ErrPayload, false},
		{"null", licenseHeader(`{"expiresAt":null}`), ErrPayload, false},
		{"boolean", licenseHeader(`{"expiresAt":true}`), ErrPayload, false},
		{"out of float range", licenseHeader(`{"expiresAt":1e400}`), ErrPayload, false},
		{"wrong key case", licenseHeader(`{"EXPIRESAT":4102444800000}`), ErrPayload, false},
		{"empty object", licenseHeader(`{}`), ErrPayload, false},
		{"array", licenseHeader(`[4102444800000]`), ErrPayload, false},
		{"plain string", licenseHeader(`hello`), ErrPayload, false},
		{"number", licenseHeader(`4102444800000`), ErrPayload, false},
		{"bad JSON", licenseHeader(`{expiresAt:4102444800000}`), ErrPayload, false},
		{"signed empty value", licenseHeader(``), ErrPayload, false},
	}
	now := time.UnixMilli(testNowMs)
	for _, c := range cases {
		request := httptest.NewRequest(http.MethodGet, "/download", nil)
		request.Header.Set("Cookie", c.cookie)
		cookie, err := Read(request, []byte(testSecret))
		if !errors.Is(err, c.err) {
			t.Errorf("%s: error %v, want %v", c.name, err, c.err)
			continue
		}
		if active := cookie.ActiveAt(now); active != c.active {
			t.Errorf("%s: active %v, want %v", c.name, active, c.active)
		}
	}
}

func TestSent(t *testing.T) {
	cases := map[string]bool{"": false, "foo=bar": false, "license=": true, "license=%E0%A4%A": true, "a=1; license=x": true}
	for cookie, want := range cases {
		request := httptest.NewRequest(http.MethodGet, "/logout", nil)
		request.Header.Set("Cookie", cookie)
		if got := Sent(request); got != want {
			t.Errorf("Sent(%q) = %v, want %v", cookie, got, want)
		}
	}
}

func TestParsePayloadValues(t *testing.T) {
	cases := map[string]float64{
		`{"expiresAt":1760604800000}`:   1760604800000,
		`{"expiresAt":"+.5e3"}`:         500,
		`{"expiresAt":-0}`:              0,
		`{"expiresAt":1e-400}`:          0,
		`{"expiresAt":"00012"}`:         12,
		`{"expiresAt":"1760604800000"}`: 1760604800000,
	}
	for payload, want := range cases {
		got, err := parsePayload(payload)
		if err != nil || got != want {
			t.Errorf("%s: got %v, %v; want %v", payload, got, err, want)
		}
	}
}

func TestIssue(t *testing.T) {
	got := Issue(time.UnixMilli(testNowMs), []byte(testSecret))
	want := "license=%7B%22expiresAt%22%3A1760604800000%7D.XJsBsY48oxqTWPIArIDZhV79Mr%2FGaQUw4dqyMbB9XRc; Max-Age=604800; Path=/; HttpOnly; Secure; SameSite=Lax"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if ClearCookie != "license=; Max-Age=0; Path=/; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Secure; SameSite=Lax" {
		t.Fatalf("ClearCookie %q", ClearCookie)
	}
}
