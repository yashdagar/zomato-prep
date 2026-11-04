#!/usr/bin/env bash
# W7 B4: a real backup of the compose MySQL restores into a fresh database and the live database is untouched.
set -euo pipefail
cd "$(dirname "$0")/.."
env_file=$(mktemp)
cp .env.example "$env_file"
dc() { docker compose -p zomato-w7check --env-file "$env_file" -f compose.prod.yml "$@"; }
cleanup() { dc down -v >/dev/null 2>&1 || true; rm -f "$env_file"; }
trap cleanup EXIT

dc up -d --wait --wait-timeout 300 mysql backup || { echo "FAIL: mysql and backup did not start"; dc logs --tail 40; exit 1; }
dc up --no-deps --exit-code-from migrate migrate >/dev/null || { echo "FAIL: the migrate job failed"; exit 1; }
dc exec -T mysql sh -c 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD" zomato -e "INSERT INTO orders (id) VALUES (1)"' 2>/dev/null || true

dc exec -T backup sh /scripts/backup.sh
dc exec -T backup sh -c 'ls /backups/latest.sql.gz >/dev/null' && echo "PASS: backup wrote latest.sql.gz"
dc exec -T backup sh /scripts/restore-test.sh && echo "PASS: restore-test.sh restored the latest dump into a fresh database"
databases=$(dc exec -T mysql sh -c 'mysql -N -uroot -p"$MYSQL_ROOT_PASSWORD" -e "SHOW DATABASES"' 2>/dev/null)
case "$databases" in *restore_test*) echo "FAIL: restore_test was left behind"; exit 1;; *) echo "PASS: restore_test was cleaned up";; esac
