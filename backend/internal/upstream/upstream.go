package upstream

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

const (
	StepTimeout  = 10 * time.Second
	TotalTimeout = 20 * time.Second
	maxBody      = 1 << 20
	maxRedirects = 10
)

var (
	ErrRefused      = errors.New("upstream answer refused")
	errBodyTooLarge = errors.New("upstream response is too large")
	errRedirects    = errors.New("too many upstream redirects")
	errDowngrade    = errors.New("upstream redirect from https to http")
)

type Request struct {
	// Name stands for the request in logs: the URL can hold the Steam API key.
	Name          string
	Method        string
	URL           string
	Authorization string
	Form          string
	OKOnly        bool
	Refusal       string
}

func NewClient(stepTimeout, totalTimeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: stepTimeout, KeepAlive: 30 * time.Second}
	return &http.Client{
		Timeout:       totalTimeout,
		CheckRedirect: followRedirect,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   stepTimeout,
			ResponseHeaderTimeout: stepTimeout,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          16,
			IdleConnTimeout:       90 * time.Second,
		},
	}
}

// Go's default policy would send the previous URL, the Steam API key in it, as Referer, and would
// resend Authorization in the clear on a redirect to http on the same host.
func followRedirect(next *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errRedirects
	}
	if via[0].URL.Scheme == "https" && next.URL.Scheme != "https" {
		return errDowngrade
	}
	next.Header.Del("Referer")
	return nil
}

func Fetch[T any](ctx context.Context, client *http.Client, request Request, read func(body []byte) (T, bool)) (T, error) {
	var zero T
	status, body, err := fetch(ctx, client, request)
	if err != nil {
		log.Printf("%s: %s", request.Name, errorKind(err))
		return zero, err
	}
	value, ok := read(body)
	if !ok || request.OKOnly && status != http.StatusOK {
		log.Printf("%s: %s (HTTP %d)", request.Name, cmp.Or(request.Refusal, "unexpected answer"), status)
		return zero, ErrRefused
	}
	return value, nil
}

// fetch reads the whole response whatever its status, as fetch().then(res => res.text()) did.
func fetch(ctx context.Context, client *http.Client, request Request) (int, []byte, error) {
	var form io.Reader
	if request.Form != "" {
		form = strings.NewReader(request.Form)
	}
	outgoing, err := http.NewRequestWithContext(ctx, request.Method, request.URL, form)
	if err != nil {
		return 0, nil, err
	}
	if request.Form != "" {
		outgoing.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if request.Authorization != "" {
		outgoing.Header.Set("Authorization", request.Authorization)
	}
	response, err := client.Do(outgoing)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		return 0, nil, err
	}
	if len(body) > maxBody {
		return 0, nil, errBodyTooLarge
	}
	return response.StatusCode, body, nil
}

// Request errors embed the URL, which carries the Steam API key, so only a kind is logged.
func errorKind(err error) string {
	var netErr net.Error
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var opErr *net.OpError
	switch {
	case errors.Is(err, errBodyTooLarge):
		return "response too large"
	case errors.Is(err, errRedirects):
		return "too many redirects"
	case errors.Is(err, errDowngrade):
		return "redirect to http refused"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "timeout"
	case errors.As(err, &dnsErr):
		return "DNS lookup failed"
	case errors.As(err, &certErr):
		return "TLS certificate rejected"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "connection closed"
	case errors.As(err, &opErr):
		return opErr.Op + " failed"
	}
	return "request failed"
}
