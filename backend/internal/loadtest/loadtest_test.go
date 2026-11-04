package loadtest_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func script(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "loadtest", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("backend/loadtest/%s does not exist yet", name)
	}
	return p
}

func needK6(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("k6")
	if err != nil {
		t.Skip("k6 is not installed (brew install k6)")
	}
	return p
}

func inspect(t *testing.T, name string) map[string]any {
	t.Helper()
	k6 := needK6(t)
	out, err := exec.Command(k6, "inspect", script(t, name)).Output()
	if err != nil {
		t.Fatalf("k6 inspect %s: %v", name, err)
	}
	var opts map[string]any
	if err := json.Unmarshal(out, &opts); err != nil {
		t.Fatalf("k6 inspect output is not JSON: %v\n%s", err, out)
	}
	return opts
}

func thresholds(opts map[string]any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(opts["thresholds"])
	return buf.String()
}

func TestW6B4_BrowseDefaultsToThreeHundredVUsForThreeMinutes(t *testing.T) {
	opts := inspect(t, "browse.js")
	d, _ := time.ParseDuration(fmt.Sprint(opts["duration"]))
	if opts["vus"] != float64(300) || d != 3*time.Minute {
		t.Fatalf("browse.js options vus=%v duration=%v, want 300 and 3m", opts["vus"], opts["duration"])
	}
}

func TestW6B4_BrowseAbortsOnErrorsAndAssertsP99(t *testing.T) {
	th := thresholds(inspect(t, "browse.js"))
	for _, want := range []string{"http_req_failed", "http_req_duration", "p(99)<300", "abortOnFail"} {
		if !strings.Contains(th, want) {
			t.Fatalf("browse.js thresholds %s lack %q", th, want)
		}
	}
}

func TestW6B4_OrderRampsToOneHundredPerSecondOverTwoMinutes(t *testing.T) {
	opts := inspect(t, "order.js")
	scenarios, _ := opts["scenarios"].(map[string]any)
	if len(scenarios) != 1 {
		t.Fatalf("order.js should define one scenario, got %v", opts["scenarios"])
	}
	for _, v := range scenarios {
		sc := v.(map[string]any)
		if sc["executor"] != "ramping-arrival-rate" {
			t.Fatalf("executor %v, want ramping-arrival-rate", sc["executor"])
		}
		peak := 0.0
		var total time.Duration
		for _, s := range sc["stages"].([]any) {
			st := s.(map[string]any)
			if tg := st["target"].(float64); tg > peak {
				peak = tg
			}
			d, err := time.ParseDuration(st["duration"].(string))
			if err != nil {
				t.Fatal(err)
			}
			total += d
		}
		if peak != 100 || total != 2*time.Minute {
			t.Fatalf("stages peak %v over %v, want 100 per second over 2m", peak, total)
		}
	}
}

func TestW6B4_ScriptsReportP99(t *testing.T) {
	for _, name := range []string{"browse.js", "order.js"} {
		opts := inspect(t, name)
		b, _ := json.Marshal(opts["summaryTrendStats"])
		if !strings.Contains(string(b), "p(99)") {
			t.Fatalf("%s summaryTrendStats %s lacks p(99)", name, b)
		}
	}
}

type summary struct {
	Metrics map[string]map[string]any `json:"metrics"`
}

func (s summary) val(metric, key string) float64 {
	v, _ := s.Metrics[metric][key].(float64)
	return v
}

func runK6(t *testing.T, name string, env ...string) summary {
	t.Helper()
	k6 := needK6(t)
	api := os.Getenv("TEST_API_URL")
	if api == "" {
		t.Skip("set TEST_API_URL=http://localhost:8080 to run")
	}
	out := filepath.Join(t.TempDir(), "summary.json")
	cmd := exec.Command(k6, "run", "--quiet", "--summary-export="+out, script(t, name))
	cmd.Env = append(os.Environ(), append([]string{"BASE_URL=" + api}, env...)...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Run(); err != nil {
		t.Fatalf("k6 run %s: %v\n%s", name, err, buf.String())
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var s summary
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestW6B4_BrowseRunsAgainstTheAPIWithoutFailedChecks(t *testing.T) {
	s := runK6(t, "browse.js", "VUS=5", "DURATION=6s")
	if n := s.val("http_reqs", "count"); n < 10 {
		t.Fatalf("only %v requests in the run", n)
	}
	if f := s.val("checks", "fails"); f != 0 {
		t.Fatalf("%v checks failed", f)
	}
}

func TestW6B4_OrderRunCreatesADistinctOrderPerIteration(t *testing.T) {
	s := runK6(t, "order.js", "PEAK=5", "RAMP=2s", "HOLD=4s")
	if n := s.val("http_reqs", "count"); n < 8 {
		t.Fatalf("only %v requests in the run", n)
	}
	if f := s.val("checks", "fails"); f != 0 {
		t.Fatalf("%v checks failed; a 200 instead of 201 means an idempotency key was reused", f)
	}
}

func saved(t *testing.T, name string) summary {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "loadtest", name))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("backend/loadtest/%s is missing: save the k6 summary with --summary-export", name)
	}
	var s summary
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("%s is not a k6 summary: %v", name, err)
	}
	return s
}

func TestW6B4_BeforeSummariesRecordPercentilesAndErrorRate(t *testing.T) {
	for _, name := range []string{"before-browse.json", "before-order.json"} {
		s := saved(t, name)
		for _, k := range []string{"med", "p(95)", "p(99)"} {
			if s.val("http_req_duration", k) <= 0 {
				t.Fatalf("%s: http_req_duration %s missing", name, k)
			}
		}
		if _, ok := s.Metrics["http_req_failed"]; !ok {
			t.Fatalf("%s: http_req_failed missing", name)
		}
	}
}

func TestW6B5_AfterBrowseIsNotWorseThanBefore(t *testing.T) {
	before := saved(t, "before-browse.json").val("http_req_duration", "p(95)")
	after := saved(t, "after-browse.json").val("http_req_duration", "p(95)")
	if after > before*1.2 {
		t.Fatalf("after p95 %.1f ms is more than 20 percent worse than before %.1f ms", after, before)
	}
}

func TestW6B5_AfterBrowseP99IsUnder300ms(t *testing.T) {
	after := saved(t, "after-browse.json").val("http_req_duration", "p(99)")
	if after >= 300 {
		t.Fatalf("after p99 %.1f ms, want under 300 ms", after)
	}
}

func TestW6B5_AfterBrowseHasNoServerErrors(t *testing.T) {
	after := saved(t, "after-browse.json").val("http_req_failed", "value")
	if after > 0.01 {
		t.Fatalf("after error rate %.4f, want at most 1 percent", after)
	}
}

func TestW6B5_ReadmeRecordsTheBeforeAndAfterNumbers(t *testing.T) {
	for _, rel := range []string{"README.md", filepath.Join("backend", "loadtest", "README.md")} {
		b, err := os.ReadFile(filepath.Join("..", "..", "..", rel))
		if err != nil {
			continue
		}
		text := strings.ToLower(string(b))
		i := strings.Index(text, "before and after")
		if i < 0 {
			continue
		}
		section := text[i:]
		ok := true
		for _, w := range []string{"p50", "p95", "p99", "error"} {
			if !strings.Contains(section, w) {
				ok = false
			}
		}
		if ok {
			return
		}
	}
	t.Fatal("no README.md or backend/loadtest/README.md has a Before and after section with p50, p95, p99 and the error rate")
}
