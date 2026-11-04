package benchreport

import (
	"errors"
	"strings"
	"testing"
)

const sampleK6Summary = `{
  "metrics": {
    "http_req_duration": {"avg": 84.25, "min": 3.1, "med": 70.2, "max": 912.4, "p(90)": 160.8, "p(95)": 212.44},
    "http_reqs": {"count": 18450, "rate": 1230.07},
    "http_req_failed": {"passes": 55, "fails": 18395, "value": 0.00298}
  }
}`

func TestW8B4_ParseK6ReadsRequestsRateLatencyAndErrors(t *testing.T) {
	s, err := ParseK6(strings.NewReader(sampleK6Summary))
	if err != nil {
		t.Fatal(err)
	}
	if s.Requests != 18450 || s.RPS != 1230.07 || s.AvgMs != 84.25 || s.P95Ms != 212.44 || s.ErrorRate != 0.00298 {
		t.Fatalf("ParseK6() = %+v", s)
	}
}

func TestW8B4_ParseK6RefusesInvalidJSON(t *testing.T) {
	if _, err := ParseK6(strings.NewReader("{nope")); err == nil {
		t.Fatal("ParseK6() accepted invalid JSON")
	}
}

func TestW8B4_ParseK6NamesTheMissingMetric(t *testing.T) {
	cases := map[string]string{
		"http_reqs":         `{"metrics":{"http_req_duration":{"avg":1,"p(95)":2},"http_req_failed":{"value":0}}}`,
		"http_req_duration": `{"metrics":{"http_reqs":{"count":1,"rate":1},"http_req_failed":{"value":0}}}`,
		"http_req_failed":   `{"metrics":{"http_reqs":{"count":1,"rate":1},"http_req_duration":{"avg":1,"p(95)":2}}}`,
	}
	for metric, in := range cases {
		t.Run(metric, func(t *testing.T) {
			_, err := ParseK6(strings.NewReader(in))
			if !errors.Is(err, ErrMissingMetric) || !strings.Contains(err.Error(), metric) {
				t.Fatalf("ParseK6() = %v, want ErrMissingMetric naming %s", err, metric)
			}
		})
	}
}

func TestW8B4_TablePutsTheHardwareOnEveryRow(t *testing.T) {
	runs := []Run{
		{Scenario: "browse local", Hardware: "M2 MacBook Air, 8 GB", Summary: Summary{Requests: 18450, RPS: 1230.07, P95Ms: 212.44, ErrorRate: 0.00298}},
		{Scenario: "browse server", Hardware: "2 vCPU, 4 GB VM", Summary: Summary{Requests: 9000, RPS: 450.4, P95Ms: 380, ErrorRate: 0}},
	}
	got, err := Table(runs)
	if err != nil {
		t.Fatal(err)
	}
	want := "| Scenario | Hardware | Requests/s | p95 | Error rate |\n" +
		"| --- | --- | --- | --- | --- |\n" +
		"| browse local | M2 MacBook Air, 8 GB | 1230 | 212.4 ms | 0.30% |\n" +
		"| browse server | 2 vCPU, 4 GB VM | 450 | 380.0 ms | 0.00% |\n"
	if got != want {
		t.Fatalf("Table() =\n%s\nwant\n%s", got, want)
	}
}

func TestW8B4_TableRefusesARowWithoutHardware(t *testing.T) {
	_, err := Table([]Run{
		{Scenario: "ok", Hardware: "laptop"},
		{Scenario: "bad", Hardware: "  "},
	})
	if !errors.Is(err, ErrNoHardware) || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("Table() = %v, want ErrNoHardware naming the scenario", err)
	}
}

func TestW8B4_TableRefusesAnEmptyRunList(t *testing.T) {
	if _, err := Table(nil); err == nil {
		t.Fatal("Table(nil) returned no error")
	}
}
