#!/usr/bin/env bash
# W3 B8: the api and worker binaries exit 0 within 10 s of SIGTERM and log
# "shutdown complete". Needs make up and make migrate, MYSQL_DSN and
# JWT_SECRET set as for make run, and port 8080 free.
set -uo pipefail
cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fail=0
for bin in api worker; do
  go build -o "$tmp/$bin" "./cmd/$bin" || { echo "FAIL: $bin does not build"; fail=1; continue; }
  "$tmp/$bin" >"$tmp/$bin.log" 2>&1 &
  pid=$!
  sleep 3
  if ! kill -0 "$pid" 2>/dev/null; then
    echo "FAIL: $bin exited before SIGTERM"; tail -5 "$tmp/$bin.log"; fail=1; continue
  fi
  start=$(date +%s)
  kill -TERM "$pid"
  wait "$pid"; code=$?
  took=$(( $(date +%s) - start ))
  if [ "$code" = 0 ] && [ "$took" -le 10 ] && grep -q "shutdown complete" "$tmp/$bin.log"; then
    echo "PASS: $bin exited 0 in ${took}s and logged shutdown complete"
  else
    echo "FAIL: $bin exit code $code after ${took}s"; tail -5 "$tmp/$bin.log"; fail=1
  fi
done
exit $fail
