package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/seron-cheng/infinite-ai/services/bff/internal/app"
	"github.com/seron-cheng/infinite-ai/services/shared/config"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	server, err := app.New(ctx, cfg)
	if err != nil {
		log.Fatalf("bff init failed: %v", err)
	}

	go func() {
		log.Printf("bff listening on :%s", cfg.BFFPort)
		if err := server.HTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("bff listen failed: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.HTTP.Shutdown(shutdownCtx)
	server.Close()
}

