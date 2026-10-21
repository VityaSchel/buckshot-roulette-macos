package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"
)

func TestErrorKind(t *testing.T) {
	wrapped := func(op string, err error) error {
		return &url.Error{Op: "Get", URL: "https://api.steampowered.com/?key=never-logged", Err: &net.OpError{Op: op, Net: "tcp", Err: err}}
	}
	cases := map[string]error{
		"response too large":       errUpstreamBodyTooLarge,
		"too many redirects":       &url.Error{Op: "Get", URL: "https://x/?key=never-logged", Err: errUpstreamRedirects},
		"redirect to http refused": &url.Error{Op: "Get", URL: "http://x/?key=never-logged", Err: errUpstreamDowngrade},
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
