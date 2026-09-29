.PHONY: run build test-unit test-integration test test-race cover lint migrate-up migrate-down docker-up docker-down

run:
	@if [ -f .env ]; then set -a; . ./.env; set +a; fi; go run ./cmd/server

build:
	go build -o bin/server ./cmd/server
	go build -o bin/migrate ./cmd/migrate

test-unit:
	go test -short -race ./...

test-integration:
	go test -race ./internal/repository/postgres/... ./test/e2e/...

test:
	go test -race ./...

# test-race is an explicit alias for `test`: every target here already
# runs with -race (a plain race-free run isn't useful for this project),
# but the name is kept for discoverability.
test-race: test

cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run

migrate-up:
	@if [ -f .env ]; then set -a; . ./.env; set +a; fi; go run ./cmd/migrate -direction=up

migrate-down:
	@if [ -f .env ]; then set -a; . ./.env; set +a; fi; go run ./cmd/migrate -direction=down

docker-up:
	docker compose up --build

docker-down:
	docker compose down -v
