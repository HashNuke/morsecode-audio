package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/HashNuke/morsecode-audio/internal/api"
	"github.com/HashNuke/morsecode-audio/internal/audio"
	"github.com/HashNuke/morsecode-audio/internal/config"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	codecContext, cancelCodecs := context.WithTimeout(context.Background(), 10*time.Second)
	err = audio.CheckCodecs(codecContext)
	cancelCodecs()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.Address())
	if err != nil {
		return fmt.Errorf("bind listener: %w", err)
	}
	defer listener.Close()
	handler := api.New()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	slog.Info("MorseCode API listening", "address", listener.Addr().String())
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		handler.Drain()
		deadline, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(deadline); err != nil {
			_ = server.Close()
			return err
		}
		return handler.WaitSockets(deadline)
	}
}
