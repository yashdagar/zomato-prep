package stock

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeTable struct {
	mu       sync.Mutex
	rows     map[int64]Row
	negative atomic.Bool
}

func newTable(onHand map[int64]int) *fakeTable {
	f := &fakeTable{rows: map[int64]Row{}}
	for id, n := range onHand {
		f.rows[id] = Row{OnHand: n}
	}
	return f
}

func (f *fakeTable) Begin(context.Context) (Tx, error) { return &fakeTx{t: f}, nil }

func (f *fakeTable) onHand(id int64) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows[id].OnHand
}

type undo struct {
	id  int64
	qty int
}

type fakeTx struct {
	t    *fakeTable
	undo []undo
	done bool
}

func (x *fakeTx) Get(_ context.Context, id int64) (Row, error) {
	x.t.mu.Lock()
	defer x.t.mu.Unlock()
	return x.t.rows[id], nil
}

func (x *fakeTx) Decrement(_ context.Context, id int64, qty, version int) (bool, error) {
	x.t.mu.Lock()
	defer x.t.mu.Unlock()
	r := x.t.rows[id]
	if r.OnHand < qty || r.Version != version {
		return false, nil
	}
	r.OnHand -= qty
	r.Version++
	if r.OnHand < 0 {
		x.t.negative.Store(true)
	}
	x.t.rows[id] = r
	x.undo = append(x.undo, undo{id, qty})
	return true, nil
}

func (x *fakeTx) Increment(_ context.Context, id int64, qty int) error {
	x.t.mu.Lock()
	defer x.t.mu.Unlock()
	r := x.t.rows[id]
	r.OnHand += qty
	r.Version++
	x.t.rows[id] = r
	return nil
}

func (x *fakeTx) Commit() error {
	x.done = true
	return nil
}

func (x *fakeTx) Rollback() error {
	if x.done {
		return nil
	}
	x.done = true
	x.t.mu.Lock()
	defer x.t.mu.Unlock()
	for _, u := range x.undo {
		r := x.t.rows[u.id]
		r.OnHand += u.qty
		r.Version++
		x.t.rows[u.id] = r
	}
	return nil
}

type fakeHolds struct {
	mu   sync.Mutex
	keys map[string]time.Duration
}

func newHolds() *fakeHolds { return &fakeHolds{keys: map[string]time.Duration{}} }

func (h *fakeHolds) Hold(_ context.Context, key string, _ int, ttl time.Duration) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.keys[key]; ok {
		return false, nil
	}
	h.keys[key] = ttl
	return true, nil
}

func (h *fakeHolds) Release(_ context.Context, key string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.keys, key)
	return nil
}

func (h *fakeHolds) ttl(key string) (time.Duration, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	d, ok := h.keys[key]
	return d, ok
}

func (h *fakeHolds) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.keys)
}

func TestW8B1_ReserveTakesStockAndKeepsAFiveMinuteHold(t *testing.T) {
	table, holds := newTable(map[int64]int{1: 10}), newHolds()
	s := NewService(table, holds)
	if err := s.Reserve(t.Context(), "o-1", []Line{{ItemID: 1, Qty: 3}}); err != nil {
		t.Fatal(err)
	}
	if got := table.onHand(1); got != 7 {
		t.Fatalf("on_hand = %d, want 7", got)
	}
	ttl, ok := holds.ttl("resv:o-1:1")
	if !ok || ttl != 5*time.Minute {
		t.Fatalf("hold resv:o-1:1 = %v, %v; want a 5 minute hold", ttl, ok)
	}
}

func TestW8B1_ReserveFailsWithErrSoldOutWhenStockIsShort(t *testing.T) {
	table := newTable(map[int64]int{1: 2})
	s := NewService(table, newHolds())
	err := s.Reserve(t.Context(), "o-1", []Line{{ItemID: 1, Qty: 3}})
	if !errors.Is(err, ErrSoldOut) {
		t.Fatalf("Reserve() = %v, want ErrSoldOut", err)
	}
	if got := table.onHand(1); got != 2 {
		t.Fatalf("on_hand = %d, want 2 untouched", got)
	}
}

