//go:build w3o3

package e2e_test

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func loginAt(t *testing.T, base, email, password string, headers ...string) resp {
	t.Helper()
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
	return rawRequest(t, clientFor(t, base), http.MethodPost, "/v1/auth/login", body, headers...)
}

func TestW3O3_TooManyLoginsFromOneIPGet429WhateverTheirXUserID(t *testing.T) {
	base, _ := startAPI(t, "LOGIN_LIMIT_PER_IP=5", "LOGIN_LIMIT_PER_EMAIL=1000")
	for i := 1; i <= 6; i++ {
		r := loginAt(t, base, uniqueEmail("o3ip"), "wrong", "X-User-ID", "forged-"+strconv.Itoa(i))
		if r.Status == http.StatusTooManyRequests {
			expectError(t, r, http.StatusTooManyRequests, "rate_limited", "")
			if r.Header.Get("Retry-After") == "" {
				t.Fatal("a 429 needs a Retry-After header")
			}
			return
		}
		if r.Status != http.StatusUnauthorized {
			t.Fatalf("login %d: %d %s, want 401 or, past the limit, 429", i, r.Status, r.Body)
		}
	}
	t.Fatal("six logins from one IP within a minute, each with a different X-User-ID, never got 429 with LOGIN_LIMIT_PER_IP=5")
}

func TestW3O3_OneEmailIsLimitedWhateverItsCaseOrSpaces(t *testing.T) {
	base, _ := startAPI(t, "LOGIN_LIMIT_PER_IP=100000", "LOGIN_LIMIT_PER_EMAIL=3")
	email := uniqueEmail("o3email")
	for _, spelling := range []string{email, strings.ToUpper(email), "  " + email + " "} {
		if r := loginAt(t, base, spelling, "wrong"); r.Status != http.StatusUnauthorized {
			t.Fatalf("attempt on %q: %d %s, want 401", spelling, r.Status, r.Body)
		}
	}
	expectError(t, loginAt(t, base, strings.ToUpper(email), "wrong"), http.StatusTooManyRequests, "rate_limited", "")
	expectError(t, loginAt(t, base, uniqueEmail("o3fresh"), "wrong"), http.StatusUnauthorized, "invalid_credentials", "")
}

func TestW3O3_TheLimiterLeavesTheRequestIntactForTheHandler(t *testing.T) {
	base, _ := startAPI(t, "LOGIN_LIMIT_PER_IP=100000", "LOGIN_LIMIT_PER_EMAIL=3")
	email := uniqueEmail("o3ok")
	signupAs(t, map[string]any{"email": email, "password": goodPassword})
	if r := loginAt(t, base, email, goodPassword); r.Status != http.StatusOK {
		t.Fatalf("a correct login through the limiter: %d %s, want 200", r.Status, r.Body)
	}
	expectError(t, rawRequest(t, clientFor(t, base), http.MethodPost, "/v1/auth/login", "not json"), http.StatusBadRequest, "invalid_body", "")
}
