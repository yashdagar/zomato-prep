#!/usr/bin/env bash
# W7 B3: the production compose stack comes up on localhost and Caddy serves https with a redirect from http.
set -euo pipefail
cd "$(dirname "$0")/.."
env_file=$(mktemp)
cp .env.example "$env_file"
dc() { docker compose -p zomato-w7check --env-file "$env_file" -f compose.prod.yml "$@"; }
cleanup() { dc down -v >/dev/null 2>&1 || true; rm -f "$env_file"; }
trap cleanup EXIT

dc up -d --build --wait --wait-timeout 300 || { echo "FAIL: the stack did not become healthy"; dc ps -a; dc logs --tail 40; exit 1; }

code=000
for _ in $(seq 30); do
  code=$(curl -ks -o /dev/null -w '%{http_code}' https://localhost/healthz || true)
  [ "$code" = "200" ] && break
  sleep 1
done
if [ "$code" != "200" ]; then echo "FAIL: https://localhost/healthz returned $code"; dc logs --tail 40 caddy api; exit 1; fi
echo "PASS: https://localhost/healthz is 200 through Caddy"

ready=$(curl -ks https://localhost/readyz || true)
case "$ready" in *'"status":"ok"'*) echo "PASS: /readyz reports mysql, redis and kafka ok";; *) echo "FAIL: /readyz said $ready"; exit 1;; esac

location=$(curl -s -o /dev/null -w '%{redirect_url}' http://localhost/healthz || true)
case "$location" in https://*) echo "PASS: http redirects to $location";; *) echo "FAIL: http://localhost/healthz did not redirect to https (got '$location')"; exit 1;; esac

if docker compose -p zomato-w7check --env-file "$env_file" -f compose.prod.yml ps migrate -a --format '{{.State}} {{.ExitCode}}' | grep -q 'exited 0'; then echo "PASS: the migrate job ran and exited 0"; else echo "FAIL: the migrate job did not exit 0"; exit 1; fi
