//go:build w2o1

package cursor

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var (
	secretA = []byte("0123456789abcdef0123456789abcdef")
	secretB = []byte("fedcba9876543210fedcba9876543210")
)

func newSigner(t *testing.T, secret []byte) *Signer {
	t.Helper()
	s, err := NewSigner(secret)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func flip(s string, i int) string {
	b := []byte(s)
	if b[i] == 'A' {
		b[i] = 'B'
	} else {
		b[i] = 'A'
	}
	return string(b)
}

func TestW2O1_SignedCursorRoundTrip(t *testing.T) {
	s := newSigner(t, secretA)
	in := Key{CreatedAt: time.UnixMilli(1_790_000_000_123).UTC(), ID: 42}
	out, err := s.Decode(s.Encode(in))
	if err != nil {
		t.Fatal(err)
	}
	if !out.CreatedAt.Equal(in.CreatedAt) || out.ID != in.ID {
		t.Fatalf("got %+v, want %+v", out, in)
	}
}

func TestW2O1_SignedCursorRoundTripWithZeroTime(t *testing.T) {
	s := newSigner(t, secretA)
	out, err := s.Decode(s.Encode(Key{ID: 7}))
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != 7 || !out.CreatedAt.IsZero() {
		t.Fatalf("got %+v, want ID 7 and the zero time", out)
	}
}

func TestW2O1_TokenIsVersionedAndDeterministic(t *testing.T) {
	s := newSigner(t, secretA)
	a, b := s.Encode(Key{ID: 5}), s.Encode(Key{ID: 5})
	if a != b {
		t.Fatalf("the same key produced two tokens: %q and %q", a, b)
	}
	if !strings.HasPrefix(a, "v1.") {
		t.Fatalf("token %q should start with the version prefix v1.", a)
	}
	if a == s.Encode(Key{ID: 6}) {
		t.Fatal("different keys produced the same token")
	}
}

func TestW2O1_TamperedTokenIsRejected(t *testing.T) {
	s := newSigner(t, secretA)
	tok := s.Encode(Key{CreatedAt: time.UnixMilli(1_790_000_000_123).UTC(), ID: 42})
	for _, i := range []int{0, 1, 2, 3, len(tok) / 2, len(tok) - 8} {
		if _, err := s.Decode(flip(tok, i)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("changing character %d of %q: err = %v, want ErrInvalid", i, tok, err)
		}
	}
}

func TestW2O1_TruncatedOrExtendedTokenIsRejected(t *testing.T) {
	s := newSigner(t, secretA)
	tok := s.Encode(Key{ID: 42})
	for _, bad := range []string{tok[:len(tok)-4], tok + "AAAA", tok + ".", "." + tok} {
		if _, err := s.Decode(bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Decode(%q): err = %v, want ErrInvalid", bad, err)
		}
	}
}

func TestW2O1_UnsignedLegacyCursorIsRejected(t *testing.T) {
	s := newSigner(t, secretA)
	forged := Encode(Key{ID: 1})
	if _, err := s.Decode(forged); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Decode(%q): err = %v, want ErrInvalid", forged, err)
	}
}

func TestW2O1_TokenSignedWithAnotherSecretIsRejected(t *testing.T) {
	tok := newSigner(t, secretA).Encode(Key{ID: 42})
	if _, err := newSigner(t, secretB).Decode(tok); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestW2O1_UnknownVersionIsRejected(t *testing.T) {
	s := newSigner(t, secretA)
	tok := s.Encode(Key{ID: 42})
	for _, v := range []string{"v2.", "v0.", "V1.", "x."} {
		if _, err := s.Decode(v + strings.TrimPrefix(tok, "v1.")); !errors.Is(err, ErrInvalid) {
			t.Fatalf("version prefix %q: err = %v, want ErrInvalid", v, err)
		}
	}
}

func TestW2O1_GarbageIsRejected(t *testing.T) {
	s := newSigner(t, secretA)
	for _, bad := range []string{"", "!!!", "v1.", "v1..", "v1.a.b", "bm90LWEtY3Vyc29y", "MTIzOmFiYw"} {
		if _, err := s.Decode(bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Decode(%q): err = %v, want ErrInvalid", bad, err)
		}
	}
}

func TestW2O1_ShortSecretIsRefused(t *testing.T) {
	for _, secret := range [][]byte{nil, []byte("short"), make([]byte, 15)} {
		if _, err := NewSigner(secret); err == nil {
			t.Fatalf("NewSigner with a %d byte secret should fail", len(secret))
		}
	}
	if _, err := NewSigner(make([]byte, 16)); err != nil {
		t.Fatalf("a 16 byte secret should be accepted: %v", err)
	}
}

func TestW2O1_SignedPage(t *testing.T) {
	s := newSigner(t, secretA)
	type row struct{ id int64 }
	rows := []row{{1}, {2}, {3}}
	keyOf := func(r row) Key { return Key{ID: r.id} }

	items, next := SignedPage(s, rows, 2, keyOf)
	if len(items) != 2 || next == "" {
		t.Fatalf("expected 2 items and a next cursor, got %d, %q", len(items), next)
	}
	k, err := s.Decode(next)
	if err != nil || k.ID != 2 {
		t.Fatalf("next cursor decodes to %+v, %v, want the key of the last item on the page", k, err)
	}
	items, next = SignedPage(s, rows, 3, keyOf)
	if len(items) != 3 || next != "" {
		t.Fatalf("expected the last page, got %d, %q", len(items), next)
	}
}
