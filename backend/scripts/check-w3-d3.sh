#!/usr/bin/env bash
# W3 D3: kill -9 the worker with events queued; after restart, exactly one
# notification per event. Needs: API up, worker started as `make worker`
# in another terminal, TEST_API_URL, and MYSQL_DATABASE if it is not zomato.
set -euo pipefail
API=${TEST_API_URL:-http://localhost:8080}
DB=${MYSQL_DATABASE:-zomato}
q() { docker compose exec -T mysql mysql -uroot -p"${MYSQL_ROOT_PASSWORD:-dev}" "$DB" -N -e "$1" 2>/dev/null; }
before_events=$(q "SELECT COUNT(*) FROM order_events")
echo "placing 80 orders..."
TEST_API_URL=$API go test ./e2e -run 'TestW3B4_IdempotencyKeyIsRequiredAndDedupes' -count=40 >/dev/null
# make worker runs `go run ./cmd/worker`, which starts a child binary named
# worker; killing only the go parent would leave the real worker running.
pid=$(pgrep -f '/exe/worker$' | head -1 || true)
[ -z "$pid" ] && pid=$(pgrep -f 'cmd/worker' | head -1 || true)
if [ -z "$pid" ]; then echo "FAIL: worker is not running (make worker)"; exit 1; fi
kill -9 "$pid"; echo "killed worker $pid; restart it now with: make worker"; sleep 20
after_events=$(q "SELECT COUNT(*) FROM order_events")
notifs=$(q "SELECT COUNT(*) FROM notifications n JOIN order_events e ON e.event_id = n.event_id")
dups=$(q "SELECT COUNT(*) FROM (SELECT event_id FROM notifications GROUP BY event_id HAVING COUNT(*) > 1) d")
echo "events: $before_events -> $after_events, notifications joined: $notifs, duplicates: $dups"
if [ "$dups" = "0" ] && [ "$notifs" -ge $((after_events - before_events)) ]; then echo PASS; else echo FAIL; exit 1; fi
