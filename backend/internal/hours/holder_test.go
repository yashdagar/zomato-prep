package hours

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestW2B7_HolderIsEmptyUntilTheFirstRebuild(t *testing.T) {
	h := NewHolder(func(context.Context) ([]Slot, error) { return []Slot{{1, 0, 1440}}, nil })
	if h.Current() != nil {
		t.Fatal("Current() before any Rebuild should be nil")
	}
	if err := h.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := h.Current().OpenAt(at(12, 0)); len(got) != 1 || got[0] != 1 {
		t.Fatalf("after Rebuild, open at noon = %v, want [1]", got)
	}
}

func TestW2B7_RebuildSwapsInTheNewSlots(t *testing.T) {
	slots := []Slot{{1, 9 * 60, 10 * 60}}
	h := NewHolder(func(context.Context) ([]Slot, error) { return slots, nil })
	_ = h.Rebuild(context.Background())
	old := h.Current()
	slots = []Slot{{2, 9 * 60, 10 * 60}}
	_ = h.Rebuild(context.Background())
	if got := h.Current().OpenAt(at(9, 30)); len(got) != 1 || got[0] != 2 {
		t.Fatalf("after the second Rebuild, open at 09:30 = %v, want [2]", got)
	}
	if got := old.OpenAt(at(9, 30)); len(got) != 1 || got[0] != 1 {
		t.Fatalf("an index a reader already holds changed under it: %v, want [1]", got)
	}
}

func TestW2B7_FailedRebuildKeepsThePreviousIndex(t *testing.T) {
	fail := false
	h := NewHolder(func(context.Context) ([]Slot, error) {
		if fail {
			return nil, errors.New("mysql down")
		}
		return []Slot{{1, 0, 1440}}, nil
	})
	_ = h.Rebuild(context.Background())
	fail = true
	if err := h.Rebuild(context.Background()); err == nil {
		t.Fatal("Rebuild should return the load error")
	}
	if h.Current() == nil || len(h.Current().OpenAt(at(12, 0))) != 1 {
		t.Fatal("a failed Rebuild must keep serving the previous index")
	}
}

func TestW2B7_RunStopsWhenTheContextIsCancelled(t *testing.T) {
	h := NewHolder(func(context.Context) ([]Slot, error) { return nil, nil })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.Run(ctx, time.Millisecond)
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1 s of the context being cancelled")
	}
}
