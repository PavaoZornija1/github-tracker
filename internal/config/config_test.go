package config_test

import (
	"testing"
	"time"

	"github.com/PavaoZornija1/github-tracker/internal/config"
)

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when DATABASE_URL is empty")
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://tracker:tracker@localhost:5432/tracker?sslmode=disable")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("REDIS_URL", "")
	t.Setenv("WORKER_CONCURRENCY", "")
	t.Setenv("GITHUB_CACHE_TTL", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.WorkerConcurrency != 5 {
		t.Fatalf("WorkerConcurrency = %d, want 5", cfg.WorkerConcurrency)
	}
	if cfg.GitHubCacheTTL != 5*time.Minute {
		t.Fatalf("GitHubCacheTTL = %v, want 5m", cfg.GitHubCacheTTL)
	}
	if !cfg.AutoMigrate() {
		t.Fatal("default APP_ENV should auto-migrate")
	}
}

func TestAutoMigrateProductionDisabled(t *testing.T) {
	cfg := config.Config{AppEnv: "production"}
	if cfg.AutoMigrate() {
		t.Fatal("production must not auto-migrate")
	}
	cfg.AppEnv = "development"
	if !cfg.AutoMigrate() {
		t.Fatal("development should auto-migrate")
	}
}
