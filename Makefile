.PHONY: help compose-up compose-down run-api run-worker test swag tidy fmt

help:
	@echo "Targets:"
	@echo "  compose-up    Start Postgres, Redis, RabbitMQ"
	@echo "  compose-down  Stop Compose stack"
	@echo "  run-api       Run API binary (requires DATABASE_URL)"
	@echo "  run-worker    Run worker binary"
	@echo "  test          Run unit tests"
	@echo "  swag          Regenerate OpenAPI (once handlers exist)"
	@echo "  tidy          go mod tidy"
	@echo "  fmt           go fmt ./..."

compose-up:
	docker compose up -d

compose-down:
	docker compose down

run-api:
	@test -n "$$DATABASE_URL" || (echo "Set DATABASE_URL (see .env.example)" && exit 1)
	go run ./cmd/api

run-worker:
	@test -n "$$DATABASE_URL" || (echo "Set DATABASE_URL (see .env.example)" && exit 1)
	go run ./cmd/worker

test:
	go test ./...

swag:
	@echo "swag init will be wired after HTTP handlers land"
	@exit 1

tidy:
	go mod tidy

fmt:
	go fmt ./...
