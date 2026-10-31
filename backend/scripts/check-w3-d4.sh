#!/usr/bin/env bash
# W3 D4: the DLQ holds only events that failed 3 times. Start the worker with
# FAIL_EVERY=1 (every send fails) and the API up, then run this. It places
# orders, waits for the retries (2 s, then 4 s of backoff), and checks every
# record on order-events.dlq carries attempt=3 and that the new events arrived.
set -euo pipefail
API=${TEST_API_URL:-http://localhost:8080}
WAIT=${WAIT:-25}
dlq() { docker compose exec -T redpanda rpk topic consume order-events.dlq -o :end -f json 2>/dev/null || true; }
before=$(dlq | grep -c '"topic":' || true)
echo "placing 4 orders (4 PLACED events)..."
TEST_API_URL=$API go test ./e2e -run 'TestW3B4_IdempotencyKeyIsRequiredAndDedupes' -count=2 >/dev/null
echo "waiting ${WAIT}s for three attempts per event..."
sleep "$WAIT"
dlq | python3 -c '
import json, sys
before = int(sys.argv[1])
text, recs, dec, i = sys.stdin.read(), [], json.JSONDecoder(), 0
while True:
    while i < len(text) and text[i].isspace():
        i += 1
    if i >= len(text):
        break
    rec, i = dec.raw_decode(text, i)
    recs.append(rec)
bad = []
for r in recs:
    h = {x["key"]: x["value"] for x in (r.get("headers") or [])}
    if h.get("attempt") != "3" and h.get("reason") != "invalid event":
        bad.append(h)
new = len(recs) - before
print(f"dlq records: {before} -> {len(recs)} (+{new}), records not at attempt 3: {len(bad)}")
sys.exit(0 if not bad and new >= 4 else 1)
' "$before" && echo PASS || { echo FAIL; exit 1; }
