package httpx

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAnswers(t *testing.T) {
	cases := []struct {
		name   string
		write  func(http.ResponseWriter)
		status int
		header map[string]string
		body   string
	}{
		{"redirect", func(w http.ResponseWriter) { Redirect(w, "/x") }, http.StatusFound, map[string]string{"Location": "/x", "Content-Length": "0", "Content-Type": ""}, ""},
		{"error", func(w http.ResponseWriter) { Error(w, http.StatusNotFound, "Not <found>") }, http.StatusNotFound, map[string]string{"Content-Type": "application/json", "Content-Length": "44"}, `{"ok":false,"error":"Not \u003cfound\u003e"}`},
		{"server error", ServerError, http.StatusInternalServerError, map[string]string{"Content-Type": "application/json"}, `{"ok":false,"error":"Internal server error"}`},
		{"body", func(w http.ResponseWriter) { WriteBody(w, http.StatusOK, "text/html;charset=utf-8", []byte("<p>")) }, http.StatusOK, map[string]string{"Content-Type": "text/html;charset=utf-8", "Content-Length": "3"}, "<p>"},
	}
	for _, c := range cases {
		recorder := httptest.NewRecorder()
		c.write(recorder)
		if recorder.Code != c.status || recorder.Body.String() != c.body {
			t.Errorf("%s: %d %q, want %d %q", c.name, recorder.Code, recorder.Body.String(), c.status, c.body)
		}
		for name, want := range c.header {
			if got := recorder.Header().Get(name); got != want {
				t.Errorf("%s: %s %q, want %q", c.name, name, got, want)
			}
		}
	}
}

func TestRecover(t *testing.T) {
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	cases := map[string]struct {
		handler func()
		logged  string
	}{
		"runtime error": {func() { var values []int; _ = values[1] }, "handler panic: runtime error: index out of range [1] with length 0"},
		"other value":   {func() { panic("secret") }, "handler panic"},
	}
	for name, c := range cases {
		logged.Reset()
		recorder := httptest.NewRecorder()
		func() {
			defer Recover(recorder)
			c.handler()
		}()
		if recorder.Code != http.StatusInternalServerError || recorder.Body.String() != `{"ok":false,"error":"Internal server error"}` {
			t.Errorf("%s: %d %q", name, recorder.Code, recorder.Body.String())
		}
		if !strings.HasSuffix(logged.String(), c.logged+"\n") {
			t.Errorf("%s: logged %q", name, logged.String())
		}
	}
	func() {
		defer func() {
			if recovered := recover(); recovered != http.ErrAbortHandler {
				t.Errorf("ErrAbortHandler became %v", recovered)
			}
		}()
		defer Recover(httptest.NewRecorder())
		panic(http.ErrAbortHandler)
	}()
	recorder := httptest.NewRecorder()
	func() { defer Recover(recorder) }()
	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
		t.Errorf("no panic: %d %q", recorder.Code, recorder.Body.String())
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestReadBody(t *testing.T) {
	cases := []struct {
		name string
		body string
		err  error
	}{
		{"fits", "token=a", nil},
		{"exactly the limit", strings.Repeat("a", 16), nil},
		{"too large", strings.Repeat("a", 17), ErrBodyTooLarge},
	}
	for _, c := range cases {
		recorder := httptest.NewRecorder()
		GuardBody(recorder, time.Second)
		body, err := ReadBody(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(c.body)), 16)
		if !errors.Is(err, c.err) || err == nil && string(body) != c.body {
			t.Errorf("%s: %q, %v; want %v", c.name, body, err, c.err)
		}
		if released := recorder.Header().Get("Connection") == ""; released != (err == nil) {
			t.Errorf("%s: Connection %q after %v", c.name, recorder.Header().Get("Connection"), err)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/", failingReader{})
	if _, err := ReadBody(httptest.NewRecorder(), request, 16); !errors.Is(err, ErrBodyRead) {
		t.Errorf("failing body: %v", err)
	}
}

func TestDropUnreadBodyOnlyAfterGuard(t *testing.T) {
	recorder := httptest.NewRecorder()
	DropUnreadBody(recorder)
	GuardBody(recorder, time.Second)
	DropUnreadBody(recorder)
	if recorder.Header().Get("Connection") != "close" {
		t.Fatalf("Connection %q", recorder.Header().Get("Connection"))
	}
}
