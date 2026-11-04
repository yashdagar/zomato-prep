//go:build w7o2

package grpcapi_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/yashdagar/zomato-prep/backend/internal/grpcapi"
)

var unaryInfo = &grpc.UnaryServerInfo{FullMethod: "/orders.v1.OrderService/PlaceOrder"}

func textLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

type ctxStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s ctxStream) Context() context.Context { return s.ctx }

func TestW7O2_RecoverUnaryTurnsAPanicIntoInternalWithoutLeakingIt(t *testing.T) {
	log, _ := textLogger()
	h := func(context.Context, any) (any, error) { panic("boom: db password is hunter2") }
	resp, err := grpcapi.RecoverUnary(log)(t.Context(), "req", unaryInfo, h)
	if resp != nil {
		t.Fatalf("resp = %v, want nil", resp)
	}
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
	if msg := status.Convert(err).Message(); msg != "internal error" || strings.Contains(msg, "hunter2") {
		t.Fatalf("message = %q, want the fixed text internal error", msg)
	}
}

func TestW7O2_RecoverUnaryLogsThePanicValueAndAStackTrace(t *testing.T) {
	log, buf := textLogger()
	h := func(context.Context, any) (any, error) { panic("boom-value") }
	_, _ = grpcapi.RecoverUnary(log)(t.Context(), "req", unaryInfo, h)
	out := buf.String()
	if !strings.Contains(out, "boom-value") {
		t.Errorf("log does not contain the panic value: %s", out)
	}
	if !strings.Contains(out, "stack") || !strings.Contains(out, "goroutine") {
		t.Errorf("log does not contain a stack trace: %s", out)
	}
	if !strings.Contains(out, unaryInfo.FullMethod) {
		t.Errorf("log does not name the method: %s", out)
	}
}

func TestW7O2_RecoverUnaryPassesResponsesAndErrorsThrough(t *testing.T) {
	log, buf := textLogger()
	resp, err := grpcapi.RecoverUnary(log)(t.Context(), "req", unaryInfo, func(context.Context, any) (any, error) { return "ok", nil })
	if err != nil || resp != "ok" {
		t.Fatalf("resp = %v, err = %v, want ok, nil", resp, err)
	}
	want := status.Error(codes.NotFound, "no such order")
	_, err = grpcapi.RecoverUnary(log)(t.Context(), "req", unaryInfo, func(context.Context, any) (any, error) { return nil, want })
	if !errors.Is(err, want) && err != want {
		t.Fatalf("err = %v, want the handler's own error", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("nothing should be logged without a panic: %s", buf.String())
	}
}

func TestW7O2_RecoverStreamTurnsAPanicIntoInternal(t *testing.T) {
	log, buf := textLogger()
	h := func(any, grpc.ServerStream) error { panic("stream-boom") }
	err := grpcapi.RecoverStream(log)(nil, ctxStream{ctx: t.Context()}, &grpc.StreamServerInfo{FullMethod: "/orders.v1.OrderService/WatchOrder"}, h)
	if status.Code(err) != codes.Internal || status.Convert(err).Message() != "internal error" {
		t.Fatalf("err = %v, want Internal internal error", err)
	}
	if !strings.Contains(buf.String(), "stream-boom") {
		t.Errorf("log does not contain the panic value: %s", buf.String())
	}
}

func TestW7O2_RecoverStreamPassesNormalReturnsThrough(t *testing.T) {
	log, _ := textLogger()
	err := grpcapi.RecoverStream(log)(nil, ctxStream{ctx: t.Context()}, &grpc.StreamServerInfo{}, func(any, grpc.ServerStream) error { return nil })
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestW7O2_TimeoutUnaryAddsADeadlineWhenTheClientSentNone(t *testing.T) {
	var got time.Time
	var ok bool
	h := func(ctx context.Context, _ any) (any, error) { got, ok = ctx.Deadline(); return nil, nil }
	start := time.Now()
	if _, err := grpcapi.TimeoutUnary(2*time.Second)(t.Context(), "req", unaryInfo, h); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("the handler context has no deadline")
	}
	if d := got.Sub(start); d < time.Second || d > 3*time.Second {
		t.Fatalf("deadline in %v, want about 2s", d)
	}
}

func TestW7O2_TimeoutUnaryKeepsAShorterClientDeadline(t *testing.T) {
	var got time.Time
	h := func(ctx context.Context, _ any) (any, error) { got, _ = ctx.Deadline(); return nil, nil }
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := grpcapi.TimeoutUnary(5*time.Second)(ctx, "req", unaryInfo, h); err != nil {
		t.Fatal(err)
	}
	if d := got.Sub(start); d > time.Second {
		t.Fatalf("deadline in %v, the client's 50ms deadline must be kept", d)
	}
}

func TestW7O2_TimeoutUnaryCapsALongerClientDeadline(t *testing.T) {
	var got time.Time
	h := func(ctx context.Context, _ any) (any, error) { got, _ = ctx.Deadline(); return nil, nil }
	ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
	defer cancel()
	start := time.Now()
	if _, err := grpcapi.TimeoutUnary(time.Second)(ctx, "req", unaryInfo, h); err != nil {
		t.Fatal(err)
	}
	if d := got.Sub(start); d > 2*time.Second {
		t.Fatalf("deadline in %v, the server limit of 1s must win", d)
	}
}

func TestW7O2_TimeoutUnaryReportsDeadlineExceededWhenTheHandlerGivesUp(t *testing.T) {
	h := func(ctx context.Context, _ any) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	_, err := grpcapi.TimeoutUnary(20*time.Millisecond)(t.Context(), "req", unaryInfo, h)
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("code = %v, want DeadlineExceeded", status.Code(err))
	}
}

func TestW7O2_TimeoutUnaryWithAZeroLimitAddsNoDeadline(t *testing.T) {
	var ok bool
	h := func(ctx context.Context, _ any) (any, error) { _, ok = ctx.Deadline(); return nil, nil }
	if _, err := grpcapi.TimeoutUnary(0)(t.Context(), "req", unaryInfo, h); err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("a zero limit must leave the context untouched")
	}
}
