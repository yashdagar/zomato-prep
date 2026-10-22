#!/usr/bin/env bash
# W3 D3: kill -9 the worker with events queued; after restart, exactly one
# notification per event. Needs: API up, worker started as `make worker`
# in another terminal, TEST_API_URL, MYSQL_DSN like
# "root:pass@tcp(127.0.0.1:3306)/zomato", and the e2e tests compiled.
set -euo pipefail
API=${TEST_API_URL:-http://localhost:8080}
: "${MYSQL_DSN:?set MYSQL_DSN}"
q() { docker compose exec -T mysql mysql -uroot -p"${MYSQL_ROOT_PASSWORD:-root}" zomato -N -e "$1"; }
before_events=$(q "SELECT COUNT(*) FROM order_events")
echo "placing 200 orders..."
TEST_API_URL=$API go test ./e2e -run 'TestW3B4_IdempotencyKeyIsRequiredAndDedupes' -count=40 >/dev/null
pid=$(pgrep -f 'cmd/worker' | head -1 || true)
if [ -z "$pid" ]; then echo "FAIL: worker is not running (make worker)"; exit 1; fi
kill -9 "$pid"; echo "killed worker $pid; restart it now with: make worker"; sleep 20
after_events=$(q "SELECT COUNT(*) FROM order_events")
notifs=$(q "SELECT COUNT(*) FROM notifications n JOIN order_events e ON e.event_id = n.event_id")
dups=$(q "SELECT COUNT(*) FROM (SELECT event_id FROM notifications GROUP BY event_id HAVING COUNT(*) > 1) d")
echo "events: $before_events -> $after_events, notifications joined: $notifs, duplicates: $dups"
if [ "$dups" = "0" ] && [ "$notifs" -ge $((after_events - before_events)) ]; then echo PASS; else echo FAIL; exit 1; fi
