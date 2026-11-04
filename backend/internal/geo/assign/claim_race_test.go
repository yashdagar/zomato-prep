package assign_test

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestTwoRidersRaceForOneClaim(t *testing.T) {
	service, _ := riders(t)

	orderID := time.Now().UnixNano()
	riderIDs := []int64{101, 202}

	type bcClaimResult struct {
		riderID int64
		won     bool
		err     error
	}

	start := make(chan struct{})
	results := make(chan bcClaimResult, len(riderIDs))

	var wg sync.WaitGroup

	for _, riderID := range riderIDs {
		wg.Add(1)

		go func(riderID int64) {
			defer wg.Done()

			<-start

			won, err := service.Claim(t.Context(), orderID, riderID)

			results <- bcClaimResult{
				riderID: riderID,
				won:     won,
				err:     err,
			}
		}(riderID)
	}

	close(start)

	wg.Wait()
	close(results)

	winnerCount := 0
	var winnerID int64

	for result := range results {
		if result.err != nil {
			t.Fatalf("rider %d claim failed: %v", result.riderID, result.err)
		}

		if result.won {
			winnerCount++
			winnerID = result.riderID
		}
	}

	if winnerCount != 1 {
		t.Fatalf("got %d winners, want exactly 1", winnerCount)
	}

	claimedBy, err := service.ClaimedBy(t.Context(), orderID)
	if err != nil {
		t.Fatalf("read claim for order %d: %v", orderID, err)
	}

	if claimedBy != winnerID {
		t.Fatalf(
			"order %d claimed by rider %d, want winning rider %d",
			orderID,
			claimedBy,
			winnerID,
		)
	}

	if claimedBy != riderIDs[0] && claimedBy != riderIDs[1] {
		t.Fatalf("order %d claimed by unexpected rider %d", orderID, claimedBy)
	}

	_ = fmt.Sprintf("%d", orderID)
}
