package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/steam"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/upstream"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/upstream/upstreamtest"
)

type rawAnswer struct {
	status string
	header http.Header
	body   string
	closes bool
}

// Test servers stay on 127.0.0.1:39560-39599, out of the parity harness's ports.
func localListener(t *testing.T) net.Listener {
	t.Helper()
	for port := 39560; port <= 39599; port++ {
		listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err == nil {
			t.Cleanup(func() { listener.Close() })
			return listener
		}
	}
	t.Fatal("no free port in 127.0.0.1:39560-39599")
	return nil
}

func startServer(t *testing.T, config *http.Server, listener net.Listener) *httptest.Server {
	t.Helper()
	server := &httptest.Server{Listener: listener, Config: config}
	server.Start()
	t.Cleanup(server.Close)
	return server
}

// servedApp runs the app behind a real listener, with its upstream calls going to upstream through the production client.
func servedApp(t *testing.T, upstreamHandler http.Handler) (*App, *httptest.Server) {
	t.Helper()
	cfg := testConfig(t)
	if upstreamHandler != nil {
		upstreamServer := startServer(t, &http.Server{Handler: upstreamHandler}, localListener(t))
		cfg.Origins.SteamCommunity, cfg.Origins.SteamAPI, cfg.Origins.ItchAPI = upstreamServer.URL, upstreamServer.URL, upstreamServer.URL
	}
	a := New(cfg, upstream.NewClient(2*time.Second, 4*time.Second), fixedClock)
	return a, startServer(t, newHTTPServer(a), localListener(t))
}

func dial(t *testing.T, server *httptest.Server) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, bufio.NewReader(conn)
}

func readAnswer(t *testing.T, conn net.Conn, reader *bufio.Reader, within time.Duration) rawAnswer {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(within))
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("no answer within %s: %v", within, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("answer body: %v", err)
	}
	return rawAnswer{status: response.Status, header: response.Header, body: string(body), closes: response.Close}
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func expectClosed(t *testing.T, conn net.Conn, reader *bufio.Reader, within time.Duration) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(within))
	_, err := reader.ReadByte()
	switch {
	case isTimeout(err):
		t.Fatalf("the connection is still open after %s", within)
	case err == nil:
		t.Fatal("unexpected bytes after the answer")
	}
}

func TestUnreadBodyClosesTheConnectionAfterTheAnswer(t *testing.T) {
	_, server := servedApp(t, nil)
	requests := map[string]string{
		"unknown route":        "GET /nope HTTP/1.1\r\nHost: t\r\nContent-Length: 100\r\n\r\n",
		"logout":               "GET /logout HTTP/1.1\r\nHost: t\r\nContent-Length: 100\r\n\r\n",
		"POST /download":       "POST /download HTTP/1.1\r\nHost: t\r\nContent-Length: 100\r\n\r\n",
		"chunked, no chunk":    "GET /nope HTTP/1.1\r\nHost: t\r\nTransfer-Encoding: chunked\r\n\r\n",
		"itch.io text/plain":   "POST /auth/itch.io/callback HTTP/1.1\r\nHost: t\r\nContent-Type: text/plain\r\nContent-Length: 100\r\n\r\n",
		"itch.io, bad cookie":  "POST /auth/itch.io/callback HTTP/1.1\r\nHost: t\r\nContent-Type: application/x-www-form-urlencoded\r\nCookie: license=x.y\r\nContent-Length: 100\r\n\r\n",
		"body sent, unwanted":  "GET /logout HTTP/1.1\r\nHost: t\r\nContent-Length: 5\r\n\r\nhello",
		"expects 100-continue": "POST /download HTTP/1.1\r\nHost: t\r\nExpect: 100-continue\r\nContent-Length: 100\r\n\r\n",
	}
	for name, request := range requests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			conn, reader := dial(t, server)
			conn.Write([]byte(request))
			answer := readAnswer(t, conn, reader, 2*time.Second)
			if !answer.closes {
				t.Errorf("%s without Connection: close", answer.status)
			}
			expectClosed(t, conn, reader, 2*time.Second)
		})
	}
}

