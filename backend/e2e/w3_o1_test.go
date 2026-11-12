//go:build w3o1

package e2e_test

import (
	"net/http"
	"strings"
	"testing"
)

func refresh(t *testing.T, token string) resp {
	t.Helper()
	return newClient(t).post("/v1/auth/refresh", map[string]string{"refresh_token": token})
}

func TestW3O1_ReplayingARotatedTokenRevokesThatLogin(t *testing.T) {
	_, first := signupAs(t, map[string]any{"email": uniqueEmail("o1"), "password": goodPassword})
	second := expectTokenPair(t, refresh(t, first.Refresh), http.StatusOK)
	expectError(t, refresh(t, first.Refresh), http.StatusUnauthorized, "invalid_refresh_token", "")
	expectError(t, refresh(t, second.Refresh), http.StatusUnauthorized, "invalid_refresh_token", "")
}

func TestW3O1_ReplayingAnOldTokenRevokesTheNewestOneSeveralRotationsLater(t *testing.T) {
	_, first := signupAs(t, map[string]any{"email": uniqueEmail("o1deep"), "password": goodPassword})
	latest := first
	for range 3 {
		latest = expectTokenPair(t, refresh(t, latest.Refresh), http.StatusOK)
	}
	expectError(t, refresh(t, first.Refresh), http.StatusUnauthorized, "invalid_refresh_token", "")
	expectError(t, refresh(t, latest.Refresh), http.StatusUnauthorized, "invalid_refresh_token", "")
}

func TestW3O1_TheSameUsersOtherLoginKeepsWorking(t *testing.T) {
	email := uniqueEmail("o1other")
	_, phone := signupAs(t, map[string]any{"email": email, "password": goodPassword})
	laptop := expectTokenPair(t, newClient(t).post("/v1/auth/login", map[string]any{"email": email, "password": goodPassword}), http.StatusOK)
	expectTokenPair(t, refresh(t, phone.Refresh), http.StatusOK)
	refresh(t, phone.Refresh)
	expectTokenPair(t, refresh(t, laptop.Refresh), http.StatusOK)
}

func TestW3O1_AnUnknownTokenRevokesNothing(t *testing.T) {
	_, tp := signupAs(t, map[string]any{"email": uniqueEmail("o1unknown"), "password": goodPassword})
	expectError(t, refresh(t, strings.Repeat("cd", 32)), http.StatusUnauthorized, "invalid_refresh_token", "")
	expectTokenPair(t, refresh(t, tp.Refresh), http.StatusOK)
}
