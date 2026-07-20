package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds runtime configuration loaded from environment variables.
type Config struct {
	AppEnv   string
	HTTPAddr string

	DatabaseURL string

	RedisURL string

	RabbitMQURL      string
	RabbitMQQueue    string
	RabbitMQDLQ      string
	RabbitMQExchange string
	RabbitMQDLX      string

	WorkerConcurrency int
	WorkerMaxRetries  int

	GitHubToken        string
	GitHubAPIBaseURL   string
	GitHubHTTPTimeout  time.Duration
	GitHubCacheTTL     time.Duration
	GitHubFetchLockTTL time.Duration
	// GitHubRateLimitRetryMax caps Rabbit retry TTL for rate-limit delays (default 15m).
	GitHubRateLimitRetryMax time.Duration

	LogLevel string
}

// AutoMigrate reports whether Ent Schema.Create should run on startup.
// Enabled for empty/development/local/test; disabled for production.
func (c Config) AutoMigrate() bool {
	switch c.AppEnv {
	case "", "development", "local", "test", "dev":
		return true
	default:
		return false
	}
}

// Load reads configuration from the process environment.
func Load() (Config, error) {
	cfg := Config{
		AppEnv:             getEnv("APP_ENV", "development"),
		HTTPAddr:           getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		RedisURL:           getEnv("REDIS_URL", "redis://localhost:6379/0"),
		RabbitMQURL:        getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		RabbitMQQueue:      getEnv("RABBITMQ_QUEUE", "repo.refresh"),
		RabbitMQDLQ:        getEnv("RABBITMQ_DLQ", "repo.refresh.dlq"),
		RabbitMQExchange:   getEnv("RABBITMQ_EXCHANGE", "repo.jobs"),
		RabbitMQDLX:        getEnv("RABBITMQ_DLX", "repo.jobs.dlx"),
		WorkerConcurrency:  getEnvInt("WORKER_CONCURRENCY", 5),
		WorkerMaxRetries:   getEnvInt("WORKER_MAX_RETRIES", 3),
		GitHubToken:             os.Getenv("GITHUB_TOKEN"),
		GitHubAPIBaseURL:        getEnv("GITHUB_API_BASE_URL", "https://api.github.com"),
		GitHubHTTPTimeout:       getEnvDuration("GITHUB_HTTP_TIMEOUT", 10*time.Second),
		GitHubCacheTTL:          getEnvDuration("GITHUB_CACHE_TTL", 5*time.Minute),
		GitHubFetchLockTTL:      getEnvDuration("GITHUB_FETCH_LOCK_TTL", 30*time.Second),
		GitHubRateLimitRetryMax: getEnvDuration("GITHUB_RATE_LIMIT_RETRY_MAX", 15*time.Minute),
		LogLevel:                getEnv("LOG_LEVEL", "info"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.WorkerConcurrency < 1 {
		return Config{}, fmt.Errorf("WORKER_CONCURRENCY must be >= 1")
	}
	if cfg.WorkerMaxRetries < 0 {
		return Config{}, fmt.Errorf("WORKER_MAX_RETRIES must be >= 0")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
