package httpxtest_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yashdagar/zomato-prep/backend/internal/httpx"
)

func status(err error) int {
	rec := httptest.NewRecorder()
	httpx.Fail(rec, err)
	return rec.Code
}

func TestW6B3_DeadlineExceededIs504(t *testing.T) {
	if got := status(context.DeadlineExceeded); got != http.StatusGatewayTimeout {
		t.Fatalf("status %d, want 504", got)
	}
}

func TestW6B3_WrappedDeadlineExceededIs504(t *testing.T) {
	err := fmt.Errorf("menu 3: items: %w", context.DeadlineExceeded)
	if got := status(err); got != http.StatusGatewayTimeout {
		t.Fatalf("status %d, want 504", got)
	}
}

func TestW6B3_OtherErrorsAre500(t *testing.T) {
	if got := status(errors.New("boom")); got != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", got)
	}
}

func TestW6B3_TimeoutBodyNamesTheError(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.Fail(rec, context.DeadlineExceeded)
	if body := rec.Body.String(); body != "{\"error\":\"timeout\"}\n" {
		t.Fatalf("body %q", body)
	}
}
