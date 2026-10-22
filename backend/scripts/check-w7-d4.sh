#!/usr/bin/env bash
# W7 D4: grpcurl can list the service, and PlaceOrder without a key is INVALID_ARGUMENT.
set -euo pipefail
ADDR=${TEST_GRPC_ADDR:-localhost:9090}
: "${TOKEN:?set TOKEN to an access token from /v1/auth/login}"
grpcurl -plaintext "$ADDR" list | grep -q orders.v1.OrderService && echo "PASS: reflection lists orders.v1.OrderService"
out=$(grpcurl -plaintext -H "authorization: Bearer $TOKEN" -d '{"restaurant_id":1,"lines":[{"item_id":1,"size":"M","qty":1}]}' "$ADDR" orders.v1.OrderService/PlaceOrder 2>&1 || true)
echo "$out" | grep -q InvalidArgument && echo "PASS: PlaceOrder without idempotency-key is InvalidArgument" || { echo "FAIL: $out"; exit 1; }