func TestStalledItchBodyAnswersAfterTheBodyTimeout(t *testing.T) {
	a, server := servedApp(t, nil)
	a.bodyTimeout = 200 * time.Millisecond
	conn, reader := dial(t, server)
	started := time.Now()
	conn.Write([]byte("POST /auth/itch.io/callback HTTP/1.1\r\nHost: t\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: 100\r\n\r\ntoken="))
	answer := readAnswer(t, conn, reader, 2*time.Second)
	if answer.status != "500 Internal Server Error" || answer.body != `{"ok":false,"error":"Internal server error"}` {
		t.Fatalf("got %s %q", answer.status, answer.body)
	}
	if waited := time.Since(started); waited < a.bodyTimeout {
		t.Fatalf("answered after %s, before the body timeout", waited)
	}
	expectClosed(t, conn, reader, time.Second)
}

func itchHandler(t *testing.T, delay time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/profile":
			io.WriteString(w, `{"user":{"id":1001}}`)
		case "/profile/owned-keys":
			select {
			case <-time.After(delay):
				io.WriteString(w, `{"owned_keys":[{"game_id":2443623}]}`)
			case <-r.Context().Done():
			}
		default:
			t.Errorf("unexpected upstream path %s", r.URL.Path)
		}
	}
}

const itchFormRequest = "POST /auth/itch.io/callback HTTP/1.1\r\nHost: t\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: 11\r\n\r\ntoken=owner"

func TestReadBodyKeepsTheConnectionAlive(t *testing.T) {
	_, server := servedApp(t, itchHandler(t, 0))
	conn, reader := dial(t, server)
	conn.Write([]byte(itchFormRequest))
	answer := readAnswer(t, conn, reader, 2*time.Second)
	if answer.header.Get("Location") != testPublicURL+"/#success" || answer.closes {
		t.Fatalf("got %s %v", answer.status, answer.header)
	}
	conn.Write([]byte("GET /logout HTTP/1.1\r\nHost: t\r\n\r\n"))
	if answer := readAnswer(t, conn, reader, 2*time.Second); answer.header.Get("Location") != testPublicURL+"/#try-again" {
		t.Fatalf("second request on the connection: %s %v", answer.status, answer.header)
	}
}

func TestLicenseCheckOutlivesTheBodyDeadline(t *testing.T) {
	a, server := servedApp(t, itchHandler(t, 500*time.Millisecond))
	a.bodyTimeout = 100 * time.Millisecond
	conn, reader := dial(t, server)
	conn.Write([]byte(itchFormRequest))
	if answer := readAnswer(t, conn, reader, 3*time.Second); answer.header.Get("Location") != testPublicURL+"/#success" {
		t.Fatalf("got %s %v", answer.status, answer.header)
	}
}

func TestClientDisconnectCancelsTheUpstreamCall(t *testing.T) {
	cases := map[string]struct {
		path    string
		request string
	}{
		"Steam check_authentication": {"/openid/login", "GET " + steamCallbackTarget(testCallbackURL) + " HTTP/1.1\r\nHost: t\r\n\r\n"},
		"itch.io owned-keys":         {"/profile/owned-keys", itchFormRequest},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			arrived := make(chan struct{}, 1)
			canceled := make(chan struct{}, 1)
			_, server := servedApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != c.path {
					io.WriteString(w, `{"user":{"id":1001}}`)
					return
				}
				// net/http notices a closed connection only once the request body has been read.
				io.ReadAll(r.Body)
				arrived <- struct{}{}
				select {
				case <-r.Context().Done():
					canceled <- struct{}{}
				case <-time.After(5 * time.Second):
				}
			}))
			conn, _ := dial(t, server)
			conn.Write([]byte(c.request))
			select {
			case <-arrived:
			case <-time.After(2 * time.Second):
				t.Fatal("the upstream call never arrived")
			}
			conn.Close()
			select {
			case <-canceled:
			case <-time.After(2 * time.Second):
				t.Fatal("the upstream call went on after the client left")
			}
		})
	}
}

func doWithoutRedirects(t *testing.T, method, target, form string) *http.Response {
	t.Helper()
	var body io.Reader = http.NoBody
	if method == http.MethodPost {
		body = strings.NewReader(form)
	}
	request, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	return response
}

