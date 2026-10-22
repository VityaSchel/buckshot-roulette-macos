package upstream

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"
	"time"
)

func TestErrorKind(t *testing.T) {
	wrapped := func(op string, err error) error {
		return &url.Error{Op: "Get", URL: "https://api.steampowered.com/?key=never-logged", Err: &net.OpError{Op: op, Net: "tcp", Err: err}}
	}
	cases := map[string]error{
		"response too large":       errBodyTooLarge,
		"too many redirects":       &url.Error{Op: "Get", URL: "https://x/?key=never-logged", Err: errRedirects},
		"redirect to http refused": &url.Error{Op: "Get", URL: "http://x/?key=never-logged", Err: errDowngrade},
		"canceled":                 &url.Error{Op: "Get", URL: "https://x/?key=never-logged", Err: context.Canceled},
		"timeout":                  wrapped("read", os.ErrDeadlineExceeded),
		"DNS lookup failed":        wrapped("dial", &net.DNSError{Err: "no such host", Name: "api.itch.io"}),
		"TLS certificate rejected": &url.Error{Op: "Get", URL: "https://x", Err: &tls.CertificateVerificationError{Err: errors.New("x509")}},
		"connection refused":       wrapped("dial", &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}),
		"connection closed":        &url.Error{Op: "Get", URL: "https://x", Err: io.ErrUnexpectedEOF},
		"write failed":             wrapped("write", errors.New("broken pipe")),
		"request failed":           errors.New("something else"),
	}
	for want, err := range cases {
		got := errorKind(err)
		if got != want {
			t.Errorf("errorKind(%v) = %q, want %q", err, got, want)
		}
		if got == fmt.Sprint(err) {
			t.Errorf("errorKind returned the error text itself")
		}
	}
}

func TestRedirectPolicy(t *testing.T) {
	request := func(target string) *http.Request {
		r, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	next := request("https://api.itch.io/next")
	next.Header.Set("Referer", "https://api.itch.io/profile")
	if err := followRedirect(next, []*http.Request{request("https://api.itch.io/profile")}); err != nil || next.Header.Get("Referer") != "" {
		t.Fatalf("https to https: %v, Referer %q", err, next.Header.Get("Referer"))
	}
	if err := followRedirect(request("http://api.itch.io/next"), []*http.Request{request("https://api.itch.io/profile")}); !errors.Is(err, errDowngrade) {
		t.Fatalf("https to http: %v", err)
	}
	via := make([]*http.Request, maxRedirects)
	for i := range via {
		via[i] = request("https://api.itch.io/profile")
	}
	if err := followRedirect(request("https://api.itch.io/next"), via); !errors.Is(err, errRedirects) {
		t.Fatalf("redirect %d: %v", len(via)+1, err)
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient(time.Second, 2*time.Second)
	transport := client.Transport.(*http.Transport)
	if client.Timeout != 2*time.Second || transport.ResponseHeaderTimeout != time.Second || transport.TLSHandshakeTimeout != time.Second {
		t.Fatalf("timeouts: total %s, header %s, TLS %s", client.Timeout, transport.ResponseHeaderTimeout, transport.TLSHandshakeTimeout)
	}
	if client.CheckRedirect == nil {
		t.Fatal("the default redirect policy leaks the Steam API key as Referer")
	}
}

type answer struct {
	status  int
	body    string
	err     error
	bodyErr error
}

type fakeTransport struct {
	answer
	received *http.Request
	form     string
}

func (f *fakeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	f.received = request
	if request.Body != nil {
		form, _ := io.ReadAll(request.Body)
		f.form = string(form)
	}
	if f.err != nil {
		return nil, f.err
	}
	var body io.Reader = strings.NewReader(f.body)
	if f.bodyErr != nil {
		body = io.MultiReader(body, iotest.ErrReader(f.bodyErr))
	}
	return &http.Response{StatusCode: f.status, Header: http.Header{}, Body: io.NopCloser(body), Request: request}, nil
}

func captureLog(t *testing.T) *bytes.Buffer {
	var logged bytes.Buffer
	log.SetOutput(&logged)
	flags := log.Flags()
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(flags)
	})
	return &logged
}

func TestFetch(t *testing.T) {
	const keyURL = "https://api.steampowered.com/?key=secret-key"
	plain := Request{Name: "call", Method: http.MethodGet, URL: keyURL}
	okOnly := Request{Name: "call", Method: http.MethodGet, URL: keyURL, OKOnly: true, Refusal: "not confirmed"}
	badURL := Request{Name: "call", Method: http.MethodGet, URL: "http://[::1"}
	digits := func(body []byte) (string, bool) { return string(body), strings.Trim(string(body), "0123456789") == "" }
	cases := []struct {
		name    string
		answer  answer
		request Request
		value   string
		err     error
		logged  string
	}{
		{"accepted", answer{status: 200, body: "42"}, plain, "42", nil, ""},
		{"status ignored", answer{status: 500, body: "42"}, plain, "42", nil, ""},
		{"refused", answer{status: 200, body: "x"}, plain, "", ErrRefused, "call: unexpected answer (HTTP 200)\n"},
		{"OK only", answer{status: 500, body: "42"}, okOnly, "", ErrRefused, "call: not confirmed (HTTP 500)\n"},
		{"failed", answer{err: syscall.ECONNREFUSED}, plain, "", syscall.ECONNREFUSED, "call: connection refused\n"},
		{"body cut short", answer{status: 200, body: "4", bodyErr: io.ErrUnexpectedEOF}, plain, "", io.ErrUnexpectedEOF, "call: connection closed\n"},
		{"too large", answer{status: 200, body: strings.Repeat("1", maxBody+1)}, plain, "", errBodyTooLarge, "call: response too large\n"},
		{"largest", answer{status: 200, body: strings.Repeat("1", maxBody)}, plain, strings.Repeat("1", maxBody), nil, ""},
		{"bad URL", answer{}, badURL, "", nil, "call: request failed\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logged := captureLog(t)
			client := &http.Client{Transport: &fakeTransport{answer: c.answer}}
			value, err := Fetch(context.Background(), client, c.request, digits)
			if value != c.value || (err == nil) != (c.logged == "") || c.err != nil && !errors.Is(err, c.err) {
				t.Errorf("got %.10q, %v; want %.10q, %v", value, err, c.value, c.err)
			}
			if logged.String() != c.logged {
				t.Errorf("logged %q, want %q", logged.String(), c.logged)
			}
		})
	}
}

func TestFetchSendsTheFormAndAuthorization(t *testing.T) {
	transport := &fakeTransport{status: 200}
	client := &http.Client{Transport: transport}
	accept := func([]byte) (bool, bool) { return true, true }
	if _, err := Fetch(context.Background(), client, Request{Name: "form", Method: http.MethodPost, URL: "https://steamcommunity.com/openid/login", Form: "a=1&b=2"}, accept); err != nil {
		t.Fatal(err)
	}
	if got := transport.received.Header; got.Get("Content-Type") != "application/x-www-form-urlencoded" || got.Get("Authorization") != "" || transport.form != "a=1&b=2" {
		t.Errorf("form request: %v %q", got, transport.form)
	}
	if _, err := Fetch(context.Background(), client, Request{Name: "bearer", Method: http.MethodGet, URL: "https://api.itch.io/profile", Authorization: "Bearer x"}, accept); err != nil {
		t.Fatal(err)
	}
	if got := transport.received.Header; got.Get("Content-Type") != "" || got.Get("Authorization") != "Bearer x" || transport.received.Body != nil {
		t.Errorf("bearer request: %v", got)
	}
}
