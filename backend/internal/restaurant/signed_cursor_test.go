//go:build w2o1

package restaurant_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/yashdagar/zomato-prep/backend/internal/cursor"
	"github.com/yashdagar/zomato-prep/backend/internal/db"
	"github.com/yashdagar/zomato-prep/backend/internal/restaurant"
)

type o1Queries struct {
	restaurant.Queries
	total uint64
}

func (q o1Queries) ListRestaurantsNear(_ context.Context, arg db.ListRestaurantsNearParams) ([]db.ListRestaurantsNearRow, error) {
	var rows []db.ListRestaurantsNearRow
	for id := arg.AfterID + 1; id <= q.total && len(rows) < int(arg.Limit); id++ {
		rows = append(rows, db.ListRestaurantsNearRow{ID: id, Name: "Restaurant", Lat: "28.460000", Lng: "77.030000"})
	}
	return rows, nil
}

type o1Page struct {
	Items []struct {
		ID int64 `json:"id"`
	} `json:"items"`
	NextCursor string `json:"next_cursor"`
}

func o1Secret(b byte) []byte {
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = b
	}
	return secret
}

func o1Signer(t *testing.T, b byte) *cursor.Signer {
	t.Helper()
	s, err := cursor.NewSigner(o1Secret(b))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func o1Get(t *testing.T, h *restaurant.Handler, cur string) (int, string) {
	t.Helper()
	q := url.Values{"lat": {"28.46"}, "lng": {"77.03"}, "radius_km": {"5"}, "limit": {"3"}}
	if cur != "" {
		q.Set("cursor", cur)
	}
	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/restaurants?"+q.Encode(), nil))
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

func o1Handler(s *cursor.Signer) *restaurant.Handler {
	svc := restaurant.NewService(o1Queries{total: 8})
	if s != nil {
		svc = svc.WithCursorSigner(s)
	}
	return restaurant.NewHandler(svc)
}

func TestW2O1_SignedWalkVisitsEveryRestaurantOnce(t *testing.T) {
	h := o1Handler(o1Signer(t, 1))
	seen := map[int64]int{}
	cur := ""
	for range 10 {
		code, body := o1Get(t, h, cur)
		if code != http.StatusOK {
			t.Fatalf("status %d: %s", code, body)
		}
		var p o1Page
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatal(err)
		}
		for _, it := range p.Items {
			seen[it.ID]++
		}
		if p.NextCursor == "" {
			break
		}
		if !strings.HasPrefix(p.NextCursor, "v1.") {
			t.Fatalf("next_cursor %q is not a signed v1 token", p.NextCursor)
		}
		cur = p.NextCursor
	}
	if len(seen) != 8 {
		t.Fatalf("saw %d restaurants, want 8", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("restaurant %d seen %d times", id, n)
		}
	}
}

func TestW2O1_TamperedCursorGetsTheSame400AsAMalformedOne(t *testing.T) {
	h := o1Handler(o1Signer(t, 1))
	_, body := o1Get(t, h, "")
	var p o1Page
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	wantCode, wantBody := o1Get(t, h, "not-base64!!")
	if wantCode != http.StatusBadRequest {
		t.Fatalf("malformed cursor: %d, want 400", wantCode)
	}
	tampered := p.NextCursor[:len(p.NextCursor)/2] + "A" + p.NextCursor[len(p.NextCursor)/2+1:]
	if tampered == p.NextCursor {
		tampered = p.NextCursor[:len(p.NextCursor)/2] + "B" + p.NextCursor[len(p.NextCursor)/2+1:]
	}
	code, got := o1Get(t, h, tampered)
	if code != wantCode || got != wantBody {
		t.Fatalf("tampered cursor: %d %s, want %d %s", code, got, wantCode, wantBody)
	}
}

func TestW2O1_ForgedUnsignedCursorIs400(t *testing.T) {
	h := o1Handler(o1Signer(t, 1))
	code, body := o1Get(t, h, cursor.Encode(cursor.Key{ID: 5}))
	if code != http.StatusBadRequest {
		t.Fatalf("forged cursor: %d %s, want 400", code, body)
	}
}

func TestW2O1_CursorSignedWithAnotherSecretIs400(t *testing.T) {
	other := o1Signer(t, 2).Encode(cursor.Key{ID: 5})
	code, body := o1Get(t, o1Handler(o1Signer(t, 1)), other)
	if code != http.StatusBadRequest {
		t.Fatalf("cursor from another secret: %d %s, want 400", code, body)
	}
}

func TestW2O1_WithoutASignerUnsignedCursorsStillWork(t *testing.T) {
	h := o1Handler(nil)
	code, body := o1Get(t, h, cursor.Encode(cursor.Key{ID: 5}))
	if code != http.StatusOK {
		t.Fatalf("unsigned cursor without a signer: %d %s, want 200", code, body)
	}
	var p o1Page
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Items) == 0 || p.Items[0].ID != 6 {
		t.Fatalf("first id after cursor 5 = %+v, want 6", p.Items)
	}
}
