//go:build w2o3

package restaurant_test

import (
	"math"
	"testing"

	"github.com/yashdagar/zomato-prep/backend/internal/restaurant"
)

func TestW2O3_HaversineKnownDistances(t *testing.T) {
	cases := []struct {
		name                   string
		lat1, lng1, lat2, lng2 float64
		want, tol              float64
	}{
		{"same point", 28.46, 77.03, 28.46, 77.03, 0, 1e-9},
		{"one degree of latitude", 10, 20, 11, 20, 111.195, 0.2},
		{"delhi to mumbai", 28.6139, 77.2090, 19.0760, 72.8777, 1148, 5},
		{"across the equator", -0.5, 30, 0.5, 30, 111.195, 0.2},
	}
	for _, c := range cases {
		if got := restaurant.HaversineKm(c.lat1, c.lng1, c.lat2, c.lng2); math.Abs(got-c.want) > c.tol {
			t.Errorf("%s: HaversineKm = %.3f, want %.3f +- %.3f", c.name, got, c.want, c.tol)
		}
	}
}

func TestW2O3_HaversineIsSymmetric(t *testing.T) {
	a := restaurant.HaversineKm(28.6139, 77.2090, 19.0760, 72.8777)
	b := restaurant.HaversineKm(19.0760, 72.8777, 28.6139, 77.2090)
	if math.Abs(a-b) > 1e-9 {
		t.Fatalf("HaversineKm(a, b) = %v but HaversineKm(b, a) = %v", a, b)
	}
}

func TestW2O3_BoundingBoxContainsEveryPointWithinTheRadius(t *testing.T) {
	for _, lat := range []float64{0, 28.46, -33.9, 60} {
		const lng, radius = 77.03, 5.0
		box := restaurant.BoundingBox(lat, lng, radius)
		for dLat := -0.12; dLat <= 0.12; dLat += 0.004 {
			for dLng := -0.2; dLng <= 0.2; dLng += 0.004 {
				p, q := lat+dLat, lng+dLng
				if restaurant.HaversineKm(lat, lng, p, q) > radius {
					continue
				}
				if p < box.MinLat || p > box.MaxLat || q < box.MinLng || q > box.MaxLng {
					t.Fatalf("lat %.2f: point (%.4f, %.4f) is %.3f km away but outside %+v",
						lat, p, q, restaurant.HaversineKm(lat, lng, p, q), box)
				}
			}
		}
	}
}

func TestW2O3_BoundingBoxIsTight(t *testing.T) {
	for _, lat := range []float64{0, 28.46, -33.9, 60} {
		const radius = 5.0
		box := restaurant.BoundingBox(lat, 77.03, radius)
		halfLat := (box.MaxLat - box.MinLat) / 2 * 111.195
		halfLng := (box.MaxLng - box.MinLng) / 2 * 111.195 * math.Cos(lat*math.Pi/180)
		if math.Abs(halfLat-radius) > 0.05 || math.Abs(halfLng-radius) > 0.1 {
			t.Fatalf("lat %.2f: box %+v spans %.3f km north-south and %.3f km east-west from the centre, want %.1f", lat, box, halfLat, halfLng, radius)
		}
	}
}
