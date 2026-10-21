package main

import (
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testSecret     = "test-secret-for-parity"
	testPublicURL  = "https://buckshot-roulette-macos.hloth.dev"
	testAPIURL     = "https://buckshot-roulette-macos.hloth.dev/api/"
	testUpstream   = "http://upstream.test"
	testNowMs      = 1760000000000
	farFutureMs    = "4102444800000"
	validCookieRaw = `{"expiresAt":4102444800000}`
)

func testEnv(overrides map[string]string) func(string) string {
	values := map[string]string{
		"STEAM_API_KEY":  "test-steam-key",
		"JWT_SECRET":     testSecret,
		"PUBLIC_URL":     testPublicURL,
		"PUBLIC_API_URL": testAPIURL,
		"DOWNLOADS_DIR":  "/nonexistent",
	}
	maps.Copy(values, overrides)
	return func(name string) string { return values[name] }
}

func testConfig(t *testing.T) config {
	t.Helper()
	cfg, err := loadConfig(testEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	cfg.origins = upstreamOrigins{steamCommunity: testUpstream, steamAPI: testUpstream, itchAPI: testUpstream}
	return cfg
}

func fixedClock(ms int64) func() time.Time {
	return func() time.Time { return time.UnixMilli(ms) }
}

type upstreamCall struct {
	method        string
	path          string
	rawQuery      string
	authorization string
	contentType   string
	body          string
}

type fakeUpstream struct {
	mu      sync.Mutex
	calls   []upstreamCall
	respond func(upstreamCall) (int, string, error)
}

func (f *fakeUpstream) RoundTrip(request *http.Request) (*http.Response, error) {
	var body []byte
	if request.Body != nil {
		body, _ = io.ReadAll(request.Body)
	}
	call := upstreamCall{
		method:        request.Method,
		path:          request.URL.Path,
		rawQuery:      request.URL.RawQuery,
		authorization: request.Header.Get("Authorization"),
		contentType:   request.Header.Get("Content-Type"),
		body:          string(body),
	}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	status, text, err := f.respond(call)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(text)),
		Request:    request,
	}, nil
}

func (f *fakeUpstream) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func newTestApp(t *testing.T, respond func(upstreamCall) (int, string, error)) (*app, *fakeUpstream) {
	t.Helper()
	upstream := &fakeUpstream{respond: respond}
	return newApp(testConfig(t), &http.Client{Transport: upstream}, fixedClock(testNowMs)), upstream
}

func noUpstream(t *testing.T) func(upstreamCall) (int, string, error) {
	return func(call upstreamCall) (int, string, error) {
		t.Errorf("unexpected upstream call %s %s", call.method, call.path)
		return http.StatusInternalServerError, "", nil
	}
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

func licenseHeader(payload string) string {
	return "license=" + encodeURIComponent(signCookieValue(payload, []byte(testSecret)))
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
