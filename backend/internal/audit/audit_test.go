package audit_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func backendDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestW6B6_MakeAuditRunsVulncheckLintAndRaceTests(t *testing.T) {
	cmd := exec.Command("make", "-n", "audit")
	cmd.Dir = backendDir(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n audit: %v\n%s", err, out)
	}
	for _, want := range []string{"govulncheck ./...", "golangci-lint run", "go test -race"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("make audit does not run %q:\n%s", want, out)
		}
	}
}

func TestW6B6_CIWorkflowRunsGovulncheck(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(backendDir(t), "..", ".github", "workflows", "backend.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "govulncheck") {
		t.Fatal(".github/workflows/backend.yml has no govulncheck step")
	}
	if !strings.Contains(string(b), "-race") {
		t.Fatal(".github/workflows/backend.yml does not run tests with -race")
	}
}

func TestW6B6_SoakScriptSamplesGoroutinesAndHeap(t *testing.T) {
	path := filepath.Join(backendDir(t), "scripts", "soak.sh")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"debug/pprof/goroutine", "debug/pprof/heap", "k6 run"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("soak.sh never references %q", want)
		}
	}
	if out, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("soak.sh does not parse: %v\n%s", err, out)
	}
}
