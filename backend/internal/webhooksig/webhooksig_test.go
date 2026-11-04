//go:build w8o1

package webhooksig

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var (
	secret = []byte("whsec-test-secret-0123456789")
	at     = time.Unix(1_700_000_000, 0)
	body   = []byte(`{"id":"evt_1","type":"payment.captured"}`)
)

func mac(key []byte, payload string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(payload))
	return hex.EncodeToString(m.Sum(nil))
}

func TestW8O1_SignBuildsATimestampedHeaderOverTimestampDotBody(t *testing.T) {
	want := fmt.Sprintf("t=%d,v1=%s", at.Unix(), mac(secret, fmt.Sprintf("%d.%s", at.Unix(), body)))
	if got := Sign(secret, at, body); got != want {
		t.Fatalf("Sign() = %q, want %q", got, want)
	}
}

func TestW8O1_AFreshSignatureVerifies(t *testing.T) {
	if err := Verify(secret, Sign(secret, at, body), body, at.Add(2*time.Second), 5*time.Minute); err != nil {
		t.Fatalf("Verify() = %v, want nil", err)
	}
}

func TestW8O1_ATamperedBodyIsABadSignature(t *testing.T) {
	err := Verify(secret, Sign(secret, at, body), []byte(`{"id":"evt_2"}`), at, 5*time.Minute)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify() = %v, want ErrBadSignature", err)
	}
}

func TestW8O1_AnotherSecretIsABadSignature(t *testing.T) {
	err := Verify([]byte("another-secret-0123456789"), Sign(secret, at, body), body, at, 5*time.Minute)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify() = %v, want ErrBadSignature", err)
	}
}

func TestW8O1_ReplayingAnOldRequestIsStale(t *testing.T) {
	err := Verify(secret, Sign(secret, at, body), body, at.Add(5*time.Minute+time.Second), 5*time.Minute)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("Verify() = %v, want ErrStale", err)
	}
}

func TestW8O1_ATimestampFromTheFutureBeyondTheToleranceIsStale(t *testing.T) {
	err := Verify(secret, Sign(secret, at, body), body, at.Add(-6*time.Minute), 5*time.Minute)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("Verify() = %v, want ErrStale", err)
	}
}

func TestW8O1_TheToleranceBoundaryIsInclusive(t *testing.T) {
	h := Sign(secret, at, body)
	if err := Verify(secret, h, body, at.Add(5*time.Minute), 5*time.Minute); err != nil {
		t.Fatalf("exactly at the tolerance: %v", err)
	}
	if err := Verify(secret, h, body, at.Add(-5*time.Minute), 5*time.Minute); err != nil {
		t.Fatalf("exactly at the tolerance in the future: %v", err)
	}
}

func TestW8O1_SwappingInAFreshTimestampBreaksTheSignature(t *testing.T) {
	old := Sign(secret, at, body)
	forged := fmt.Sprintf("t=%d,%s", at.Add(10*time.Minute).Unix(), strings.SplitN(old, ",", 2)[1])
	err := Verify(secret, forged, body, at.Add(10*time.Minute), 5*time.Minute)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify() = %v, want ErrBadSignature", err)
	}
}

func TestW8O1_AStaleRequestWithABadSignatureReportsTheBadSignature(t *testing.T) {
	err := Verify(secret, "t=1700000000,v1="+strings.Repeat("0", 64), body, at.Add(time.Hour), 5*time.Minute)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify() = %v, want ErrBadSignature", err)
	}
}

func TestW8O1_AnyOfSeveralV1SignaturesMayMatchForKeyRotation(t *testing.T) {
	good := Sign(secret, at, body)
	rotated := fmt.Sprintf("t=%d,v1=%s,%s", at.Unix(), strings.Repeat("a", 64), strings.SplitN(good, ",", 2)[1])
	if err := Verify(secret, rotated, body, at, 5*time.Minute); err != nil {
		t.Fatalf("Verify() = %v, want nil", err)
	}
}

func TestW8O1_MalformedHeadersAreBadHeaders(t *testing.T) {
	cases := map[string]string{
		"empty":         "",
		"no timestamp":  "v1=" + strings.Repeat("a", 64),
		"no signature":  "t=1700000000",
		"bad timestamp": "t=soon,v1=" + strings.Repeat("a", 64),
		"bad hex":       "t=1700000000,v1=zz",
		"no equals":     "t1700000000,v1abc",
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Verify(secret, h, body, at, 5*time.Minute); !errors.Is(err, ErrBadHeader) {
				t.Fatalf("Verify(%q) = %v, want ErrBadHeader", h, err)
			}
		})
	}
}
