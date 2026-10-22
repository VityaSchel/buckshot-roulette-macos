package upstreamtest

import (
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
)

type Call struct {
	Method        string
	Path          string
	RawQuery      string
	Authorization string
	ContentType   string
	Body          string
}

type Respond func(Call) (status int, body string, err error)

type Transport struct {
	mu      sync.Mutex
	calls   []Call
	respond Respond
}

func NewClient(respond Respond) (*http.Client, *Transport) {
	transport := &Transport{respond: respond}
	return &http.Client{Transport: transport}, transport
}

func (t *Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	var body []byte
	if request.Body != nil {
		body, _ = io.ReadAll(request.Body)
	}
	call := Call{
		Method:        request.Method,
		Path:          request.URL.Path,
		RawQuery:      request.URL.RawQuery,
		Authorization: request.Header.Get("Authorization"),
		ContentType:   request.Header.Get("Content-Type"),
		Body:          string(body),
	}
	t.mu.Lock()
	t.calls = append(t.calls, call)
	t.mu.Unlock()
	status, text, err := t.respond(call)
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

func (t *Transport) Calls() []Call {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.calls)
}

func Unused(t testing.TB) Respond {
	return func(call Call) (int, string, error) {
		t.Errorf("unexpected upstream call %s %s", call.Method, call.Path)
		return http.StatusInternalServerError, "", nil
	}
}
