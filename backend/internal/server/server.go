package server

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"time"

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/compat"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/config"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/download"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/httpx"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/itchio"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/steam"
)

const (
	// Elysia answered a bad signature with the relative "/logout", which misses Caddy's /api prefix,
	// so the cookie was never cleared. Set to false to restore that answer.
	clearBadLicenseThroughAPI = true
	requestBodyTimeout        = 30 * time.Second
	shutdownGrace             = 10 * time.Second
)

type route struct {
	get  http.HandlerFunc
	post http.HandlerFunc
}

type App struct {
	cfg              config.Config
	now              func() time.Time
	steam            *steam.Client
	itch             *itchio.Client
	routes           map[string]route
	badLicenseTarget string
	bodyTimeout      time.Duration
	stallTimeout     time.Duration
}

func New(cfg config.Config, client *http.Client, now func() time.Time) *App {
	a := &App{
		cfg:              cfg,
		now:              now,
		steam:            steam.New(cfg, client, now),
		itch:             itchio.New(cfg, client, now),
		badLicenseTarget: "/logout",
		bodyTimeout:      requestBodyTimeout,
		stallTimeout:     download.StallTimeout,
	}
	if clearBadLicenseThroughAPI {
		a.badLicenseTarget = cfg.LogoutURL
	}
	a.routes = map[string]route{
		"/auth/steam":            {get: redirectTo(a.steam.LoginURL())},
		"/auth/steam/callback":   {get: a.steamCallback},
		"/auth/itch.io":          {get: redirectTo(a.itch.LoginURL())},
		"/auth/itch.io/callback": {get: itchio.ServeCallbackPage, post: a.itchCallback},
		"/download":              {get: a.download},
		"/logout":                {get: a.logout},
	}
	return a
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer httpx.Recover(w)
	if r.ContentLength != 0 {
		httpx.GuardBody(w, a.bodyTimeout)
		defer httpx.DropUnreadBody(w)
	}
	handler := a.handlerFor(r)
	if handler == nil {
		httpx.Error(w, http.StatusNotFound, "Not found")
		return
	}
	handler(w, r)
}

func (a *App) handlerFor(r *http.Request) http.HandlerFunc {
	route := a.routes[compat.RoutePath(r.URL.EscapedPath())]
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		return route.get
	case http.MethodPost:
		return route.post
	}
	return nil
}

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Longer than Caddy's 2 minute keep-alive, so the proxy closes idle connections first.
		IdleTimeout:                  150 * time.Second,
		MaxHeaderBytes:               64 << 10,
		DisableGeneralOptionsHandler: true,
	}
}

func Serve(ctx context.Context, listener net.Listener, handler http.Handler) error {
	server := newHTTPServer(handler)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	log.Printf("Server running on http://%s", listener.Addr())

	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Print("shutdown: closing the connections still open")
		server.Close()
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
