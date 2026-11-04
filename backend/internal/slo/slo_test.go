//go:build w6o2

package slo_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/yashdagar/zomato-prep/backend/internal/slo"
)

func buckets(pairs ...float64) []slo.Bucket {
	var out []slo.Bucket
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, slo.Bucket{UpperBound: pairs[i], Count: pairs[i+1]})
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

var inf = math.Inf(1)

func TestW6O2_QuantileInterpolatesInsideTheBucket(t *testing.T) {
	b := buckets(0.1, 50, 0.5, 90, 1, 100, inf, 100)
	cases := map[float64]float64{0.5: 0.1, 0.95: 0.75, 0.99: 0.95, 1: 1}
	for q, want := range cases {
		if got := slo.Quantile(q, b); !near(got, want) {
			t.Fatalf("q=%v: got %v, want %v", q, got, want)
		}
	}
}

func TestW6O2_FirstBucketStartsAtZero(t *testing.T) {
	b := buckets(0.2, 10, inf, 10)
	if got := slo.Quantile(0.5, b); !near(got, 0.1) {
		t.Fatalf("got %v, want 0.1", got)
	}
}

func TestW6O2_QuantileInTheInfBucketReturnsTheLastFiniteBound(t *testing.T) {
	b := buckets(0.1, 10, 1, 20, inf, 30)
	if got := slo.Quantile(0.99, b); !near(got, 1) {
		t.Fatalf("got %v, want 1", got)
	}
}

func TestW6O2_QuantileOutsideZeroToOneIsInfinite(t *testing.T) {
	b := buckets(0.1, 10, inf, 10)
	if !math.IsInf(slo.Quantile(-0.1, b), -1) || !math.IsInf(slo.Quantile(1.1, b), 1) {
		t.Fatal("q below 0 must be -Inf and above 1 must be +Inf")
	}
}

func TestW6O2_NoObservationsIsNaN(t *testing.T) {
	if !math.IsNaN(slo.Quantile(0.5, nil)) || !math.IsNaN(slo.Quantile(0.5, buckets(0.1, 0, inf, 0))) {
		t.Fatal("an empty histogram has no quantile")
	}
}

const exposition = `# HELP http_request_duration_seconds Duration of HTTP requests in seconds.
# TYPE http_request_duration_seconds histogram
http_request_duration_seconds_bucket{method="GET",route="/a",status="200",le="0.1"} 10
http_request_duration_seconds_bucket{method="GET",route="/a",status="200",le="0.5"} 20
http_request_duration_seconds_bucket{method="GET",route="/a",status="200",le="+Inf"} 20
http_request_duration_seconds_sum{method="GET",route="/a",status="200"} 3.5
http_request_duration_seconds_count{method="GET",route="/a",status="200"} 20
http_request_duration_seconds_bucket{method="GET",route="/a",status="500",le="0.1"} 0
http_request_duration_seconds_bucket{method="GET",route="/a",status="500",le="0.5"} 5
http_request_duration_seconds_bucket{method="GET",route="/a",status="500",le="+Inf"} 5
http_request_duration_seconds_bucket{method="GET",route="/b",status="200",le="0.1"} 100
http_request_duration_seconds_bucket{method="GET",route="/b",status="200",le="0.5"} 100
http_request_duration_seconds_bucket{method="GET",route="/b",status="200",le="+Inf"} 100
other_metric_bucket{le="1"} 7
`

func TestW6O2_ParseBucketsSumsSeriesThatMatchTheLabels(t *testing.T) {
	got, err := slo.ParseBuckets(strings.NewReader(exposition), "http_request_duration_seconds", map[string]string{"route": "/a"})
	if err != nil {
		t.Fatal(err)
	}
	want := buckets(0.1, 10, 0.5, 25, inf, 25)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestW6O2_ParseBucketsWithoutLabelsMergesEverySeries(t *testing.T) {
	got, err := slo.ParseBuckets(strings.NewReader(exposition), "http_request_duration_seconds", nil)
	if err != nil {
		t.Fatal(err)
	}
	if last := got[len(got)-1]; !math.IsInf(last.UpperBound, 1) || last.Count != 125 {
		t.Fatalf("got %v, want the +Inf bucket to hold 125", got)
	}
}

func TestW6O2_ParseBucketsIgnoresOtherMetrics(t *testing.T) {
	got, err := slo.ParseBuckets(strings.NewReader(exposition), "other_metric", nil)
	if err != nil || len(got) != 1 || got[0].Count != 7 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestW6O2_ParseBucketsReportsAMissingMetric(t *testing.T) {
	_, err := slo.ParseBuckets(strings.NewReader(exposition), "nope", nil)
	if !errors.Is(err, slo.ErrNoSeries) {
		t.Fatalf("err = %v, want ErrNoSeries", err)
	}
	_, err = slo.ParseBuckets(strings.NewReader(exposition), "http_request_duration_seconds", map[string]string{"route": "/missing"})
	if !errors.Is(err, slo.ErrNoSeries) {
		t.Fatalf("err = %v, want ErrNoSeries for a label that matches nothing", err)
	}
}

func TestW6O2_P99OfTheRoute(t *testing.T) {
	b, err := slo.ParseBuckets(strings.NewReader(exposition), "http_request_duration_seconds", map[string]string{"route": "/a"})
	if err != nil {
		t.Fatal(err)
	}
	want := 0.1 + 0.4*(0.99*25-10)/(25-10)
	if got := slo.Quantile(0.99, b); !near(got, want) {
		t.Fatalf("p99 = %v, want %v", got, want)
	}
}
