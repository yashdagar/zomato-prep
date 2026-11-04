package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

var secret = []byte("whsec-test-secret-0123456789")

type fakeStore struct {
	mu       sync.Mutex
	payments map[string]Payment
	seen     map[string]bool
}

func newStore() *fakeStore {
	return &fakeStore{payments: map[string]Payment{}, seen: map[string]bool{}}
}

func (s *fakeStore) Create(_ context.Context, p Payment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payments[p.ID] = p
	return nil
}

func (s *fakeStore) Get(_ context.Context, id string) (Payment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.payments[id]
	if !ok {
		return Payment{}, ErrNotFound
	}
	return p, nil
}

func (s *fakeStore) Transition(_ context.Context, id string, from, to Status) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.payments[id]
	if !ok {
		return false, ErrNotFound
	}
	if p.Status != from {
		return false, nil
	}
	p.Status = to
	s.payments[id] = p
	return true, nil
}

func (s *fakeStore) SeenEvent(_ context.Context, eventID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[eventID] {
		return false, nil
	}
	s.seen[eventID] = true
	return true, nil
}

func (s *fakeStore) Pending(_ context.Context, olderThan time.Time) ([]Payment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Payment
	for _, p := range s.payments {
		if p.Status == StatusPending && p.CreatedAt.Before(olderThan) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *fakeStore) status(id string) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.payments[id].Status
}

type fakeOrders struct {
	mu        sync.Mutex
	placed    []string
	cancelled []string
}

func (o *fakeOrders) MarkPlaced(_ context.Context, id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.placed = append(o.placed, id)
	return nil
}

func (o *fakeOrders) MarkCancelled(_ context.Context, id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.cancelled = append(o.cancelled, id)
	return nil
}

type fakeProvider struct {
	mu       sync.Mutex
	statuses map[string]Status
	asked    []string
	refunds  []string
}

func (p *fakeProvider) CreateIntent(_ context.Context, orderID string, _ int64) (string, error) {
	return "pi_" + orderID, nil
}

func (p *fakeProvider) Status(_ context.Context, ref string) (Status, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.asked = append(p.asked, ref)
	return p.statuses[ref], nil
}

func (p *fakeProvider) Refund(_ context.Context, ref string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refunds = append(p.refunds, ref)
	return nil
}

type rig struct {
	svc      *Service
	store    *fakeStore
	orders   *fakeOrders
	provider *fakeProvider
	now      time.Time
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{
		store:    newStore(),
		orders:   &fakeOrders{},
		provider: &fakeProvider{statuses: map[string]Status{}},
		now:      time.Unix(1_700_000_000, 0),
	}
	r.svc = NewService(r.store, r.orders, r.provider, secret, func() time.Time { return r.now })
	return r
}

