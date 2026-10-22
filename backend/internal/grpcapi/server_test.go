package grpcapi_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	ordersv1 "github.com/yashdagar/zomato-prep/backend/gen/orders/v1"
	"github.com/yashdagar/zomato-prep/backend/internal/auth"
	"github.com/yashdagar/zomato-prep/backend/internal/grpcapi"
	"github.com/yashdagar/zomato-prep/backend/internal/order"
)

type fakeStore struct {
	mu     sync.Mutex
	orders map[int64]*ordersv1.Order
	placed map[string]int64
	events chan *ordersv1.WatchOrderResponse
}

func newFake() *fakeStore {
	return &fakeStore{
		orders: map[int64]*ordersv1.Order{7: {Id: 7, UserId: 1, Status: ordersv1.Status_STATUS_PLACED, Version: 1}},
		placed: map[string]int64{},
		events: make(chan *ordersv1.WatchOrderResponse, 4),
	}
}

func (f *fakeStore) Get(_ context.Context, id int64) (*ordersv1.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.orders[id]
	if !ok {
		return nil, grpcapi.ErrNotFound
	}
	return o, nil
}

func (f *fakeStore) Place(_ context.Context, userID int64, key string, req *ordersv1.PlaceOrderRequest) (*ordersv1.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.placed[key]; ok {
		return f.orders[id], nil
	}
	id := int64(len(f.orders) + 1)
	o := &ordersv1.Order{Id: id, UserId: userID, RestaurantId: req.GetRestaurantId(), Status: ordersv1.Status_STATUS_PLACED, Version: 1, Lines: req.GetLines()}
	f.orders[id] = o
	f.placed[key] = id
	return o, nil
}

func (f *fakeStore) Transition(_ context.Context, p auth.Principal, orderID int64, _ string, ev order.Event) (*ordersv1.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.orders[orderID]
	if !ok {
		return nil, grpcapi.ErrNotFound
	}
	next, err := order.Next(order.Status(statusName(o.GetStatus())), ev, order.Role(p.Role))
	if err != nil {
		return nil, err
	}
	o.Status = statusEnum(next)
	o.Version++
	return o, nil
}

func (f *fakeStore) Subscribe(_ context.Context, _ int64) (<-chan *ordersv1.WatchOrderResponse, error) {
	return f.events, nil
}

func statusName(s ordersv1.Status) string {
	return s.String()[len("STATUS_"):]
}

func statusEnum(s order.Status) ordersv1.Status {
	return ordersv1.Status(ordersv1.Status_value["STATUS_"+string(s)])
}

func start(t *testing.T, store grpcapi.Store, signer *auth.Signer) ordersv1.OrderServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			grpcapi.LogUnary(slog.New(slog.DiscardHandler)),
			grpcapi.AuthUnary(signer, map[string]bool{ordersv1.OrderService_GetOrder_FullMethodName: true}),
		),
		grpc.StreamInterceptor(grpcapi.AuthStream(signer)),
	)
	ordersv1.RegisterOrderServiceServer(srv, grpcapi.New(store))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return ordersv1.NewOrderServiceClient(conn)
}

