//go:build grpc

// Run after buf generate in your repo:
//
//	TEST_GRPC_ADDR=localhost:9090 go test -tags grpc ./e2e -run W7
package e2e_test

import (
	"context"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	ordersv1 "github.com/yashdagar/zomato-prep/backend/gen/orders/v1"
)

func grpcClient(t *testing.T) ordersv1.OrderServiceClient {
	t.Helper()
	addr := os.Getenv("TEST_GRPC_ADDR")
	if addr == "" {
		t.Skip("set TEST_GRPC_ADDR=localhost:9090 to run")
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return ordersv1.NewOrderServiceClient(conn)
}

func TestW7B8_UnknownOrderIsNotFound(t *testing.T) {
	c := grpcClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.GetOrder(ctx, &ordersv1.GetOrderRequest{Id: 999999999})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("code = %v, want NotFound", status.Code(err))
	}
}

func TestW7B8_PlaceOrderNeedsTokenThenKey(t *testing.T) {
	c := grpcClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := &ordersv1.PlaceOrderRequest{RestaurantId: 1, Lines: []*ordersv1.Line{{ItemId: 1, Size: "M", Qty: 1}}}
	if _, err := c.PlaceOrder(ctx, req); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("no token: %v, want Unauthenticated", status.Code(err))
	}
	_, tk := signup(t, "customer", 0)
	authed := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+tk.Access)
	if _, err := c.PlaceOrder(authed, req); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("no idempotency-key: %v, want InvalidArgument", status.Code(err))
	}
}
