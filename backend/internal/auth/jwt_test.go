package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestW3B1_IssueAndParse(t *testing.T) {
	s := NewSigner("test-secret", 15*time.Minute)
	tok, err := s.Issue(42, "customer", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Parse(tok)
	if err != nil || p.UserID != 42 || p.Role != "customer" {
		t.Fatalf("Parse() = %+v, %v", p, err)
	}
}

func TestW3B1_ExpiredTokenIsRejected(t *testing.T) {
	s := NewSigner("test-secret", time.Minute)
	tok, _ := s.Issue(1, "customer", time.Now().Add(-2*time.Minute))
	if _, err := s.Parse(tok); err == nil {
		t.Fatal("expired token must fail")
	}
}

func TestW3B1_TokenSignedWithAnotherAlgorithmIsRejected(t *testing.T) {
	s := NewSigner("test-secret", time.Minute)
	claims := jwt.MapClaims{"sub": "1", "role": "customer", "exp": time.Now().Add(time.Minute).Unix()}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS384, claims).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Parse(tok); err == nil {
		t.Fatal("an HS384 token must fail Parse: the signing method has to be pinned to HS256")
	}
}

func TestW3B1_TokenSignedWithAnotherSecretIsRejected(t *testing.T) {
	tok, _ := NewSigner("attacker-secret", time.Minute).Issue(1, "restaurant", time.Now())
	if _, err := NewSigner("test-secret", time.Minute).Parse(tok); err == nil {
		t.Fatal("a token signed with a different secret must fail Parse")
	}
}
