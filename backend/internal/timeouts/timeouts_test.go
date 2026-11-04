package timeouts_test

import (
	"context"
	"testing"
	"time"

	"github.com/yashdagar/zomato-prep/backend/internal/timeouts"
)

func remaining(t *testing.T, ctx context.Context) time.Duration {
	t.Helper()
	d, ok := ctx.Deadline()
	if !ok {
		t.Fatal("context has no deadline")
	}
	return time.Until(d)
}

func TestW6B3_ReadTimeoutIs500ms(t *testing.T) {
	ctx, cancel := timeouts.Read(context.Background())
	defer cancel()
	if d := remaining(t, ctx); d > 500*time.Millisecond || d < 450*time.Millisecond {
		t.Fatalf("read deadline in %v, want about 500ms", d)
	}
}

func TestW6B3_WriteTimeoutIs2s(t *testing.T) {
	ctx, cancel := timeouts.Write(context.Background())
	defer cancel()
	if d := remaining(t, ctx); d > 2*time.Second || d < 1900*time.Millisecond {
		t.Fatalf("write deadline in %v, want about 2s", d)
	}
}

func TestW6B3_ParentDeadlineWinsWhenShorter(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelParent()
	ctx, cancel := timeouts.Write(parent)
	defer cancel()
	if d := remaining(t, ctx); d > 50*time.Millisecond {
		t.Fatalf("deadline in %v, want the parent's 50ms", d)
	}
}

func TestW6B3_CancelReleasesTheContext(t *testing.T) {
	ctx, cancel := timeouts.Read(context.Background())
	cancel()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("context not done after cancel")
	}
}
