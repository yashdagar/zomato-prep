#!/usr/bin/env bash
# W1 D4: the API keeps serving while Redis is down (fail open).
set -euo pipefail
API=${TEST_API_URL:-http://localhost:8080}
docker compose stop redis >/dev/null
sleep 1
code=$(curl -s -o /dev/null -w '%{http_code}' -H "X-User-ID: failopen-$RANDOM" "$API/healthz")
docker compose start redis >/dev/null
if [ "$code" = "200" ]; then echo "PASS: /healthz returned 200 with Redis stopped"; else echo "FAIL: /healthz returned $code with Redis stopped"; exit 1; fi
