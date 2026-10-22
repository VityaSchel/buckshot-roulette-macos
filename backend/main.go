package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/config"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/server"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/upstream"
)

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	app := server.New(cfg, upstream.NewClient(upstream.StepTimeout, upstream.TotalTimeout), time.Now)
	return server.Serve(ctx, listener, app)
}
