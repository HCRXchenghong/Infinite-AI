package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/seron-cheng/infinite-ai/services/core/internal/app"
	"github.com/seron-cheng/infinite-ai/services/shared/config"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	server, err := app.New(ctx, cfg)
	if err != nil {
		log.Fatalf("core init failed: %v", err)
	}

	go func() {
		log.Printf("core api listening on :%s", cfg.CorePort)
		if err := server.HTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("core api listen failed: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.HTTP.Shutdown(shutdownCtx)
	server.Close()
}

