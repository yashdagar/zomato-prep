package hours

import (
	"slices"
	"testing"
	"time"
)

func at(hh, mm int) time.Time { return time.Date(2026, 10, 24, hh, mm, 0, 0, time.UTC) }

func TestW2B7_OpenAt(t *testing.T) {
	idx := Build([]Slot{
		{1, 8 * 60, 13 * 60},
		{1, 20 * 60, 22*60 + 30},
		{2, 11 * 60, 23 * 60},
		{3, 22 * 60, 3 * 60},
	})
	tests := []struct {
		hh, mm int
		want   []int64
	}{
		{9, 30, []int64{1}},
		{13, 0, []int64{2}},
		{21, 0, []int64{1, 2}},
		{22, 45, []int64{2, 3}},
		{1, 15, []int64{3}},
		{5, 0, nil},
	}
	for _, tc := range tests {
		got := idx.OpenAt(at(tc.hh, tc.mm))
		slices.Sort(got)
		if !slices.Equal(got, tc.want) {
			t.Fatalf("%02d:%02d: got %v, want %v", tc.hh, tc.mm, got, tc.want)
		}
	}
}

func TestW2B7_ManyRestaurantsCrossWordBoundary(t *testing.T) {
	var slots []Slot
	for id := int64(1); id <= 200; id++ {
		slots = append(slots, Slot{id, 10 * 60, 11 * 60})
	}
	if got := len(Build(slots).OpenAt(at(10, 30))); got != 200 {
		t.Fatalf("got %d open, want 200", got)
	}
}