func TestOversizedUpstreamAnswers(t *testing.T) {
	const upstreamBodyLimit = 1 << 20
	huge := `{"pad":"` + strings.Repeat("a", upstreamBodyLimit) + `"}`
	steamTarget := steamCallbackTarget(testCallbackURL)
	cases := []struct {
		name     string
		hugePath string
		method   string
		target   string
		status   int
		location string
	}{
		{"check_authentication", "/openid/login", http.MethodGet, steamTarget, http.StatusBadGateway, ""},
		{"GetOwnedGames", "/IPlayerService/GetOwnedGames/v1/", http.MethodGet, steamTarget, http.StatusFound, testPublicURL + "/#error-steam-license-unknown"},
		{"itch.io profile", "/profile", http.MethodPost, "/auth/itch.io/callback", http.StatusFound, testPublicURL + "/#error-itch.io-license-unknown"},
		{"itch.io owned-keys", "/profile/owned-keys", http.MethodPost, "/auth/itch.io/callback", http.StatusFound, testPublicURL + "/#error-itch.io-license-unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, server := servedApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case c.hugePath:
					io.WriteString(w, huge)
				case "/openid/login":
					io.WriteString(w, openIDValidAnswer)
				case "/profile":
					io.WriteString(w, `{"user":{"id":1001}}`)
				default:
					io.WriteString(w, ownsBuckshot)
				}
			}))
			response := doWithoutRedirects(t, c.method, server.URL+c.target, "token=owner")
			if response.StatusCode != c.status || response.Header.Get("Location") != c.location {
				t.Fatalf("got %d %q", response.StatusCode, response.Header.Get("Location"))
			}
		})
	}
}

type receivedRequest struct {
	server   string
	path     string
	referer  string
	keyFound bool
}

func TestRedirectedUpstreamCallKeepsTheKeyHome(t *testing.T) {
	const key = "test-steam-key"
	var mu sync.Mutex
	var received []receivedRequest
	record := func(server string, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		keyFound := strings.Contains(r.URL.String(), key) || strings.Contains(string(body), key)
		for _, values := range r.Header {
			keyFound = keyFound || strings.Contains(strings.Join(values, "\n"), key)
		}
		mu.Lock()
		received = append(received, receivedRequest{server, r.URL.Path, r.Header.Get("Referer"), keyFound})
		mu.Unlock()
	}
	landing := startServer(t, &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record("landing", r)
		io.WriteString(w, ownsBuckshot)
	})}, localListener(t))
	_, server := servedApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record("upstream", r)
		switch r.URL.Path {
		case "/openid/login":
			io.WriteString(w, openIDValidAnswer)
		case "/IPlayerService/GetOwnedGames/v1/":
			http.Redirect(w, r, landing.URL+"/elsewhere", http.StatusFound)
		}
	}))
	response := doWithoutRedirects(t, http.MethodGet, server.URL+steamCallbackTarget(testCallbackURL), "")
	if location := response.Header.Get("Location"); location != testPublicURL+"/#success" {
		t.Fatalf("got %d %q", response.StatusCode, location)
	}
	want := []receivedRequest{
		{"upstream", "/openid/login", "", false},
		{"upstream", "/IPlayerService/GetOwnedGames/v1/", "", true},
		{"landing", "/elsewhere", "", false},
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(received, want) {
		t.Fatalf("received %+v, want %+v", received, want)
	}
}

func TestUpstreamTimeouts(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"no answer": func(w http.ResponseWriter, r *http.Request) {
			io.ReadAll(r.Body)
			<-r.Context().Done()
		},
		"trickling body": func(w http.ResponseWriter, r *http.Request) {
			io.ReadAll(r.Body)
			for {
				io.WriteString(w, " ")
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
					return
				case <-time.After(50 * time.Millisecond):
				}
			}
		},
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			upstreamServer := startServer(t, &http.Server{Handler: handler}, localListener(t))
			cfg := testConfig(t)
			cfg.Origins.SteamCommunity = upstreamServer.URL
			a := New(cfg, upstream.NewClient(200*time.Millisecond, 500*time.Millisecond), fixedClock)
			started := time.Now()
			assertJSONError(t, serve(a, http.MethodGet, steamCallbackTarget(testCallbackURL)), http.StatusBadGateway, steam.ErrUnavailable.Message)
			if waited := time.Since(started); waited > 2*time.Second {
				t.Fatalf("gave up after %s", waited)
			}
		})
	}
}

