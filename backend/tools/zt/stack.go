package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const (
	e2eDB        = "zomato_e2e"
	apiUser      = "zomato_api"
	apiPassword  = "zomato_api"
	apiAddr      = "127.0.0.1:8080"
	refreshEvery = "2s"
	cursorSecret = "e2e-cursor-secret-0123456789abcdef"
	jwtSecret    = "e2e-jwt-secret-0123456789abcdef0123456789"
)

var resetTopics = []string{"order-events", "order-events.retry", "order-events.dlq"}

var migrationFile = regexp.MustCompile(`^(\d+)_.+\.up\.sql$`)

type stack struct {
	steps  []string
	err    string
	apiErr string
	api    *exec.Cmd
	exited chan struct{}
	log    string
	dir    string
}

func rootDSN() string {
	if d := os.Getenv("TEST_MYSQL_ROOT_DSN"); d != "" {
		return d
	}
	return "root:" + envOr("MYSQL_ROOT_PASSWORD", "dev") + "@tcp(127.0.0.1:3306)/"
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func withParams(dsn, db string) string {
	base, _, _ := strings.Cut(dsn, "?")
	base = strings.TrimSuffix(base, "/") + "/" + db
	return base + "?parseTime=true&multiStatements=true"
}

func startStack() *stack {
	st := &stack{}
	dir, err := os.MkdirTemp("", "zt-stack-")
	if err != nil {
		st.err = err.Error()
		return st
	}
	st.dir = dir
	st.export()

	root := withParams(rootDSN(), "")
	db, err := sql.Open("mysql", root)
	if err != nil {
		st.err = err.Error()
		return st
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err = db.PingContext(ctx)
	cancel()
	if err != nil {
		st.err = "MySQL is not reachable at 127.0.0.1:3306; start it with make up"
		return st
	}
	setup := []string{
		"DROP DATABASE IF EXISTS " + e2eDB,
		"CREATE DATABASE " + e2eDB,
		fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%%' IDENTIFIED BY '%s'", apiUser, apiPassword),
		fmt.Sprintf("GRANT ALL ON %s.* TO '%s'@'%%'", e2eDB, apiUser),
		"SET GLOBAL general_log = 'OFF'",
		"SET GLOBAL log_output = 'TABLE'",
	}
	for _, q := range setup {
		if _, err := db.Exec(q); err != nil {
			st.err = fmt.Sprintf("preparing the test database: %v", err)
			return st
		}
	}

	if err := resetKafka(); err != nil {
		_ = os.Setenv("TEST_KAFKA_ERROR", err.Error())
	} else {
		st.steps = append(st.steps, "Kafka topics reset")
	}

	n, err := migrate(withParams(rootDSN(), e2eDB), "db/migrations")
	if err != nil {
		st.apiErr = err.Error()
		return st
	}
	st.steps = append(st.steps, fmt.Sprintf("%d migrations", n))

	if exists("cmd/seed") {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		cmd := exec.CommandContext(ctx, "go", "run", "./cmd/seed")
		cmd.Env = append(os.Environ(), "MYSQL_DSN="+withParams(rootDSN(), e2eDB))
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			st.apiErr = "go run ./cmd/seed failed: " + lastLines(string(out), 6)
			return st
		}
		st.steps = append(st.steps, "seeded")
	}

	if !exists("cmd/api") {
		st.apiErr = "cmd/api is not written yet"
		return st
	}
	if c, err := net.DialTimeout("tcp", apiAddr, 300*time.Millisecond); err == nil {
		_ = c.Close()
		st.apiErr = "something is already listening on :8080; stop your API, or set TEST_API_URL=http://localhost:8080 to test the one that is running"
		return st
	}
	bin := filepath.Join(dir, "api")
	build := exec.Command("go", "build", "-race", "-o", bin, "./cmd/api")
	if out, err := build.CombinedOutput(); err != nil {
		st.apiErr = "go build ./cmd/api failed: " + lastLines(string(out), 6)
		return st
	}
	if exists("cmd/worker") {
		worker := filepath.Join(dir, "worker")
		build := exec.Command("go", "build", "-race", "-o", worker, "./cmd/worker")
		if out, err := build.CombinedOutput(); err != nil {
			_ = os.Setenv("TEST_WORKER_ERROR", "go build ./cmd/worker failed: "+lastLines(string(out), 6))
		} else {
			_ = os.Setenv("TEST_WORKER_BIN", worker)
		}
	} else {
		_ = os.Setenv("TEST_WORKER_ERROR", "cmd/worker is not written yet")
	}
	env := []string{
		"MYSQL_DSN=" + fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true", apiUser, apiPassword, mysqlHost(), e2eDB),
		"REDIS_ADDR=" + envOr("REDIS_ADDR", "127.0.0.1:6379"),
		"KAFKA_BROKERS=" + kafkaBrokers(),
		"JWT_SECRET=" + jwtSecret,
		"HOURS_REFRESH_INTERVAL=" + refreshEvery,
		"CURSOR_SECRET=" + cursorSecret,
		"LOGIN_LIMIT_PER_IP=100000",
		"LOGIN_LIMIT_PER_EMAIL=100000",
	}
	envFile := filepath.Join(dir, "api.env")
	if err := os.WriteFile(envFile, []byte(strings.Join(env, "\n")+"\n"), 0o600); err != nil {
		st.apiErr = err.Error()
		return st
	}
	_ = os.Setenv("TEST_API_ENV_FILE", envFile)
	_ = os.Setenv("TEST_API_BIN", bin)
	st.log = filepath.Join(dir, "api.log")
	logf, err := os.Create(st.log)
	if err != nil {
		st.apiErr = err.Error()
		return st
	}
	st.api = exec.Command(bin)
	st.api.Stdout, st.api.Stderr = logf, logf
	st.api.Env = append(append(os.Environ(), env...), "HTTP_ADDR="+apiAddr)
	if err := st.api.Start(); err != nil {
		st.apiErr = err.Error()
		return st
	}
	st.exited = make(chan struct{})
	go func() { _ = st.api.Wait(); _ = logf.Close(); close(st.exited) }()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if c, err := net.DialTimeout("tcp", apiAddr, 200*time.Millisecond); err == nil {
			_ = c.Close()
			break
		}
		select {
		case <-st.exited:
			st.apiErr = "the API exited at startup: " + lastLines(readFile(st.log), 6)
			return st
		default:
		}
		if time.Now().After(deadline) {
			st.apiErr = "the API did not listen on :8080 within 30s"
			st.stop()
			return st
		}
		time.Sleep(100 * time.Millisecond)
	}
	st.steps = append(st.steps, "API on :8080 with the race detector")
	return st
}

