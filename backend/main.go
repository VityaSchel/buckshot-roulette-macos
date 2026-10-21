package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const shutdownGrace = 10 * time.Second

func main() {
	log.SetFlags(0)
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
	listener, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	handler := newApp(cfg, newUpstreamClient(upstreamStepTimeout, upstreamTotalTimeout), time.Now)
	if err := serveUntil(ctx, listener, handler); err != nil {
		log.Print(err)
		stop()
		os.Exit(1)
	}
}

func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Longer than Caddy's 2 minute keep-alive, so the proxy closes idle connections first.
		IdleTimeout:                  150 * time.Second,
		MaxHeaderBytes:               64 << 10,
		DisableGeneralOptionsHandler: true,
	}
}

func serveUntil(ctx context.Context, listener net.Listener, handler http.Handler) error {
	server := newServer(handler)
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
