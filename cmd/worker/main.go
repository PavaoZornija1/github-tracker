package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/PavaoZornija1/github-tracker/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("worker starting",
		"concurrency", cfg.WorkerConcurrency,
		"max_retries", cfg.WorkerMaxRetries,
		"rabbitmq_queue", cfg.RabbitMQQueue,
		"rabbitmq_dlq", cfg.RabbitMQDLQ,
		"github_token_set", cfg.GitHubToken != "",
	)

	// RabbitMQ consumer wiring lands in a later commit.
	<-ctx.Done()
	slog.Info("worker shutting down", "reason", ctx.Err())
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
