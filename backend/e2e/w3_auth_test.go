package e2e_test

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const goodPassword = "correct horse battery staple"

type tokenPair struct {
	Access    string `json:"access_token"`
	Refresh   string `json:"refresh_token"`
	TokenType string `json:"token_type"`
	ExpiresIn int    `json:"expires_in"`
}

func jwtSecret(t *testing.T) []byte {
	t.Helper()
	return []byte(needDB(t, "TEST_JWT_SECRET"))
}

func claimsOf(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("access_token %q is not a JWT", token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("access_token payload is not base64url: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("access_token payload is not JSON: %v", err)
	}
	return claims
}

func signWith(t *testing.T, method jwt.SigningMethod, key any, claims map[string]any) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, jwt.MapClaims(claims)).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func signupAs(t *testing.T, body map[string]any) (resp, tokenPair) {
	t.Helper()
	r := newClient(t).post("/v1/auth/signup", body)
	var tp tokenPair
	if r.Status == http.StatusCreated {
		r.JSON(t, &tp)
	}
	return r, tp
}

func expectTokenPair(t *testing.T, r resp, status int) tokenPair {
	t.Helper()
	if r.Status != status {
		t.Fatalf("%d %s, want %d with a token pair", r.Status, r.Body, status)
	}
	var tp tokenPair
	r.JSON(t, &tp)
	if tp.TokenType != "Bearer" || tp.ExpiresIn != 900 {
		t.Fatalf("token_type %q expires_in %d, want Bearer and 900", tp.TokenType, tp.ExpiresIn)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(tp.Refresh) {
		t.Fatalf("refresh_token = %q, want 32 random bytes as 64 lowercase hex characters", tp.Refresh)
	}
	parsed, err := jwt.Parse(tp.Access, func(*jwt.Token) (any, error) { return jwtSecret(t), nil }, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !parsed.Valid {
		t.Fatalf("access_token does not verify as HS256 with JWT_SECRET: %v", err)
	}
	exp, err := parsed.Claims.GetExpirationTime()
	if err != nil || exp == nil {
		t.Fatal("access_token has no exp claim")
	}
	if d := time.Until(exp.Time); math.Abs(d.Seconds()-900) > 60 {
		t.Fatalf("access_token expires in %v, want about 15 minutes", d.Round(time.Second))
	}
	return tp
}

func TestW3B1_SignupReturns201WithAVerifiableTokenPair(t *testing.T) {
	r, _ := signupAs(t, map[string]any{"email": uniqueEmail("pair"), "password": goodPassword})
	expectTokenPair(t, r, http.StatusCreated)
}

func TestW3B1_PasswordsAreStoredAsBcryptHashes(t *testing.T) {
	db := apiDB(t)
	email := uniqueEmail("hash")
	signupAs(t, map[string]any{"email": email, "password": goodPassword})
	var hash string
	if err := db.QueryRowContext(t.Context(), "SELECT password_hash FROM users WHERE email = ?", email).Scan(&hash); err != nil {
		t.Fatalf("users row for %s: %v", email, err)
	}
	m := regexp.MustCompile(`^\$2[aby]\$(\d\d)\$.{53}$`).FindStringSubmatch(hash)
	if m == nil {
		t.Fatalf("password_hash %q is not a bcrypt hash", hash)
	}
	if cost, _ := strconv.Atoi(m[1]); cost < 10 {
		t.Fatalf("bcrypt cost %d is too cheap to slow down a guessing attack; use at least 10", cost)
	}
}

func TestW3B1_EmailIsTrimmedAndLowercased(t *testing.T) {
	db := apiDB(t)
	email := uniqueEmail("case")
	r, _ := signupAs(t, map[string]any{"email": "  " + strings.ToUpper(email) + " ", "password": goodPassword})
	if r.Status != http.StatusCreated {
		t.Fatalf("signup with spaces and capitals: %d %s", r.Status, r.Body)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM users WHERE email = ?", email); n != 1 {
		t.Fatal("the stored email should be trimmed and lowercased")
	}
	expectTokenPair(t, newClient(t).post("/v1/auth/login", map[string]any{"email": strings.ToUpper(email), "password": goodPassword}), http.StatusOK)
}

func TestW3B1_ASecondSignupWithTheSameEmailIsAConflict(t *testing.T) {
	email := uniqueEmail("dup")
	signupAs(t, map[string]any{"email": email, "password": goodPassword})
	r, _ := signupAs(t, map[string]any{"email": strings.ToUpper(email), "password": goodPassword})
	expectError(t, r, http.StatusConflict, "email_taken", "")
}

func TestW3B1_SignupRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name, field string
		body        map[string]any
	}{
		{"malformed email", "email", map[string]any{"email": "not-an-email", "password": goodPassword}},
		{"email with a display name", "email", map[string]any{"email": "Bob <bob@test.local>", "password": goodPassword}},
		{"empty password", "password", map[string]any{"email": uniqueEmail("empty"), "password": ""}},
		{"password over 72 bytes", "password", map[string]any{"email": uniqueEmail("long"), "password": strings.Repeat("a", 73)}},
		{"unknown role", "role", map[string]any{"email": uniqueEmail("role"), "password": goodPassword, "role": "admin"}},
		{"restaurant without its id", "restaurant_id", map[string]any{"email": uniqueEmail("rest"), "password": goodPassword, "role": "restaurant"}},
		{"customer with a restaurant id", "restaurant_id", map[string]any{"email": uniqueEmail("cust"), "password": goodPassword, "restaurant_id": 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := signupAs(t, tc.body)
			expectError(t, r, http.StatusBadRequest, "invalid_parameter", tc.field)
		})
	}
	t.Run("not json", func(t *testing.T) {
		expectError(t, postRaw(t, "/v1/auth/signup", "email=a@b.c"), http.StatusBadRequest, "invalid_body", "")
	})
}

