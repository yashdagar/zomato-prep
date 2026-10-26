.PHONY: lint test unit e2e e2e-grpc all run worker up down migrate sqlc
# Unit tests compile against the packages you write; Redis-backed ones run
# when TEST_REDIS_ADDR is set (W1B7 also needs the redis build tag). E2E tests run against a live API.

lint:
	golangci-lint run ./...

test:
	@go run ./tools/zt test $(W) $(if $(V),-v)

unit:
	go test -race -count=1 $(if $(TEST_REDIS_ADDR),-tags redis) ./internal/...

e2e:
	go test -count=1 ./e2e/...

e2e-grpc:
	go test -count=1 -tags grpc ./e2e/... -run W7

all: unit e2e

run:
	@go run ./tools/zt banner api
	@go run ./cmd/api

worker:
	@go run ./tools/zt banner worker
	@go run ./cmd/worker

up:
	docker compose up -d

down:
	docker compose down

migrate:
	migrate -database "mysql://root:$${MYSQL_ROOT_PASSWORD:-dev}@tcp(127.0.0.1:3306)/zomato" -path db/migrations up

sqlc:
	sqlc generate
