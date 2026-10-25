.PHONY: test unit e2e e2e-grpc all run worker
# Unit tests compile against the packages you write; Redis-backed ones run
# when TEST_REDIS_ADDR is set. E2E tests run against a live API.
test:
	@go run ./tools/zt test $(W) $(if $(V),-v)

unit:
	go test -race -count=1 ./internal/...

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