func (r *rig) intent(t *testing.T, orderID string) Payment {
	t.Helper()
	p, err := r.svc.CreateIntent(t.Context(), orderID, 49900)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func event(id, paymentID, typ string) []byte {
	return []byte(fmt.Sprintf(`{"id":%q,"payment_id":%q,"type":%q}`, id, paymentID, typ))
}

func deliver(r *rig, body []byte, sig string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/payments/webhook", bytes.NewReader(body))
	req.Header.Set("X-Signature", sig)
	rec := httptest.NewRecorder()
	r.svc.Webhook().ServeHTTP(rec, req)
	return rec
}

func signed(r *rig, body []byte) *httptest.ResponseRecorder {
	return deliver(r, body, Sign(secret, body))
}

func TestW8B2_SignIsHexHMACSHA256OfTheBody(t *testing.T) {
	body := []byte(`{"id":"evt_1"}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if got := Sign(secret, body); got != want {
		t.Fatalf("Sign() = %q, want %q", got, want)
	}
}

func TestW8B2_VerifyAcceptsTheRightSignatureAndRejectsEverythingElse(t *testing.T) {
	body := []byte(`{"id":"evt_1"}`)
	good := Sign(secret, body)
	cases := []struct {
		name   string
		secret []byte
		body   []byte
		sig    string
		want   bool
	}{
		{"correct", secret, body, good, true},
		{"tampered body", secret, []byte(`{"id":"evt_2"}`), good, false},
		{"other secret", []byte("another-secret-0123456789"), body, good, false},
		{"not hex", secret, body, "zz" + good[2:], false},
		{"truncated", secret, body, good[:len(good)-2], false},
		{"empty", secret, body, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Verify(tc.secret, tc.body, tc.sig); got != tc.want {
				t.Fatalf("Verify() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestW8B2_CreateIntentStoresAPendingPaymentWithTheProviderRef(t *testing.T) {
	r := newRig(t)
	p := r.intent(t, "o-1")
	if p.ID == "" || p.OrderID != "o-1" || p.Status != StatusPending || p.ProviderRef != "pi_o-1" || p.AmountPaise != 49900 {
		t.Fatalf("CreateIntent() = %+v", p)
	}
	if !p.CreatedAt.Equal(r.now) {
		t.Fatalf("CreatedAt = %v, want the injected clock %v", p.CreatedAt, r.now)
	}
	got, err := r.store.Get(t.Context(), p.ID)
	if err != nil || got != p {
		t.Fatalf("stored payment = %+v, %v; want %+v", got, err, p)
	}
}

func TestW8B2_ACapturedWebhookMarksThePaymentCapturedAndPlacesTheOrder(t *testing.T) {
	r := newRig(t)
	p := r.intent(t, "o-1")
	if rec := signed(r, event("evt_1", p.ID, "payment.captured")); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if r.store.status(p.ID) != StatusCaptured || len(r.orders.placed) != 1 || r.orders.placed[0] != "o-1" {
		t.Fatalf("status=%s placed=%v", r.store.status(p.ID), r.orders.placed)
	}
}

func TestW8B2_AFailedWebhookMarksThePaymentFailedAndCancelsTheOrder(t *testing.T) {
	r := newRig(t)
	p := r.intent(t, "o-1")
	if rec := signed(r, event("evt_1", p.ID, "payment.failed")); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if r.store.status(p.ID) != StatusFailed || len(r.orders.cancelled) != 1 || len(r.orders.placed) != 0 {
		t.Fatalf("status=%s cancelled=%v placed=%v", r.store.status(p.ID), r.orders.cancelled, r.orders.placed)
	}
}

func TestW8B2_ABadSignatureIs401AndChangesNothing(t *testing.T) {
	r := newRig(t)
	p := r.intent(t, "o-1")
	rec := deliver(r, event("evt_1", p.ID, "payment.captured"), "00"+Sign(secret, nil)[2:])
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if r.store.status(p.ID) != StatusPending || len(r.orders.placed) != 0 {
		t.Fatalf("state changed: %s, placed %v", r.store.status(p.ID), r.orders.placed)
	}
}

func TestW8B2_TheSignatureIsCheckedBeforeTheBodyIsParsed(t *testing.T) {
	r := newRig(t)
	if rec := deliver(r, []byte(`{not json`), "deadbeef"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned garbage: status = %d, want 401", rec.Code)
	}
	if rec := signed(r, []byte(`{not json`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("signed garbage: status = %d, want 400", rec.Code)
	}
}

func TestW8B2_AMissingSignatureHeaderIs401(t *testing.T) {
	r := newRig(t)
	p := r.intent(t, "o-1")
	req := httptest.NewRequest(http.MethodPost, "/v1/payments/webhook", bytes.NewReader(event("evt_1", p.ID, "payment.captured")))
	rec := httptest.NewRecorder()
	r.svc.Webhook().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestW8B2_AnUnknownPaymentIs404AndTheEventIsNotConsumed(t *testing.T) {
	r := newRig(t)
	if rec := signed(r, event("evt_1", "pay_missing", "payment.captured")); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	p := r.intent(t, "o-1")
	if rec := signed(r, event("evt_1", p.ID, "payment.captured")); rec.Code != http.StatusOK || r.store.status(p.ID) != StatusCaptured {
		t.Fatalf("a retry of evt_1 after the payment exists must still apply: status %d, %s", rec.Code, r.store.status(p.ID))
	}
}

func TestW8B2_TheSameWebhookThreeTimesPlacesTheOrderOnce(t *testing.T) {
	r := newRig(t)
	p := r.intent(t, "o-1")
	body := event("evt_1", p.ID, "payment.captured")
	for i := 0; i < 3; i++ {
		if rec := signed(r, body); rec.Code != http.StatusOK {
			t.Fatalf("delivery %d: status = %d, want 200", i, rec.Code)
		}
	}
	if len(r.orders.placed) != 1 || r.store.status(p.ID) != StatusCaptured || len(r.provider.refunds) != 0 {
		t.Fatalf("placed=%v status=%s refunds=%v", r.orders.placed, r.store.status(p.ID), r.provider.refunds)
	}
}

func TestW8B2_ConcurrentDuplicateDeliveriesPlaceTheOrderOnce(t *testing.T) {
	r := newRig(t)
	p := r.intent(t, "o-1")
	body := event("evt_1", p.ID, "payment.captured")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			signed(r, body)
		}()
	}
	wg.Wait()
	if len(r.orders.placed) != 1 {
		t.Fatalf("placed %d times, want 1", len(r.orders.placed))
	}
}

func TestW8B2_ASecondCaptureWithAnotherEventIDChangesNothing(t *testing.T) {
	r := newRig(t)
	p := r.intent(t, "o-1")
	signed(r, event("evt_1", p.ID, "payment.captured"))
	signed(r, event("evt_2", p.ID, "payment.captured"))
	if len(r.orders.placed) != 1 || len(r.provider.refunds) != 0 {
		t.Fatalf("placed=%v refunds=%v", r.orders.placed, r.provider.refunds)
	}
}

func TestW8B2_AFailureArrivingAfterACaptureIsIgnored(t *testing.T) {
	r := newRig(t)
	p := r.intent(t, "o-1")
	signed(r, event("evt_2", p.ID, "payment.captured"))
	if rec := signed(r, event("evt_1", p.ID, "payment.failed")); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if r.store.status(p.ID) != StatusCaptured || len(r.orders.cancelled) != 0 {
		t.Fatalf("status=%s cancelled=%v", r.store.status(p.ID), r.orders.cancelled)
	}
}

func TestW8B2_ACaptureArrivingAfterAFailureIsRefundedOnce(t *testing.T) {
	r := newRig(t)
	p := r.intent(t, "o-1")
	signed(r, event("evt_1", p.ID, "payment.failed"))
	body := event("evt_2", p.ID, "payment.captured")
	signed(r, body)
	signed(r, body)
	if r.store.status(p.ID) != StatusFailed || len(r.orders.placed) != 0 {
		t.Fatalf("status=%s placed=%v, want FAILED and no placed order", r.store.status(p.ID), r.orders.placed)
	}
	if len(r.provider.refunds) != 1 || r.provider.refunds[0] != "pi_o-1" {
		t.Fatalf("refunds = %v, want one refund of pi_o-1", r.provider.refunds)
	}
}

func TestW8B2_ReconcileSettlesOldPendingPaymentsFromTheProvider(t *testing.T) {
	r := newRig(t)
	paid, dead := r.intent(t, "o-1"), r.intent(t, "o-2")
	r.provider.statuses["pi_o-1"] = StatusCaptured
	r.provider.statuses["pi_o-2"] = StatusFailed
	r.now = r.now.Add(6 * time.Minute)
	if err := r.svc.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if r.store.status(paid.ID) != StatusCaptured || r.store.status(dead.ID) != StatusFailed {
		t.Fatalf("statuses = %s and %s", r.store.status(paid.ID), r.store.status(dead.ID))
	}
	if len(r.orders.placed) != 1 || r.orders.placed[0] != "o-1" || len(r.orders.cancelled) != 1 || r.orders.cancelled[0] != "o-2" {
		t.Fatalf("placed=%v cancelled=%v", r.orders.placed, r.orders.cancelled)
	}
}

func TestW8B2_ReconcileLeavesRecentPaymentsAlone(t *testing.T) {
	r := newRig(t)
	p := r.intent(t, "o-1")
	r.provider.statuses["pi_o-1"] = StatusCaptured
	r.now = r.now.Add(4 * time.Minute)
	if err := r.svc.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if r.store.status(p.ID) != StatusPending || len(r.provider.asked) != 0 {
		t.Fatalf("status=%s asked=%v, want PENDING and no provider call", r.store.status(p.ID), r.provider.asked)
	}
}

func TestW8B2_ReconcileKeepsPaymentsTheProviderStillCallsPending(t *testing.T) {
	r := newRig(t)
	p := r.intent(t, "o-1")
	r.provider.statuses["pi_o-1"] = StatusPending
	r.now = r.now.Add(10 * time.Minute)
	if err := r.svc.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if r.store.status(p.ID) != StatusPending || len(r.orders.placed)+len(r.orders.cancelled) != 0 {
		t.Fatalf("status=%s placed=%v cancelled=%v", r.store.status(p.ID), r.orders.placed, r.orders.cancelled)
	}
}

func TestW8B2_ReconcileAfterAWebhookDoesNotPlaceTheOrderTwice(t *testing.T) {
	r := newRig(t)
	p := r.intent(t, "o-1")
	signed(r, event("evt_1", p.ID, "payment.captured"))
	r.provider.statuses["pi_o-1"] = StatusCaptured
	r.now = r.now.Add(time.Hour)
	if err := r.svc.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(r.orders.placed) != 1 {
		t.Fatalf("placed %d times, want 1", len(r.orders.placed))
	}
}
