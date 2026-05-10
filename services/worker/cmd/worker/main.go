package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/seron-cheng/infinite-ai/services/shared/config"
	"github.com/seron-cheng/infinite-ai/services/worker/internal/worker"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runner, err := worker.New(ctx, cfg)
	if err != nil {
		log.Fatalf("worker init failed: %v", err)
	}
	defer runner.Close()

	if err := runner.Run(ctx); err != nil {
		log.Fatalf("worker run failed: %v", err)
	}
}

