package main

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"syscall"
	"time"
)

const (
	upstreamStepTimeout  = 10 * time.Second
	upstreamTotalTimeout = 20 * time.Second
	maxUpstreamBody      = 1 << 20
	maxUpstreamRedirects = 10
)

var (
	errUpstreamBodyTooLarge = errors.New("upstream response is too large")
	errUpstreamRedirects    = errors.New("too many upstream redirects")
	errUpstreamDowngrade    = errors.New("upstream redirect from https to http")
)

type upstreamResponse struct {
	status int
	body   []byte
}

func newUpstreamClient(stepTimeout, totalTimeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: stepTimeout, KeepAlive: 30 * time.Second}
	return &http.Client{
		Timeout:       totalTimeout,
		CheckRedirect: followUpstreamRedirect,
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
func followUpstreamRedirect(next *http.Request, via []*http.Request) error {
	if len(via) >= maxUpstreamRedirects {
		return errUpstreamRedirects
	}
	if via[0].URL.Scheme == "https" && next.URL.Scheme != "https" {
		return errUpstreamDowngrade
	}
	next.Header.Del("Referer")
	return nil
}

// fetch reads the whole response whatever its status, as fetch().then(res => res.text()) did.
func fetch(client *http.Client, request *http.Request) (upstreamResponse, error) {
	response, err := client.Do(request)
	if err != nil {
		return upstreamResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxUpstreamBody+1))
	if err != nil {
		return upstreamResponse{}, err
	}
	if len(body) > maxUpstreamBody {
		return upstreamResponse{}, errUpstreamBodyTooLarge
	}
	return upstreamResponse{status: response.StatusCode, body: body}, nil
}

// Request errors embed the URL, which carries the Steam API key, so only a kind is logged.
func logUpstreamError(call string, err error) {
	log.Printf("%s: %s", call, errorKind(err))
}

func logUpstreamAnswer(call, problem string, status int) {
	log.Printf("%s: %s (HTTP %d)", call, problem, status)
}

func errorKind(err error) string {
	var netErr net.Error
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var opErr *net.OpError
	switch {
	case errors.Is(err, errUpstreamBodyTooLarge):
		return "response too large"
	case errors.Is(err, errUpstreamRedirects):
		return "too many redirects"
	case errors.Is(err, errUpstreamDowngrade):
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
