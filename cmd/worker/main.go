package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/PavaoZornija1/github-tracker/internal/cache"
	"github.com/PavaoZornija1/github-tracker/internal/config"
	"github.com/PavaoZornija1/github-tracker/internal/githubclient"
	"github.com/PavaoZornija1/github-tracker/internal/platform/db"
	"github.com/PavaoZornija1/github-tracker/internal/platform/logging"
	"github.com/PavaoZornija1/github-tracker/internal/platform/redisx"
	"github.com/PavaoZornija1/github-tracker/internal/platform/requestid"
	"github.com/PavaoZornija1/github-tracker/internal/queue"
	"github.com/PavaoZornija1/github-tracker/internal/ratelimit"
	"github.com/PavaoZornija1/github-tracker/internal/service"
)

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

	consumer := queue.NewConsumer(mq, queue.ConsumerOptions{
		Concurrency: cfg.WorkerConcurrency,
		MaxRetries:  cfg.WorkerMaxRetries,
		Handler: func(ctx context.Context, job queue.RefreshJob, attempt int) error {
			ctx = requestid.WithJobID(ctx, job.JobID.String())
			log := logging.FromContext(ctx, logger)
			log.Info("processing refresh job", "batch_id", job.BatchID, "repo_id", job.RepoID, "attempt", attempt)
			err := batchSvc.ProcessRefreshJob(ctx, job, attempt)
			if err != nil {
				log.Info("refresh job outcome", "err", err, "attempt", attempt)
			}
			return err
		},
		KickHandler: func(ctx context.Context, kick queue.BatchKick) error {
			ctx = requestid.WithJobID(ctx, kick.BatchID.String())
			log := logging.FromContext(ctx, logger)
			log.Info("fan-out batch kick", "batch_id", kick.BatchID)
			return batchSvc.FanOutBatch(ctx, kick.BatchID)
		},
	})

	logger.Info("worker starting",
		"concurrency", cfg.WorkerConcurrency,
		"max_retries", cfg.WorkerMaxRetries,
		"queue", cfg.RabbitMQQueue,
		"retry_queue", queue.RetryQueueName(cfg.RabbitMQQueue),
		"dlq", cfg.RabbitMQDLQ,
	)

	if err := consumer.Run(ctx); err != nil {
		logger.Error("worker stopped", "err", err)
		os.Exit(1)
	}
	logger.Info("worker shut down cleanly")
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
