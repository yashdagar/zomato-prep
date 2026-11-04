# zomato-prep

[![backend](https://github.com/yashdagar/zomato-prep/actions/workflows/backend.yml/badge.svg)](https://github.com/yashdagar/zomato-prep/actions/workflows/backend.yml)
![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)
![MySQL](https://img.shields.io/badge/MySQL-8.4-4479A1?logo=mysql&logoColor=white)
![Redis](https://img.shields.io/badge/Redis-7-DC382D?logo=redis&logoColor=white)
![Kafka](https://img.shields.io/badge/Kafka-Redpanda-231F20?logo=apachekafka&logoColor=white)
![Expo](https://img.shields.io/badge/Expo-React_Native-000020?logo=expo&logoColor=white)

The test suite for **[Learn the Zomato tech stack from scratch](https://yashdagar.netlify.app/learn-zomato-tech-stack-from-scratch)**, a free, self-paced, eight-week course where you build a food-delivery system with Go, MySQL, Redis, Kafka, WebSockets, gRPC and a React Native app.

This repository starts with only tests. You write the code. Each exercise on the course site ends with a command that runs the tests for exactly that step, and a test that will not compile tells you which package or function you have not written yet. The test is the spec.

- [Quick start](#quick-start)
- [What you will build](#what-you-will-build)
- [Curriculum](#curriculum)
- [How the tests work](#how-the-tests-work)
- [Running tests](#running-tests)
- [Repository layout](#repository-layout)
- [Tech stack](#tech-stack)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)
- [The API the e2e tests expect](#the-api-the-e2e-tests-expect)
- [Seed assumptions](#seed-assumptions)

## Quick start

You need Go, Docker, Node.js and `make`. The exact versions are in [Tech stack](#tech-stack).

```sh
git clone https://github.com/yashdagar/zomato-prep.git
cd zomato-prep/backend
docker compose up -d      # MySQL, Redis and Redpanda
make test                 # every test, grouped by week, with the next step to do
```

On a fresh clone almost every step reports `not written`. Open the course, pick the first exercise, write the code it describes and run its `make test W=...` command until the step turns green.

## What you will build

A small version of a food-delivery platform, one layer at a time.

- A **Go API** with rate limiting, restaurants, menus, authentication, orders and an admin view.
- **MySQL** for the data, **Redis** for caching, rate limits and rider locations, and **Kafka** for order events.
- A **WebSocket** hub and **Redis pub/sub** for live order tracking, and a **gRPC** service next to the HTTP one.
- A **React Native** customer and rider app built with Expo.
- Metrics, load tests, a production-style Docker Compose stack, backups and a write-up of the finished project.

## Curriculum

Every week has lessons, exercises, a capacity-planning lesson and an optional extensions module. The course site is the source of truth for the order and the explanations.

- **Week 1, Project setup and rate limiting.** Go project layout, the Makefile, lint and Docker Compose, then token bucket, sliding-window and Redis-backed rate limiters with tests and CI.
- **Week 2, Restaurants and menus with MySQL.** Migrations, `database/sql`, sqlc, keyset pagination, solving N+1 queries, error handling and the menu price configurator.
- **Week 3, Authentication, orders and Kafka events.** Password hashing, JWT and refresh tokens, middleware, idempotent order placement, the order state machine, the outbox pattern and Kafka consumers.
- **Week 4, Customer application with Expo.** Expo Router, secure storage, a typed API client with token refresh, TanStack Query lists, a cart store and checkout.
- **Week 5, Live order tracking and rider management.** Channels and the WebSocket hub, Redis pub/sub, Redis geo and exclusive rider claims, and the rider and customer tracking screens.
- **Week 6, Performance, caching and load testing.** Cache-aside with jitter and singleflight, Prometheus metrics, structured logs and timeouts, k6 load tests, pprof and reading `EXPLAIN`.
- **Week 7, Deployment and production operations.** Multi-stage Dockerfiles, a CI workflow that builds images, a Caddy and Compose stack with health checks, backups, an EAS preview build and gRPC.
- **Week 8, Advanced features and project documentation.** Optimistic locking for stock, payment flows with webhooks, a benchmark report and the project README.

## How the tests work

- Tests are named after the step they verify, such as `TestW3B4_...`. Here `W3` is the week, `B4` is the build step, and an `O` in place of the `B` means an optional extension.
- Unit tests live next to the package you write, one package per step, and run with `-race`.
- End-to-end tests in `backend/e2e` talk to your running API over HTTP, so the API contract at the bottom of this file is part of the specification.
- Optional extensions use build tags such as `w2o1`, which `make test W=2O1` sets for you.
- The `make test` front end, `tools/zt`, groups results by week and step, marks a step `not written` while its package is missing, and prints compile errors once per package. It exits non-zero on a failing or non-compiling test, so it works in CI.

## Running tests

```sh
cd backend
make test                                  # every test, grouped by week and step, with the next step to do
make test W=W1                             # one week
make test W=W1B2 V=1                       # one step, full failure output
make test W=2O1                            # one optional extension
make run                                   # logo banner, then go run ./cmd/api
make worker                                # logo banner, then go run ./cmd/worker
make lint                                  # golangci-lint over the packages you have written
```

`make test` runs the unit tests with `-race`, the e2e tests and the app tests together. Set `NO_COLOR=1` for plain output. It lists which environment variables would un-skip the tests that need a service.

The raw commands still work:

```sh
cd backend
go test -race ./internal/ratelimit/                    # one step
make unit                                              # every unit test (Redis ones need TEST_REDIS_ADDR)
TEST_REDIS_ADDR=127.0.0.1:6379 make unit               # also adds -tags redis for W1B7
TEST_API_URL=http://localhost:8080 make e2e            # against your running API and worker
TEST_API_URL=http://localhost:8080 go test ./e2e -run W3   # one week
TEST_GRPC_ADDR=localhost:9090 make e2e-grpc            # week 7, after buf generate

cd ../app-tests && npm install && npx jest             # the Expo app libraries
```

`-run W3B4` runs one step and `-run W3` runs a week.

## Repository layout

- `backend/` is the Go module `github.com/yashdagar/zomato-prep/backend`. You write `cmd/` and `internal/`.
  - `internal/<pkg>/*_test.go` are the unit tests, one package per step.
  - `e2e/` holds black-box tests against a running API, with `TEST_API_URL=http://localhost:8080`.
  - `proto/orders/v1` is the gRPC contract for week 7, used by `buf lint` and `buf generate` into `gen/`.
  - `scripts/` holds checks that need Docker, `kill -9` or `grpcurl`.
  - `tools/zt` is the `make test` and `make run` front end.
  - `docker-compose.yml` starts MySQL, Redis and Redpanda for local work.
- `app-tests/` holds the Jest tests for the app libraries: the API client, cart store, reconnect backoff and similar.
- `app/` is the Expo app you create in week 4 with `npx create-expo-app app --template blank-typescript`.

## Tech stack

- **Go 1.27** with `chi`, `database/sql`, sqlc, golang-migrate, `coder/websocket` and gRPC.
- **MySQL 8.4**, **Redis 7** and **Redpanda** (Kafka-compatible), all started by `docker compose up`.
- **Node.js and Jest** for the app libraries, and **Expo** with React Native for the app.
- **golangci-lint**, **buf**, **k6** and **Docker** for linting, protobuf, load tests and images.

## Troubleshooting

- **A test says `undefined`.** The package or function it expects does not exist yet. Read the test, then write it.
- **A step shows `skipped`.** It needs a service. Run `docker compose up -d` and set `TEST_REDIS_ADDR` or `TEST_API_URL` as listed at the end of `make test`.
- **`make lint` fails on a clean clone.** It only lints packages that contain non-test Go files, so write the package first.
- **Redis or MySQL tests are flaky on a busy laptop.** Close other heavy processes or raise the timeouts in the failing test run with `V=1` to see the output.
- **Port already in use.** Another MySQL, Redis or Kafka is running. Stop it or change the port in `docker-compose.yml`.

## Contributing

Pull requests use the template in `.github/PULL_REQUEST_TEMPLATE.md`. Read `CONTRIBUTING.md` for the title format, the branch and label rules, and the body sections a pull request must fill in.

## The API the e2e tests expect

Everything is JSON. Ids are integers. Money is paise.

- `POST /v1/auth/signup` and `/login` with `{email, password}`; signup also accepts
  `role` (`customer`, `restaurant`, `rider`) and `restaurant_id` so tests can
  create every role. Both return `{access_token, refresh_token}`.
  `POST /v1/auth/refresh {refresh_token}` returns a new pair and a reused token is 401.
  `GET /v1/me` returns `{id, email, role}`; anonymous is 401.
- `GET /v1/restaurants?lat&lng&radius_km&limit&cursor` returns
  `{items: [{id, name, lat, lng, is_open}], next_cursor}`; limit is clamped to 50;
  a bad cursor or a bad number is 400.
- `GET /v1/restaurants/{id}/menu` returns
  `{items: [{id, name, sizes: [{size, price_paise}], groups: [{id, name, min_select,
  max_select, options: [{id, name, in_stock, prices: {"R": 1000, "*": 500}}]}]}]}`;
  `"*"` means every size. Unknown restaurant is 404.
- `POST /v1/menu/price {item_id, size, chosen: {"<group_id>": [option_ids]}, qty}`
  returns `{description, unit_paise, total_paise, errors}` with every error at once.
- `POST /v1/orders` needs an `Idempotency-Key` header (400 without) and a body
  `{restaurant_id, lines: [{item_id, size, chosen, qty}]}`; returns the order
  `{id, status, version, total_paise, restaurant_id}`. The same key returns the same order.
  `GET /v1/orders?cursor&limit` returns `{items, next_cursor}` for the caller;
  `GET /v1/orders/{id}` adds `events: [{event_id, from_status, to_status}]` and is 404
  or 403 for another customer.
- `POST /v1/orders/{id}/events {event_id, event}` returns the order; wrong role is 403,
  illegal transition is 409, a repeated `event_id` is 200 and changes nothing.
- `GET /v1/ws?token=<access>` upgrades to a WebSocket (401 without a token) and sends
  `{order_id, event_id, from, to}` per transition.
- `POST /v1/rider/online`, `/offline`, `/location {lat, lng}` for riders (403 for others).
  `POST /v1/orders/{id}/accept` is 200 for the rider holding the claim, 409 otherwise.
- `GET /metrics` exposes `http_request_duration_seconds`, `orders_placed_total`,
  `ws_connections`. Every response carries `X-Request-Id`.
- `GET /v1/admin/stats?minutes=5` returns `{orders}` for the restaurant role, 403 otherwise.
- gRPC on `:9090`: `orders.v1.OrderService` from `backend/proto`, reflection on.

## Seed assumptions

`cmd/seed` puts 50 restaurants within 5 km of 28.46, 77.03 and gives the first
one a full menu whose first item has sizes and a group with `max_select` 1 and
at least two options (the crust group). The e2e tests discover ids from the API.
