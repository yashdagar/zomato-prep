package e2e_test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func needDB(t *testing.T, env string) string {
	t.Helper()
	if e := os.Getenv("TEST_STACK_ERROR"); e != "" {
		t.Fatalf("the test stack did not start: %s", e)
	}
	v := os.Getenv(env)
	if v == "" {
		t.Skip("run through make test, which sets " + env)
	}
	return v
}

func openDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatalf("connect to MySQL: %v", err)
	}
	return db
}

func apiDB(t *testing.T) *sql.DB {
	t.Helper()
	return openDB(t, needDB(t, "TEST_MYSQL_DSN"))
}

func dsnFor(root, name string) string {
	base, _, _ := strings.Cut(root, "?")
	return strings.TrimSuffix(base, "/") + "/" + name + "?parseTime=true&multiStatements=true"
}

var scratchSeq int

func scratchDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	root := needDB(t, "TEST_MYSQL_ROOT_DSN")
	admin := openDB(t, dsnFor(root, ""))
	scratchSeq++
	name := fmt.Sprintf("zomato_scratch_%d_%d", os.Getpid(), scratchSeq)
	mustExec(t, admin, "DROP DATABASE IF EXISTS "+name)
	mustExec(t, admin, "CREATE DATABASE "+name)
	t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE IF EXISTS " + name) })
	dsn := dsnFor(root, name)
	return openDB(t, dsn), dsn
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) sql.Result {
	t.Helper()
	r, err := db.ExecContext(t.Context(), q, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return r
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(t.Context(), q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

type migration struct {
	version int
	up      string
	down    string
}

var migrationName = regexp.MustCompile(`^(\d+)_.+\.(up|down)\.sql$`)

func migrations(t *testing.T) []migration {
	t.Helper()
	dir := needDB(t, "TEST_MIGRATIONS_DIR")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	byVersion := map[int]*migration{}
	for _, e := range entries {
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		v, _ := strconv.Atoi(m[1])
		if byVersion[v] == nil {
			byVersion[v] = &migration{version: v}
		}
		p := filepath.Join(dir, e.Name())
		if m[2] == "up" {
			byVersion[v].up = p
		} else {
			byVersion[v].down = p
		}
	}
	out := make([]migration, 0, len(byVersion))
	for _, m := range byVersion {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out
}

func runFile(t *testing.T, db *sql.DB, path string) error {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(src)) == "" {
		return nil
	}
	_, err = db.ExecContext(t.Context(), string(src))
	return err
}

func migrateUp(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, m := range migrations(t) {
		if m.up == "" {
			t.Fatalf("migration %04d has no .up.sql file", m.version)
		}
		if err := runFile(t, db, m.up); err != nil {
			t.Fatalf("%s: %v", filepath.Base(m.up), err)
		}
	}
}

// statements runs fn and returns the SQL that the API's own MySQL user sent
// meanwhile, from MySQL's general log. Tests in this package run one at a
// time, so only the API and its background work talk to MySQL in that window.
func statements(t *testing.T, fn func()) []string {
	t.Helper()
	root := openDB(t, dsnFor(needDB(t, "TEST_MYSQL_ROOT_DSN"), ""))
	user := needDB(t, "TEST_API_DB_USER")
	mustExec(t, root, "SET GLOBAL log_output = 'TABLE'")
	mustExec(t, root, "TRUNCATE TABLE mysql.general_log")
	mustExec(t, root, "SET GLOBAL general_log = 'ON'")
	defer func() { _, _ = root.Exec("SET GLOBAL general_log = 'OFF'") }()
	fn()
	time.Sleep(50 * time.Millisecond)
	mustExec(t, root, "SET GLOBAL general_log = 'OFF'")
	rows, err := root.QueryContext(t.Context(), `
		SELECT CONVERT(argument USING utf8mb4)
		FROM mysql.general_log
		WHERE user_host LIKE ? AND command_type IN ('Query', 'Execute')
		ORDER BY event_time`, user+"[%")
	if err != nil {
		t.Fatalf("read mysql.general_log: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var q string
		if err := rows.Scan(&q); err != nil {
			t.Fatal(err)
		}
		if q = strings.TrimSpace(q); q != "" {
			out = append(out, q)
		}
	}
	return out
}

var dataStatement = regexp.MustCompile(`(?i)^\s*(select|insert|update|delete|replace|with)\b`)

func dataStatements(all []string, skip ...string) []string {
	var out []string
	for _, q := range all {
		if !dataStatement.MatchString(stripComments(q)) {
			continue
		}
		lower := strings.ToLower(q)
		skipped := false
		for _, s := range skip {
			if strings.Contains(lower, s) {
				skipped = true
			}
		}
		if !skipped {
			out = append(out, q)
		}
	}
	return out
}