func TestW3B1_LoginReturnsAWorkingTokenPair(t *testing.T) {
	email := uniqueEmail("login")
	signupAs(t, map[string]any{"email": email, "password": goodPassword})
	tp := expectTokenPair(t, newClient(t).post("/v1/auth/login", map[string]any{"email": email, "password": goodPassword}), http.StatusOK)
	expectTokenPair(t, newClient(t).post("/v1/auth/refresh", map[string]string{"refresh_token": tp.Refresh}), http.StatusOK)
}

func TestW3B1_WrongPasswordAndUnknownEmailGetTheSameAnswer(t *testing.T) {
	email := uniqueEmail("enum")
	signupAs(t, map[string]any{"email": email, "password": goodPassword})
	c := newClient(t)
	expectError(t, c.post("/v1/auth/login", map[string]any{"email": email, "password": "wrong password"}), http.StatusUnauthorized, "invalid_credentials", "")
	expectError(t, c.post("/v1/auth/login", map[string]any{"email": uniqueEmail("ghost"), "password": "wrong password"}), http.StatusUnauthorized, "invalid_credentials", "")
}

func TestW3B1_RefreshRotatesTheTokenAndTheOldOneStopsWorking(t *testing.T) {
	_, first := signupAs(t, map[string]any{"email": uniqueEmail("rot"), "password": goodPassword})
	c := newClient(t)
	second := expectTokenPair(t, c.post("/v1/auth/refresh", map[string]string{"refresh_token": first.Refresh}), http.StatusOK)
	if second.Refresh == first.Refresh {
		t.Fatal("refresh returned the same refresh_token; it must rotate")
	}
	expectTokenPair(t, c.post("/v1/auth/refresh", map[string]string{"refresh_token": second.Refresh}), http.StatusOK)
	expectError(t, c.post("/v1/auth/refresh", map[string]string{"refresh_token": first.Refresh}), http.StatusUnauthorized, "invalid_refresh_token", "")
	expectError(t, c.post("/v1/auth/refresh", map[string]string{"refresh_token": strings.Repeat("ab", 32)}), http.StatusUnauthorized, "invalid_refresh_token", "")
}

