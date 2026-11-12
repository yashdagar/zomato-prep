package e2e_test

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type proc struct {
	t      *testing.T
	name   string
	cmd    *exec.Cmd
	out    *syncBuffer
	done   chan struct{}
	err    error
	exited time.Time
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func apiEnv(t *testing.T, overrides ...string) []string {
	t.Helper()
	file := needDB(t, "TEST_API_ENV_FILE")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	env := os.Environ()
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if l != "" {
			env = append(env, l)
		}
	}
	return append(env, overrides...)
}

func binary(t *testing.T, env, errEnv string) string {
	t.Helper()
	if e := os.Getenv("TEST_API_ERROR"); e != "" {
		t.Fatalf("the API did not start: %s", e)
	}
	if e := os.Getenv(errEnv); e != "" {
		t.Fatal(e)
	}
	return needDB(t, env)
}

func startProc(t *testing.T, name, bin string, env []string) *proc {
	t.Helper()
	p := &proc{t: t, name: name, out: &syncBuffer{}, done: make(chan struct{})}
	p.cmd = exec.Command(bin)
	p.cmd.Env = env
	p.cmd.Stdout, p.cmd.Stderr = p.out, p.out
	if err := p.cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	go func() {
		p.err = p.cmd.Wait()
		p.exited = time.Now()
		close(p.done)
	}()
	t.Cleanup(func() {
		p.signal(syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(15 * time.Second):
			_ = p.cmd.Process.Kill()
			<-p.done
		}
		if strings.Contains(p.out.String(), "WARNING: DATA RACE") {
			t.Errorf("data race in %s:\n%s", name, raceReport(p.out.String()))
		}
	})
	return p
}

func raceReport(out string) string {
	i := strings.Index(out, "WARNING: DATA RACE")
	lines := strings.Split(out[i:], "\n")
	if len(lines) > 30 {
		lines = lines[:30]
	}
	return strings.Join(lines, "\n")
}

func (p *proc) signal(sig syscall.Signal) {
	select {
	case <-p.done:
	default:
		_ = p.cmd.Process.Signal(sig)
	}
}

// stop sends SIGTERM and returns the exit code and how long the process took
// to exit, or -1 if it was still running after the timeout.
func (p *proc) stop(timeout time.Duration) (int, time.Duration) {
	start := time.Now()
	p.signal(syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(timeout):
		return -1, timeout
	}
	var exit *exec.ExitError
	if errors.As(p.err, &exit) {
		return exit.ExitCode(), p.exited.Sub(start)
	}
	return 0, p.exited.Sub(start)
}

func (p *proc) kill() {
	p.signal(syscall.SIGKILL)
	<-p.done
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// startAPI runs a second instance of the API on its own address, with the
// same settings as the one make test started plus the overrides.
func startAPI(t *testing.T, overrides ...string) (string, *proc) {
	t.Helper()
	bin := binary(t, "TEST_API_BIN", "TEST_API_ERROR")
	addr := freeAddr(t)
	p := startProc(t, "API on "+addr, bin, apiEnv(t, append(overrides, "HTTP_ADDR="+addr)...))
	deadline := time.Now().Add(30 * time.Second)
	for {
		if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			_ = c.Close()
			return "http://" + addr, p
		}
		select {
		case <-p.done:
			t.Fatalf("the API started with HTTP_ADDR=%s exited: %s", addr, lastOutput(p.out.String()))
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the API did not listen on HTTP_ADDR=%s within 30s; read the address from HTTP_ADDR", addr)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func startWorker(t *testing.T, overrides ...string) *proc {
	t.Helper()
	bin := binary(t, "TEST_WORKER_BIN", "TEST_WORKER_ERROR")
	kafka(t)
	return startProc(t, "worker", bin, apiEnv(t, overrides...))
}

func lastOutput(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 8 {
		lines = lines[len(lines)-8:]
	}
	return strings.Join(lines, "\n")
}

func clientFor(t *testing.T, base string) *client {
	t.Helper()
	c := newClient(t)
	c.base = base
	return c
}

func rawRequest(t *testing.T, c *client, method, path, body string, headers ...string) resp {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, c.base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", c.limitKey)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return resp{Status: res.StatusCode, Header: res.Header, Body: b}
}
