//go:build w2o1

package e2e_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func cursorsOfAWalk(t *testing.T) []string {
	t.Helper()
	c := newClient(t)
	base := listPath(centreLat, centreLng, 5, 7, nil)
	var out []string
	cursor := ""
	for range 100 {
		path := base
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		r := c.get(path)
		var p page[listed]
		r.JSON(t, &p)
		if r.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", path, r.Status, r.Body)
		}
		if p.NextCursor == "" {
			return out
		}
		out = append(out, p.NextCursor)
		cursor = p.NextCursor
	}
	t.Fatal("walk did not end")
	return nil
}

func altered(c byte) byte {
	const classes = "ABCDEFGHIJKLMNOPQRSTUVWXYZ abcdefghijklmnopqrstuvwxyz 0123456789"
	for _, class := range strings.Fields(classes) {
		if i := strings.IndexByte(class, c); i >= 0 {
			return class[(i+1)%len(class)]
		}
	}
	return 'A'
}

func TestW2O1_SignedCursorsStillWalkEveryRestaurantOnce(t *testing.T) {
	ids, _ := walk(t, newClient(t), listPath(centreLat, centreLng, 5, 7, nil), nil)
	onceEach(t, ids)
	if len(ids) < 40 {
		t.Fatalf("walked %d restaurants, want the seeded 50", len(ids))
	}
}

func TestW2O1_TheSamePositionAlwaysGivesTheSameCursor(t *testing.T) {
	a, b := cursorsOfAWalk(t), cursorsOfAWalk(t)
	if strings.Join(a, ",") != strings.Join(b, ",") {
		t.Fatal("two walks of the same query produced different cursors; sign only the position, not a time or a random nonce")
	}
}

func TestW2O1_ChangingAnyCharacterOfACursorIs400(t *testing.T) {
	c := newClient(t)
	cur := cursorsOfAWalk(t)[1]
	base := listPath(centreLat, centreLng, 5, 7, nil)
	for i := 0; i < len(cur)-2; i++ {
		b := []byte(cur)
		b[i] = altered(b[i])
		r := c.get(base + "&cursor=" + url.QueryEscape(string(b)))
		if r.Status != http.StatusBadRequest {
			t.Fatalf("cursor with character %d changed (%q): %d %s, want 400 invalid_cursor; sign the cursor so clients cannot edit it", i, string(b), r.Status, r.Body)
		}
		expectError(t, r, http.StatusBadRequest, "invalid_cursor", "")
	}
}

func TestW2O1_TruncatedOrExtendedCursorsAre400(t *testing.T) {
	c := newClient(t)
	cur := cursorsOfAWalk(t)[0]
	base := listPath(centreLat, centreLng, 5, 7, nil)
	for _, bad := range []string{cur[:len(cur)/2], cur[:len(cur)-4], cur + "AAAA", cur + cur} {
		expectError(t, c.get(base+"&cursor="+url.QueryEscape(bad)), http.StatusBadRequest, "invalid_cursor", "")
	}
}