func (st *stack) export() {
	_ = os.Setenv("TEST_API_URL", "http://"+apiAddr)
	_ = os.Setenv("TEST_MYSQL_DSN", withParams(rootDSN(), e2eDB))
	_ = os.Setenv("TEST_MYSQL_ROOT_DSN", rootDSN())
	_ = os.Setenv("TEST_API_DB_USER", apiUser)
	_ = os.Setenv("TEST_HOURS_REFRESH_INTERVAL", refreshEvery)
	_ = os.Setenv("TEST_MIGRATIONS_DIR", mustAbs("db/migrations"))
	_ = os.Setenv("TEST_BACKEND_DIR", mustAbs("."))
	_ = os.Setenv("TEST_KAFKA_BROKERS", kafkaBrokers())
	_ = os.Setenv("TEST_JWT_SECRET", jwtSecret)
}

func (st *stack) fail() {
	if st.err != "" {
		_ = os.Setenv("TEST_STACK_ERROR", st.err)
	}
	if e := st.err + st.apiErr; e != "" {
		_ = os.Setenv("TEST_API_ERROR", e)
	}
}

func (st *stack) stop() {
	if st.api == nil || st.api.Process == nil {
		return
	}
	_ = st.api.Process.Signal(syscall.SIGTERM)
	select {
	case <-st.exited:
	case <-time.After(10 * time.Second):
		_ = st.api.Process.Kill()
		<-st.exited
	}
}

func (st *stack) races() []string {
	if st.log == "" {
		return nil
	}
	var out []string
	inRace := false
	sc := bufio.NewScanner(strings.NewReader(readFile(st.log)))
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "WARNING: DATA RACE") {
			inRace = true
		}
		if inRace {
			out = append(out, l)
			if strings.HasPrefix(l, "==================") && len(out) > 1 {
				inRace = false
			}
		}
	}
	return out
}

func (st *stack) cleanup() {
	if st.dir != "" {
		_ = os.RemoveAll(st.dir)
	}
}

func migrate(dsn, dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, nil
	}
	type mig struct {
		version int
		name    string
	}
	var ups []mig
	for _, e := range entries {
		if m := migrationFile.FindStringSubmatch(e.Name()); m != nil {
			v, _ := strconv.Atoi(m[1])
			ups = append(ups, mig{v, e.Name()})
		}
	}
	sort.Slice(ups, func(i, j int) bool { return ups[i].version < ups[j].version })
	for i := 1; i < len(ups); i++ {
		if ups[i].version == ups[i-1].version {
			return 0, fmt.Errorf("two migrations share version %d: %s and %s", ups[i].version, ups[i-1].name, ups[i].name)
		}
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return 0, err
	}
	defer func() { _ = db.Close() }()
	for _, m := range ups {
		src, err := os.ReadFile(filepath.Join(dir, m.name))
		if err != nil {
			return 0, err
		}
		if strings.TrimSpace(string(src)) == "" {
			continue
		}
		if _, err := db.Exec(string(src)); err != nil {
			return 0, fmt.Errorf("migration %s failed: %v", m.name, err)
		}
	}
	return len(ups), nil
}

func mysqlHost() string {
	d := rootDSN()
	if i := strings.Index(d, "@tcp("); i >= 0 {
		rest := d[i+5:]
		if j := strings.Index(rest, ")"); j >= 0 {
			return rest[:j]
		}
	}
	return "127.0.0.1:3306"
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func mustAbs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

func readFile(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

func showStack(w io.Writer, s style, st *stack) {
	if e := st.err + st.apiErr; e != "" {
		pf(w, "  %s  %s\n", s.red("stack"), strings.Join(append(st.steps, e), " · "))
		if st.log != "" {
			pf(w, "         %s\n", s.dim("API log: "+lastLines(readFile(st.log), 3)))
		}
		ln(w)
		return
	}
	pf(w, "  %s  %s\n\n", s.dim("stack"), strings.Join(st.steps, " · "))
}