func me(t *testing.T, token string) resp {
	t.Helper()
	c := newClient(t)
	c.token = token
	return c.get("/v1/me")
}

func TestW3B2_MeReturnsTheCallersAccount(t *testing.T) {
	email := uniqueEmail("me")
	_, tp := signupAs(t, map[string]any{"email": email, "password": goodPassword})
	r := me(t, tp.Access)
	var got map[string]any
	r.JSON(t, &got)
	rid, present := got["restaurant_id"]
	if r.Status != http.StatusOK || got["email"] != email || got["role"] != "customer" || !present || rid != nil {
		t.Fatalf("me = %d %s, want 200 with this email, role customer and restaurant_id null", r.Status, r.Body)
	}
	if id, ok := got["id"].(float64); !ok || id <= 0 {
		t.Fatalf("me = %s, want a positive id", r.Body)
	}
	_, staff := signupAs(t, map[string]any{"email": uniqueEmail("staff"), "password": goodPassword, "role": "restaurant", "restaurant_id": 3})
	r = me(t, staff.Access)
	r.JSON(t, &got)
	if got["role"] != "restaurant" || got["restaurant_id"] != float64(3) {
		t.Fatalf("restaurant account me = %s, want role restaurant and restaurant_id 3", r.Body)
	}
}

func TestW3B2_ProtectedRoutesNeedABearerToken(t *testing.T) {
	anon := newClient(t)
	expectError(t, anon.get("/v1/me"), http.StatusUnauthorized, "unauthorized", "")
}

func TestW3B2_EveryBadCredentialGetsTheSame401(t *testing.T) {
	_, tp := signupAs(t, map[string]any{"email": uniqueEmail("bad"), "password": goodPassword})
	claims := claimsOf(t, tp.Access)
	expired := map[string]any{}
	for k, v := range claims {
		expired[k] = v
	}
	expired["exp"] = time.Now().Add(-2 * time.Minute).Unix()
	escalated := map[string]any{}
	for k, v := range claims {
		escalated[k] = v
	}
	changed := false
	for k, v := range escalated {
		if v == "customer" {
			escalated[k] = "restaurant"
			changed = true
		}
	}
	if !changed {
		escalated["exp"] = time.Now().Add(24 * time.Hour).Unix()
	}
	parts := strings.Split(tp.Access, ".")
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString(mustJSON(t, escalated)) + "." + parts[2]
	headers := map[string]string{
		"basic scheme":           "Basic " + tp.Access,
		"empty bearer":           "Bearer ",
		"not a jwt":              "Bearer not-a-jwt",
		"no space after bearer":  "Bearer" + tp.Access,
		"expired":                "Bearer " + signWith(t, jwt.SigningMethodHS256, jwtSecret(t), expired),
		"signed with HS384":      "Bearer " + signWith(t, jwt.SigningMethodHS384, jwtSecret(t), claims),
		"signed with alg none":   "Bearer " + signWith(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, claims),
		"another secret":         "Bearer " + signWith(t, jwt.SigningMethodHS256, []byte("an-attacker-secret-that-is-32-bytes!"), claims),
		"payload changed":        "Bearer " + tampered,
		"refresh token as token": "Bearer " + tp.Refresh,
	}
	for name, h := range headers {
		t.Run(name, func(t *testing.T) {
			expectError(t, newClient(t).get("/v1/me", "Authorization", h), http.StatusUnauthorized, "unauthorized", "")
		})
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestW3B2_PublicRoutesNeedNoToken(t *testing.T) {
	c := newClient(t)
	if r := c.get("/healthz"); r.Status != http.StatusOK {
		t.Fatalf("healthz without a token: %d, want 200", r.Status)
	}
	if r := c.get(listPath(centreLat, centreLng, 5, 1, nil)); r.Status != http.StatusOK {
		t.Fatalf("restaurant list without a token: %d, want 200", r.Status)
	}
	r := c.post("/v1/auth/login", map[string]any{"email": uniqueEmail("public"), "password": "x"})
	expectError(t, r, http.StatusUnauthorized, "invalid_credentials", "")
}