// stingyListener gives every connection a small send buffer, so a client that stops reading stalls the server quickly.
type stingyListener struct{ net.Listener }

func (l stingyListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if tcp, ok := conn.(*net.TCPConn); ok {
		tcp.SetWriteBuffer(16 << 10)
	}
	return conn, err
}

func downloadServer(t *testing.T, size int, stallTimeout time.Duration) (net.Conn, *bufio.Reader) {
	t.Helper()
	a := downloadApp(t, make([]byte, size))
	a.stallTimeout = stallTimeout
	server := startServer(t, newHTTPServer(a), stingyListener{localListener(t)})
	conn, reader := dial(t, server)
	conn.(*net.TCPConn).SetReadBuffer(16 << 10)
	conn.Write([]byte("GET /download HTTP/1.1\r\nHost: t\r\nCookie: " + licenseHeader(validCookieRaw) + "\r\n\r\n"))
	return conn, reader
}

func TestStalledDownloadIsDropped(t *testing.T) {
	const size = 16 << 20
	conn, reader := downloadServer(t, size, 200*time.Millisecond)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if status, err := reader.ReadString('\n'); err != nil || !strings.HasPrefix(status, "HTTP/1.1 200") {
		t.Fatalf("status %q, %v", status, err)
	}
	time.Sleep(time.Second)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	received, err := io.Copy(io.Discard, reader)
	if isTimeout(err) {
		t.Fatal("the server kept the stalled download open")
	}
	if received >= size {
		t.Fatalf("received the whole file (%d bytes) although the download stalled", received)
	}
}

func TestSlowDownloadKeepsGoing(t *testing.T) {
	const size = 2 << 20
	stallTimeout := 300 * time.Millisecond
	conn, reader := downloadServer(t, size, stallTimeout)
	conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	var received int64
	buffer := make([]byte, 16<<10)
	for {
		n, err := response.Body.Read(buffer)
		received += int64(n)
		if err != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if received != size {
		t.Fatalf("received %d of %d bytes", received, size)
	}
	if took := time.Since(started); took < 2*stallTimeout {
		t.Fatalf("the download took %s, too fast to show that a slow client keeps going", took)
	}
	time.Sleep(2 * stallTimeout)
	conn.Write([]byte("GET /logout HTTP/1.1\r\nHost: t\r\n\r\n"))
	if answer := readAnswer(t, conn, reader, 2*time.Second); answer.header.Get("Location") != testPublicURL+"/#try-again" {
		t.Fatalf("the connection is unusable after the download: %s", answer.status)
	}
}

func TestServeShutsDownGracefully(t *testing.T) {
	a, _ := newTestApp(t, func(call upstreamtest.Call) (int, string, error) {
		if call.Path == "/profile" {
			time.Sleep(300 * time.Millisecond)
			return http.StatusOK, `{"user":{"id":1001}}`, nil
		}
		return http.StatusOK, `{"owned_keys":[{"game_id":2443623}]}`, nil
	})
	listener := localListener(t)
	ctx, stop := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, listener, a) }()

	answered := make(chan string, 1)
	go func() {
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, err := client.Post("http://"+listener.Addr().String()+"/auth/itch.io/callback", "application/x-www-form-urlencoded", strings.NewReader("token=owner"))
		if err != nil {
			answered <- err.Error()
			return
		}
		response.Body.Close()
		answered <- response.Header.Get("Location")
	}()
	time.Sleep(100 * time.Millisecond)
	stop()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after the context ended")
	}
	if location := <-answered; location != testPublicURL+"/#success" {
		t.Fatalf("the request in flight got %q", location)
	}
	if conn, err := net.Dial("tcp", listener.Addr().String()); err == nil {
		conn.Close()
		t.Fatal("the listener is still open")
	}
}
