# zomato-prep

Tests first, code second. This repository starts with **only tests** for the
eight-week Go + React Native plan at https://yashdagar.netlify.app/zomato.
Each build step on the site ends with a **Verify** command that runs the tests
for exactly that step. A test that will not compile means the package or
function it expects does not exist yet: that is the spec.

## Layout

```
backend/            Go module github.com/yashdagar/zomato-prep/backend (you write cmd/ and internal/)
  internal/<pkg>/*_test.go   unit tests, one package per step (W1 B2 = internal/ratelimit, ...)
  e2e/              black-box tests against a running API, TEST_API_URL=http://localhost:8080
  proto/orders/v1   the gRPC contract for week 7 (buf lint + buf generate into gen/)
  scripts/          checks that need Docker, kill -9 or grpcurl
app-tests/          Jest tests for app/lib (ApiClient, cart store, reconnect backoff)
app/                the Expo app, created in week 4 (npx create-expo-app app --template blank-typescript)
```

## Running

```sh
cd backend
go test -race ./internal/ratelimit/        # one step
make unit                                  # every unit test (Redis ones need TEST_REDIS_ADDR)
TEST_REDIS_ADDR=127.0.0.1:6379 make unit
TEST_API_URL=http://localhost:8080 make e2e            # against your running API and worker
TEST_API_URL=http://localhost:8080 go test ./e2e -run W3   # one week
TEST_GRPC_ADDR=localhost:9090 make e2e-grpc            # week 7, after buf generate

cd ../app-tests && npm install && npx jest             # week 4 and 5 app tests
```

Test names start with the step they verify (`TestW3B4_...`), so `-run W3B4`
runs one step and `-run W3` runs a week.

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
