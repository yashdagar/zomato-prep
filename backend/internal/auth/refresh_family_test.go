//go:build w3o1

package auth_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"

	"github.com/yashdagar/zomato-prep/backend/internal/auth"
)

func refreshStore(t *testing.T) (*auth.RefreshStore, *redis.Client) {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set TEST_REDIS_ADDR=127.0.0.1:6379 to run")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	return auth.NewRefreshStore(rdb, time.Minute), rdb
}

func unique(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func TestW3O1_ConsumeReturnsTheUserAndFamilyOfTheToken(t *testing.T) {
	s, _ := refreshStore(t)
	tok, fam := unique("tok"), unique("fam")
	if err := s.Save(t.Context(), tok, 42, fam); err != nil {
		t.Fatal(err)
	}
	userID, gotFam, err := s.Consume(t.Context(), tok)
	if err != nil || userID != 42 || gotFam != fam {
		t.Fatalf("Consume() = %d, %q, %v; want 42, %q, nil", userID, gotFam, err, fam)
	}
}

func TestW3O1_UnknownTokenIsInvalidNotReused(t *testing.T) {
	s, _ := refreshStore(t)
	_, _, err := s.Consume(t.Context(), unique("never-issued"))
	if !errors.Is(err, auth.ErrInvalidRefresh) || errors.Is(err, auth.ErrRefreshReused) {
		t.Fatalf("Consume(unknown) error = %v, want ErrInvalidRefresh only", err)
	}
}

func TestW3O1_RotatedTokenPresentedAgainIsReported(t *testing.T) {
	s, _ := refreshStore(t)
	tok, fam := unique("tok"), unique("fam")
	if err := s.Save(t.Context(), tok, 7, fam); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Consume(t.Context(), tok); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Consume(t.Context(), tok)
	if !errors.Is(err, auth.ErrRefreshReused) {
		t.Fatalf("second Consume error = %v, want ErrRefreshReused", err)
	}
}

func TestW3O1_ReuseRevokesTheLiveTokenOfTheFamily(t *testing.T) {
	s, _ := refreshStore(t)
	old, next, fam := unique("old"), unique("next"), unique("fam")
	if err := s.Save(t.Context(), old, 7, fam); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Consume(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(t.Context(), next, 7, fam); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Consume(t.Context(), old); !errors.Is(err, auth.ErrRefreshReused) {
		t.Fatalf("replayed token error = %v, want ErrRefreshReused", err)
	}
	if _, _, err := s.Consume(t.Context(), next); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Fatalf("live token of the revoked family error = %v, want ErrInvalidRefresh", err)
	}
}

func TestW3O1_ReuseLeavesOtherFamiliesAlone(t *testing.T) {
	s, _ := refreshStore(t)
	old, mine, other := unique("old"), unique("mine"), unique("other")
	famA, famB := unique("famA"), unique("famB")
	for tok, fam := range map[string]string{old: famA, mine: famA, other: famB} {
		if err := s.Save(t.Context(), tok, 7, fam); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Consume(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Consume(t.Context(), old); !errors.Is(err, auth.ErrRefreshReused) {
		t.Fatalf("replayed token error = %v, want ErrRefreshReused", err)
	}
	if _, _, err := s.Consume(t.Context(), other); err != nil {
		t.Fatalf("token of an unrelated family failed after the revocation: %v", err)
	}
}

type tokenPair struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
}

func authHandlers(t *testing.T) (*auth.Handlers, *sql.DB) {
	t.Helper()
	addr, dsn := os.Getenv("TEST_REDIS_ADDR"), os.Getenv("TEST_MYSQL_DSN")
	if addr == "" || dsn == "" {
		t.Skip("set TEST_REDIS_ADDR and TEST_MYSQL_DSN, and run make migrate, to run")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	h, err := auth.NewHandlers(db, rdb, auth.NewSigner("test-secret-that-is-at-least-32-bytes", 15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return h, db
}

func call(t *testing.T, h http.HandlerFunc, body any) (int, tokenPair) {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", bytes.NewReader(raw))
	h(rec, req)
	var tp tokenPair
	_ = json.Unmarshal(rec.Body.Bytes(), &tp)
	return rec.Code, tp
}

func signedUp(t *testing.T, h *auth.Handlers, db *sql.DB) (email string, tp tokenPair) {
	t.Helper()
	email = fmt.Sprintf("w3o1-%d@test.local", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE email = ?`, email) })
	code, tp := call(t, h.Signup, map[string]string{"email": email, "password": "correct-horse-battery"})
	if code != http.StatusCreated {
		t.Fatalf("signup status = %d", code)
	}
	return email, tp
}

func TestW3O1_ReplayedRefreshTokenGets401AndKillsTheWholeFamily(t *testing.T) {
	h, db := authHandlers(t)
	_, first := signedUp(t, h, db)

	code, second := call(t, h.Refresh, map[string]string{"refresh_token": first.Refresh})
	if code != http.StatusOK || second.Refresh == "" {
		t.Fatalf("first refresh = %d, want 200 with a new token", code)
	}
	if code, _ := call(t, h.Refresh, map[string]string{"refresh_token": first.Refresh}); code != http.StatusUnauthorized {
		t.Fatalf("replay of the rotated token = %d, want 401", code)
	}
	if code, _ := call(t, h.Refresh, map[string]string{"refresh_token": second.Refresh}); code != http.StatusUnauthorized {
		t.Fatalf("the newest token after a replay = %d, want 401 because the family is revoked", code)
	}
}

func TestW3O1_ReplayDoesNotRevokeTheSameUsersOtherLogin(t *testing.T) {
	h, db := authHandlers(t)
	email, signup := signedUp(t, h, db)
	code, otherDevice := call(t, h.Login, map[string]string{"email": email, "password": "correct-horse-battery"})
	if code != http.StatusOK {
		t.Fatalf("login status = %d", code)
	}
	if code, _ := call(t, h.Refresh, map[string]string{"refresh_token": signup.Refresh}); code != http.StatusOK {
		t.Fatalf("refresh = %d", code)
	}
	call(t, h.Refresh, map[string]string{"refresh_token": signup.Refresh})
	if code, _ := call(t, h.Refresh, map[string]string{"refresh_token": otherDevice.Refresh}); code != http.StatusOK {
		t.Fatalf("a different login of the same user = %d, want 200: each login starts its own family", code)
	}
}
