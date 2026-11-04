//go:build w8o3

package percentile

import (
	"errors"
	"testing"
)

func seq(n int) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = float64(n - i)
	}
	return s
}

func TestW8O3_NearestRankOnOneToOneHundred(t *testing.T) {
	cases := map[float64]float64{50: 50, 90: 90, 95: 95, 99: 99, 100: 100, 1: 1}
	for p, want := range cases {
		got, err := Nearest(seq(100), p)
		if err != nil || got != want {
			t.Fatalf("Nearest(p=%v) = %v, %v; want %v", p, got, err, want)
		}
	}
}

func TestW8O3_NearestRankRoundsTheRankUp(t *testing.T) {
	s := []float64{15, 20, 35, 40, 50}
	cases := map[float64]float64{30: 20, 40: 20, 50: 35, 75: 40, 100: 50}
	for p, want := range cases {
		if got, err := Nearest(s, p); err != nil || got != want {
			t.Fatalf("Nearest(p=%v) = %v, %v; want %v", p, got, err, want)
		}
	}
}

func TestW8O3_NearestDoesNotReorderTheCallersSlice(t *testing.T) {
	s := []float64{3, 1, 2}
	if _, err := Nearest(s, 50); err != nil {
		t.Fatal(err)
	}
	if s[0] != 3 || s[1] != 1 || s[2] != 2 {
		t.Fatalf("input slice changed to %v", s)
	}
}

func TestW8O3_ASingleSampleIsEveryPercentile(t *testing.T) {
	for _, p := range []float64{1, 50, 99, 100} {
		if got, err := Nearest([]float64{42}, p); err != nil || got != 42 {
			t.Fatalf("Nearest(p=%v) = %v, %v; want 42", p, got, err)
		}
	}
}

func TestW8O3_BadInputIsRefused(t *testing.T) {
	if _, err := Nearest(nil, 50); !errors.Is(err, ErrNoSamples) {
		t.Fatalf("no samples: err = %v, want ErrNoSamples", err)
	}
	for _, p := range []float64{0, -1, 100.5} {
		if _, err := Nearest([]float64{1, 2}, p); !errors.Is(err, ErrBadPercentile) {
			t.Fatalf("p=%v: err = %v, want ErrBadPercentile", p, err)
		}
	}
}

func TestW8O3_SummarizeReportsTheUsualLatencyNumbers(t *testing.T) {
	st, err := Summarize(seq(100))
	if err != nil {
		t.Fatal(err)
	}
	want := Stats{Count: 100, Min: 1, Max: 100, Mean: 50.5, P50: 50, P95: 95, P99: 99}
	if st != want {
		t.Fatalf("Summarize() = %+v, want %+v", st, want)
	}
}

func TestW8O3_SummarizeRefusesNoSamples(t *testing.T) {
	if _, err := Summarize(nil); !errors.Is(err, ErrNoSamples) {
		t.Fatalf("err = %v, want ErrNoSamples", err)
	}
}
