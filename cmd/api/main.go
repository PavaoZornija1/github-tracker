package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PavaoZornija1/github-tracker/internal/cache"
	"github.com/PavaoZornija1/github-tracker/internal/config"
	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
	"github.com/PavaoZornija1/github-tracker/internal/httpapi"
	"github.com/PavaoZornija1/github-tracker/internal/platform/db"
	"github.com/PavaoZornija1/github-tracker/internal/platform/logging"
	"github.com/PavaoZornija1/github-tracker/internal/platform/redisx"
	"github.com/PavaoZornija1/github-tracker/internal/queue"
	"github.com/PavaoZornija1/github-tracker/internal/ratelimit"
	"github.com/PavaoZornija1/github-tracker/internal/service"

	_ "github.com/PavaoZornija1/github-tracker/docs"
)

// @title GitHub Tracker API
// @version 1.0
// @description Tracks GitHub repositories. No end-user auth. GITHUB_TOKEN is optional server-side config only.
// @host localhost:8080
// @BasePath /api
func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}

	logger := logging.NewJSON(parseLogLevel(cfg.LogLevel))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	entClient, err := db.OpenPostgres(ctx, cfg.DatabaseURL, db.OpenOptions{AutoMigrate: cfg.AutoMigrate()})
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	defer entClient.Close()

	rdb, err := redisx.Connect(ctx, cfg.RedisURL)
	if err != nil {
		logger.Error("open redis", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	mq, err := queue.Connect(queue.Config{
		URL:                cfg.RabbitMQURL,
		Exchange:           cfg.RabbitMQExchange,
		Queue:              cfg.RabbitMQQueue,
		DLX:                cfg.RabbitMQDLX,
		DLQ:                cfg.RabbitMQDLQ,
		MaxRetryExpiration: cfg.GitHubRateLimitRetryMax,
	})
	if err != nil {
		logger.Error("open rabbitmq", "err", err)
		os.Exit(1)
	}
	defer mq.Close()

	gate := ratelimit.NewGitHubGate(rdb)
	gh := githubclient.New(githubclient.Options{
		BaseURL:  cfg.GitHubAPIBaseURL,
		Token:    cfg.GitHubToken,
		Timeout:  cfg.GitHubHTTPTimeout,
		Observer: gate,
	})
	ghCache := cache.NewGitHubCache(rdb, gh, cache.GitHubCacheOptions{
		TTL:     cfg.GitHubCacheTTL,
		LockTTL: cfg.GitHubFetchLockTTL,
		Gate:    gate,
	})
	repoSvc := service.NewRepoService(entClient, ghCache)
	batchSvc := service.NewBatchService(entClient, repoSvc, queue.NewPublisher(mq), cfg.WorkerMaxRetries)

	engine := httpapi.NewRouter(httpapi.RouterDeps{
		Repos:   repoSvc,
		Batches: batchSvc,
		Logger:  logger,
		Ready: func(ctx context.Context) error {
			if _, err := entClient.Repository.Query().Limit(1).Count(ctx); err != nil {
				return fmt.Errorf("postgres: %w", err)
			}
			if err := rdb.Ping(ctx).Err(); err != nil {
				return fmt.Errorf("redis: %w", err)
			}
			return nil
		},
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           engine,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("api listening",
			"addr", cfg.HTTPAddr,
			"github_token_set", cfg.GitHubToken != "",
		)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("api shutting down", "reason", ctx.Err())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("api shutdown", "err", err)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("api server failed", "err", err)
			os.Exit(1)
		}
	}
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
