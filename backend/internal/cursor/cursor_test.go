package cursor

import (
	"testing"
	"time"
)

func TestW2B4_CursorRoundTrip(t *testing.T) {
	in := Key{CreatedAt: time.UnixMilli(1_790_000_000_123).UTC(), ID: 42}
	out, err := Decode(Encode(in))
	if err != nil {
		t.Fatal(err)
	}
	if !out.CreatedAt.Equal(in.CreatedAt) || out.ID != in.ID {
		t.Fatalf("got %+v, want %+v", out, in)
	}
}

func TestW2B4_DecodeRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "!!!", "bm90LWEtY3Vyc29y", "MTIzOmFiYw"} {
		if _, err := Decode(s); err == nil {
			t.Fatalf("Decode(%q) should fail", s)
		}
	}
}

func TestW2B4_Page(t *testing.T) {
	type row struct {
		id int64
	}
	rows := []row{{1}, {2}, {3}}
	keyOf := func(r row) Key { return Key{ID: r.id} }

	items, next := Page(rows, 2, keyOf)
	if len(items) != 2 || next == "" {
		t.Fatalf("expected 2 items and a next cursor, got %d, %q", len(items), next)
	}
	items, next = Page(rows, 3, keyOf)
	if len(items) != 3 || next != "" {
		t.Fatalf("expected the last page, got %d, %q", len(items), next)
	}
}