func withAuth(t *testing.T, signer *auth.Signer, userID int64, role string, extra ...string) context.Context {
	t.Helper()
	tok, err := signer.Issue(userID, role, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	pairs := append([]string{"authorization", "Bearer " + tok}, extra...)
	return metadata.AppendToOutgoingContext(t.Context(), pairs...)
}

func code(err error) codes.Code { return status.Code(err) }

func TestW7B8_GetOrderIsPublicAndMapsNotFound(t *testing.T) {
	signer := auth.NewSigner("secret", time.Hour)
	c := start(t, newFake(), signer)

	got, err := c.GetOrder(t.Context(), &ordersv1.GetOrderRequest{Id: 7})
	if err != nil || got.GetOrder().GetId() != 7 {
		t.Fatalf("GetOrder(7) = %v, %v", got, err)
	}
	_, err = c.GetOrder(t.Context(), &ordersv1.GetOrderRequest{Id: 99})
	if code(err) != codes.NotFound {
		t.Fatalf("GetOrder(99) code = %v, want NotFound", code(err))
	}
}

func TestW7B8_PlaceOrderRequiresTokenAndIdempotencyKey(t *testing.T) {
	signer := auth.NewSigner("secret", time.Hour)
	c := start(t, newFake(), signer)
	req := &ordersv1.PlaceOrderRequest{RestaurantId: 3, Lines: []*ordersv1.Line{{ItemId: 1, Size: "M", Qty: 1, UnitPaise: 29900}}}

	_, err := c.PlaceOrder(t.Context(), req)
	if code(err) != codes.Unauthenticated {
		t.Fatalf("no token: code = %v, want Unauthenticated", code(err))
	}
	_, err = c.PlaceOrder(withAuth(t, signer, 1, "customer"), req)
	if code(err) != codes.InvalidArgument {
		t.Fatalf("no key: code = %v, want InvalidArgument", code(err))
	}
}

func TestW7B8_PlaceOrderTwiceWithSameKeyReturnsSameOrder(t *testing.T) {
	signer := auth.NewSigner("secret", time.Hour)
	c := start(t, newFake(), signer)
	ctx := withAuth(t, signer, 1, "customer", "idempotency-key", "k-1")
	req := &ordersv1.PlaceOrderRequest{RestaurantId: 3, Lines: []*ordersv1.Line{{ItemId: 1, Size: "M", Qty: 2, UnitPaise: 29900}}}

	first, err := c.PlaceOrder(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.PlaceOrder(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.GetOrder().GetId() != second.GetOrder().GetId() {
		t.Fatalf("ids differ: %d vs %d", first.GetOrder().GetId(), second.GetOrder().GetId())
	}
}

func TestW7B8_TransitionMapsDomainErrorsToCodes(t *testing.T) {
	signer := auth.NewSigner("secret", time.Hour)
	c := start(t, newFake(), signer)

	cases := []struct {
		name string
		role string
		ev   string
		want codes.Code
	}{
		{"customer cannot accept", "customer", "ACCEPT", codes.PermissionDenied},
		{"deliver from placed is illegal", "rider", "DELIVER", codes.FailedPrecondition},
		{"restaurant accepts", "restaurant", "ACCEPT", codes.OK},
		{"missing order", "restaurant", "ACCEPT", codes.NotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := int64(7)
			if tc.want == codes.NotFound {
				id = 404
			}
			_, err := c.Transition(withAuth(t, signer, 2, tc.role), &ordersv1.TransitionRequest{OrderId: id, EventId: "e-" + tc.name, Event: tc.ev})
			if code(err) != tc.want {
				t.Fatalf("code = %v (%v), want %v", code(err), err, tc.want)
			}
		})
	}
}

func TestW7B8_WatchOrderStreamsUntilStoreCloses(t *testing.T) {
	signer := auth.NewSigner("secret", time.Hour)
	store := newFake()
	c := start(t, store, signer)

	stream, err := c.WatchOrder(withAuth(t, signer, 1, "customer"), &ordersv1.WatchOrderRequest{OrderId: 7})
	if err != nil {
		t.Fatal(err)
	}
	store.events <- &ordersv1.WatchOrderResponse{OrderId: 7, EventId: "e1", From: ordersv1.Status_STATUS_PLACED, To: ordersv1.Status_STATUS_ACCEPTED}
	store.events <- &ordersv1.WatchOrderResponse{OrderId: 7, EventId: "e2", From: ordersv1.Status_STATUS_ACCEPTED, To: ordersv1.Status_STATUS_PREPARING}
	close(store.events)

	var got []string
	for {
		ev, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ev.GetEventId())
	}
	if len(got) != 2 || got[0] != "e1" || got[1] != "e2" {
		t.Fatalf("got events %v, want [e1 e2]", got)
	}
}

func TestW7B8_WatchOrderWithoutTokenIsUnauthenticated(t *testing.T) {
	signer := auth.NewSigner("secret", time.Hour)
	c := start(t, newFake(), signer)
	stream, err := c.WatchOrder(t.Context(), &ordersv1.WatchOrderRequest{OrderId: 7})
	if err != nil {
		t.Fatal(err)
	}
	_, err = stream.Recv()
	if code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", code(err))
	}
}
