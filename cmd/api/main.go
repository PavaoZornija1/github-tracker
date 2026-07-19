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

	slog.Info("api starting",
		"addr", cfg.HTTPAddr,
		"redis_configured", cfg.RedisURL != "",
		"rabbitmq_queue", cfg.RabbitMQQueue,
		"github_token_set", cfg.GitHubToken != "",
	)

	// HTTP server wiring lands in a later commit.
	<-ctx.Done()
	slog.Info("api shutting down", "reason", ctx.Err())
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
