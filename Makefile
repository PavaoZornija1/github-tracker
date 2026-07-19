.PHONY: help compose-up compose-down run-api run-worker test generate swag tidy fmt

help:
	@echo "Targets:"
	@echo "  compose-up    Start Postgres, Redis, RabbitMQ"
	@echo "  compose-down  Stop Compose stack"
	@echo "  run-api       Run API binary (requires DATABASE_URL)"
	@echo "  run-worker    Run worker binary"
	@echo "  test          Run unit tests"
	@echo "  generate      Regenerate Ent client"
	@echo "  swag          Regenerate OpenAPI docs (docs/docs.go + swagger.{json,yaml})"
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

generate:
	go generate ./ent

swag:
	go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g cmd/api/main.go -o docs --parseDependency --parseInternal

tidy:
	go mod tidy

fmt:
	go fmt ./...