func TestW8B1_AFailedLineRollsBackEarlierLinesAndReleasesEveryHold(t *testing.T) {
	table, holds := newTable(map[int64]int{1: 5, 2: 1}), newHolds()
	s := NewService(table, holds)
	err := s.Reserve(t.Context(), "o-1", []Line{{ItemID: 1, Qty: 2}, {ItemID: 2, Qty: 4}})
	if !errors.Is(err, ErrSoldOut) {
		t.Fatalf("Reserve() = %v, want ErrSoldOut", err)
	}
	if table.onHand(1) != 5 || table.onHand(2) != 1 {
		t.Fatalf("stock = %d and %d, want 5 and 1 after the rollback", table.onHand(1), table.onHand(2))
	}
	if n := holds.count(); n != 0 {
		t.Fatalf("%d holds left, want 0", n)
	}
}

func TestW8B1_ReservingTheSameOrderTwiceTakesStockOnce(t *testing.T) {
	table := newTable(map[int64]int{1: 10})
	s := NewService(table, newHolds())
	lines := []Line{{ItemID: 1, Qty: 4}}
	if err := s.Reserve(t.Context(), "o-1", lines); err != nil {
		t.Fatal(err)
	}
	err := s.Reserve(t.Context(), "o-1", lines)
	if !errors.Is(err, ErrAlreadyReserved) {
		t.Fatalf("second Reserve() = %v, want ErrAlreadyReserved", err)
	}
	if got := table.onHand(1); got != 6 {
		t.Fatalf("on_hand = %d, want 6", got)
	}
}

func TestW8B1_RestockPutsTheUnitsBackAndReleasesTheHolds(t *testing.T) {
	table, holds := newTable(map[int64]int{1: 10, 2: 10}), newHolds()
	s := NewService(table, holds)
	lines := []Line{{ItemID: 1, Qty: 3}, {ItemID: 2, Qty: 2}}
	if err := s.Reserve(t.Context(), "o-1", lines); err != nil {
		t.Fatal(err)
	}
	if err := s.Restock(t.Context(), "o-1", lines); err != nil {
		t.Fatal(err)
	}
	if table.onHand(1) != 10 || table.onHand(2) != 10 {
		t.Fatalf("stock = %d and %d, want 10 and 10", table.onHand(1), table.onHand(2))
	}
	if n := holds.count(); n != 0 {
		t.Fatalf("%d holds left, want 0", n)
	}
	if err := s.Reserve(t.Context(), "o-1", lines); err != nil {
		t.Fatalf("the same order can reserve again after a restock: %v", err)
	}
}

func TestW8B1_FiveHundredCartsRaceForTenUnitsAndExactlyTenWin(t *testing.T) {
	table := newTable(map[int64]int{1: 10})
	s := NewService(table, newHolds())
	var wins, soldOut, other atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			switch err := s.Reserve(context.Background(), fmt.Sprintf("o-%d", i), []Line{{ItemID: 1, Qty: 1}}); {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, ErrSoldOut):
				soldOut.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins.Load() != 10 || soldOut.Load() != 490 || other.Load() != 0 {
		t.Fatalf("wins=%d soldOut=%d other=%d, want 10, 490 and 0", wins.Load(), soldOut.Load(), other.Load())
	}
	if table.onHand(1) != 0 || table.negative.Load() {
		t.Fatalf("on_hand = %d, negative seen = %v; want 0 and never negative", table.onHand(1), table.negative.Load())
	}
}

func TestW8B1_BiggerLinesRaceWithoutOverselling(t *testing.T) {
	table := newTable(map[int64]int{1: 10})
	s := NewService(table, newHolds())
	var wins atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.Reserve(context.Background(), fmt.Sprintf("o-%d", i), []Line{{ItemID: 1, Qty: 3}}) == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 3 || table.onHand(1) != 1 || table.negative.Load() {
		t.Fatalf("wins=%d on_hand=%d, want 3 wins and 1 left", wins.Load(), table.onHand(1))
	}
}

func TestW8B1_RacingCartsWithTwoLinesNeverLeakStock(t *testing.T) {
	table, holds := newTable(map[int64]int{1: 20, 2: 5}), newHolds()
	s := NewService(table, holds)
	var wins atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.Reserve(context.Background(), fmt.Sprintf("o-%d", i), []Line{{ItemID: 1, Qty: 1}, {ItemID: 2, Qty: 1}}) == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 5 {
		t.Fatalf("wins = %d, want 5", wins.Load())
	}
	if table.onHand(1) != 15 || table.onHand(2) != 0 {
		t.Fatalf("stock = %d and %d, want 15 and 0", table.onHand(1), table.onHand(2))
	}
	if n := holds.count(); n != 10 {
		t.Fatalf("%d holds, want 10 (two lines for each of the 5 winners)", n)
	}
}
