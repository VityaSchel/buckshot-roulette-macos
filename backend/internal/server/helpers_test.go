package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/compat"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/config"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/upstream/upstreamtest"
)

const (
	testSecret        = "test-secret-for-parity"
	testPublicURL     = "https://buckshot-roulette-macos.hloth.dev"
	testAPIURL        = "https://buckshot-roulette-macos.hloth.dev/api/"
	testCallbackURL   = testAPIURL + "auth/steam/callback"
	testUpstream      = "http://upstream.test"
	testNowMs         = 1760000000000
	validCookieRaw    = `{"expiresAt":4102444800000}`
	openIDValidAnswer = "ns:http://specs.openid.net/auth/2.0\nis_valid:true\n"
	ownsBuckshot      = `{"response":{"game_count":1,"games":[{"appid":2835570,"playtime_forever":42}]}}`
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	env := map[string]string{
		"STEAM_API_KEY":  "test-steam-key",
		"JWT_SECRET":     testSecret,
		"PUBLIC_URL":     testPublicURL,
		"PUBLIC_API_URL": testAPIURL,
		"DOWNLOADS_DIR":  "/nonexistent",
	}
	cfg, err := config.Load(func(name string) string { return env[name] })
	if err != nil {
		t.Fatal(err)
	}
	cfg.Origins = config.Origins{SteamCommunity: testUpstream, SteamAPI: testUpstream, ItchAPI: testUpstream}
	return cfg
}

func fixedClock() time.Time {
	return time.UnixMilli(testNowMs)
}

func newTestApp(t *testing.T, respond upstreamtest.Respond) (*App, *upstreamtest.Transport) {
	t.Helper()
	client, transport := upstreamtest.NewClient(respond)
	return New(testConfig(t), client, fixedClock), transport
}

func serve(handler http.Handler, method, target string, headers ...string) *httptest.ResponseRecorder {
	return serveBody(handler, method, target, "", headers...)
}

func serveBody(handler http.Handler, method, target, body string, headers ...string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Add(headers[i], headers[i+1])
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// licenseHeader signs like Elysia's signCookie, apart from the license package under test.
func licenseHeader(payload string) string {
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte(payload))
	return "license=" + compat.EncodeURIComponent(payload+"."+base64.RawStdEncoding.EncodeToString(mac.Sum(nil)))
}

func steamCallbackTarget(returnTo string) string {
	const claimedID = "https://steamcommunity.com/openid/id/76561197960010001"
	return "/auth/steam/callback?" + compat.EncodeForm(
		"openid.ns", "http://specs.openid.net/auth/2.0",
		"openid.mode", "id_res",
		"openid.op_endpoint", "https://steamcommunity.com/openid/login",
		"openid.claimed_id", claimedID,
		"openid.identity", claimedID,
		"openid.return_to", returnTo,
		"openid.response_nonce", "2026-10-20T12:00:00Zparity",
		"openid.assoc_handle", "1234567890",
		"openid.signed", "signed,op_endpoint,claimed_id,identity,return_to,response_nonce,assoc_handle",
		"openid.sig", "Kq3+/parity=",
	)
}

func assertRedirect(t *testing.T, recorder *httptest.ResponseRecorder, location string) {
	t.Helper()
	if recorder.Code != http.StatusFound {
		t.Fatalf("status %d, want 302 (body %q)", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Location"); got != location {
		t.Fatalf("Location %q, want %q", got, location)
	}
	if got := recorder.Header().Get("Content-Type"); got != "" {
		t.Fatalf("redirect has Content-Type %q", got)
	}
	if recorder.Body.Len() != 0 || recorder.Header().Get("Content-Length") != "0" {
		t.Fatalf("redirect has a body: %q (Content-Length %q)", recorder.Body.String(), recorder.Header().Get("Content-Length"))
	}
}

func assertJSONError(t *testing.T, recorder *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	want := `{"ok":false,"error":"` + message + `"}`
	if recorder.Code != status || recorder.Body.String() != want {
		t.Fatalf("got %d %q, want %d %q", recorder.Code, recorder.Body.String(), status, want)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type %q", got)
	}
}

func expectHeaders(t *testing.T, header http.Header, want map[string]string) {
	t.Helper()
	for name, value := range want {
		if got := header.Get(name); got != value {
			t.Errorf("%s: %q, want %q", name, got, value)
		}
	}
}
