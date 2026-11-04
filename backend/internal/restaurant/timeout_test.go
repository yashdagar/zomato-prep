package restaurant_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/yashdagar/zomato-prep/backend/internal/db"
	"github.com/yashdagar/zomato-prep/backend/internal/restaurant"
)

type hangingQueries struct{ *fakeQueries }

func (hangingQueries) ListItemsByRestaurant(ctx context.Context, _ uint64) ([]db.Item, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestW6B3_SlowDatabaseReturns504InsteadOfHanging(t *testing.T) {
	h := restaurant.NewHandler(restaurant.NewService(hangingQueries{&fakeQueries{}}))
	r := chi.NewRouter()
	r.Get("/v1/restaurants/{id}/menu", h.Menu)
	rec := httptest.NewRecorder()
	start := time.Now()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/restaurants/1/menu", nil))
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status %d, want 504", rec.Code)
	}
	if took := time.Since(start); took > 1500*time.Millisecond {
		t.Fatalf("took %v, the read timeout should fire at 500ms", took)
	}
}
