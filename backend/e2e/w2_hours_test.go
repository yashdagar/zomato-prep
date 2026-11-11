package e2e_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	hoursLat = 12.9716
	hoursLng = 77.5946
)

var kolkata = func() *time.Location {
	l, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		panic(err)
	}
	return l
}()

func refreshInterval(t *testing.T) time.Duration {
	t.Helper()
	d, err := time.ParseDuration(os.Getenv("TEST_HOURS_REFRESH_INTERVAL"))
	if err != nil {
		t.Skip("run through make test, which starts the API with HOURS_REFRESH_INTERVAL")
	}
	return d
}

type slot struct{ open, close int }

func hm(h, m int) int { return h*60 + m }

func withSlots(t *testing.T, db *sql.DB, name string, slots ...slot) int64 {
	t.Helper()
	id := insertRestaurant(t, db, name, hoursLat, hoursLng, true)
	for _, s := range slots {
		mustExec(t, db, "INSERT INTO open_slots (restaurant_id, open_min, close_min) VALUES (?, ?, ?)", id, s.open, s.close)
	}
	return id
}

func ist(h, m int) time.Time {
	return time.Date(2026, 1, 5, h, m, 0, 0, kolkata)
}

// openAt lists the hours test area at a moment and returns is_open by id.
func openAt(t *testing.T, at *time.Time) map[int64]bool {
	t.Helper()
	extra := url.Values{}
	if at != nil {
		extra.Set("at", at.Format(time.RFC3339))
	}
	_, pages := walk(t, newClient(t), listPath(hoursLat, hoursLng, 1, 50, extra), nil)
	out := map[int64]bool{}
	for _, p := range pages {
		for _, it := range p {
			if it.IsOpen == nil {
				t.Fatalf("restaurant %d has no boolean is_open", it.ID)
			}
			out[it.ID] = *it.IsOpen
		}
	}
	return out
}

// expectOpen waits up to three refresh intervals for the API to load the
// hours a test wrote, then checks every moment against it.
func expectOpen(t *testing.T, id int64, moments map[time.Time]bool) {
	t.Helper()
	deadline := time.Now().Add(3*refreshInterval(t) + time.Second)
	var last string
	for {
		var wrong []string
		for at, want := range moments {
			at := at
			if got := openAt(t, &at)[id]; got != want {
				wrong = append(wrong, fmt.Sprintf("%s is_open=%v want %v", at.Format("15:04 MST"), got, want))
			}
		}
		if len(wrong) == 0 {
			return
		}
		last = strings.Join(wrong, "; ")
		if time.Now().After(deadline) {
			t.Fatalf("restaurant %d: %s", id, last)
		}
		time.Sleep(refreshInterval(t) / 4)
	}
}

func TestW2B7_EveryListedRestaurantHasABooleanIsOpen(t *testing.T) {
	r := newClient(t).get(listPath(centreLat, centreLng, 5, 50, nil))
	var p page[listed]
	r.JSON(t, &p)
	if r.Status != http.StatusOK || len(p.Items) == 0 {
		t.Fatalf("list: %d %s", r.Status, r.Body)
	}
	for _, it := range p.Items {
		if it.IsOpen == nil {
			t.Fatalf("restaurant %d has no boolean is_open", it.ID)
		}
	}
}

func TestW2B7_ASlotIncludesItsOpeningMinuteAndExcludesItsClosingMinute(t *testing.T) {
	id := withSlots(t, apiDB(t), "Lunch and dinner", slot{hm(11, 0), hm(23, 0)})
	expectOpen(t, id, map[time.Time]bool{
		ist(10, 59): false, ist(11, 0): true, ist(15, 30): true, ist(22, 59): true, ist(23, 0): false, ist(3, 0): false,
	})
}

func TestW2B7_AtIsReadAsKolkataLocalTimeWhateverOffsetItCarries(t *testing.T) {
	id := withSlots(t, apiDB(t), "Offsets", slot{hm(11, 0), hm(12, 0)})
	tokyo := time.FixedZone("JST", 9*3600)
	expectOpen(t, id, map[time.Time]bool{
		time.Date(2026, 1, 5, 5, 30, 0, 0, time.UTC): true,
		time.Date(2026, 1, 5, 5, 29, 0, 0, time.UTC): false,
		time.Date(2026, 1, 5, 14, 45, 0, 0, tokyo):   true,
		time.Date(2026, 1, 5, 11, 15, 0, 0, tokyo):   false,
	})
}

func TestW2B7_AnOvernightSlotRunsPastMidnight(t *testing.T) {
	id := withSlots(t, apiDB(t), "Late night", slot{hm(18, 0), hm(3, 0)})
	expectOpen(t, id, map[time.Time]bool{
		ist(17, 59): false, ist(18, 0): true, ist(23, 59): true, ist(0, 0): true, ist(2, 59): true, ist(3, 0): false, ist(12, 0): false,
	})
}

func TestW2B7_ARestaurantCanHaveSeveralSlots(t *testing.T) {
	id := withSlots(t, apiDB(t), "Split shift", slot{hm(8, 0), hm(10, 0)}, slot{hm(12, 0), hm(15, 0)}, slot{hm(22, 0), hm(1, 0)})
	expectOpen(t, id, map[time.Time]bool{
		ist(9, 0): true, ist(11, 0): false, ist(13, 0): true, ist(16, 0): false, ist(23, 0): true, ist(0, 30): true, ist(1, 0): false,
	})
}

func TestW2B7_NoSlotsMeansClosedAndAWholeDaySlotMeansAlwaysOpen(t *testing.T) {
	db := apiDB(t)
	closed := withSlots(t, db, "No hours")
	always := withSlots(t, db, "Always open", slot{0, 1440})
	moments := map[time.Time]bool{ist(0, 0): true, ist(12, 0): true, ist(23, 59): true}
	expectOpen(t, always, moments)
	for at := range moments {
		at := at
		if openAt(t, &at)[closed] {
			t.Fatalf("a restaurant with no slots is open at %s", at.Format("15:04"))
		}
	}
}

func TestW2B7_HundredsOfRestaurantsAreEachEvaluatedOnTheirOwn(t *testing.T) {
	db := apiDB(t)
	want := map[int64]bool{}
	for i := range 150 {
		var slots []slot
		switch {
		case i%3 == 0:
			slots = []slot{{hm(10, 0), hm(12, 0)}}
		case i%3 == 1:
			slots = []slot{{hm(23, 0), hm(11, 0)}}
		default:
			slots = []slot{{hm(12, 0), hm(13, 0)}}
		}
		id := withSlots(t, db, fmt.Sprintf("Bulk %03d", i), slots...)
		want[id] = i%3 == 0
	}
	at := ist(11, 30)
	deadline := time.Now().Add(3*refreshInterval(t) + time.Second)
	for {
		got := openAt(t, &at)
		var wrong []int64
		for id, w := range want {
			if got[id] != w {
				wrong = append(wrong, id)
			}
		}
		if len(wrong) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of 150 restaurants have the wrong is_open at 11:30, for example %d; check how restaurants past the first 64 map to bits", len(wrong), wrong[0])
		}
		time.Sleep(refreshInterval(t) / 4)
	}
}

func TestW2B7_ChangedHoursShowUpWithinARefreshInterval(t *testing.T) {
	db := apiDB(t)
	id := withSlots(t, db, "Changing hours", slot{hm(9, 0), hm(17, 0)})
	expectOpen(t, id, map[time.Time]bool{ist(10, 0): true, ist(18, 0): false})
	mustExec(t, db, "UPDATE open_slots SET open_min = ?, close_min = ? WHERE restaurant_id = ?", hm(17, 0), hm(20, 0), id)
	expectOpen(t, id, map[time.Time]bool{ist(10, 0): false, ist(18, 0): true})
}

func TestW2B7_AFailedRefreshKeepsTheLastGoodHours(t *testing.T) {
	db := apiDB(t)
	id := withSlots(t, db, "Survives outages", slot{hm(9, 0), hm(17, 0)})
	expectOpen(t, id, map[time.Time]bool{ist(10, 0): true})
	mustExec(t, db, "RENAME TABLE open_slots TO open_slots_offline")
	restored := false
	restore := func() {
		if !restored {
			restored = true
			mustExec(t, db, "RENAME TABLE open_slots_offline TO open_slots")
		}
	}
	t.Cleanup(restore)
	time.Sleep(3 * refreshInterval(t))
	at := ist(10, 0)
	if !openAt(t, &at)[id] {
		t.Fatal("after refreshes failed because open_slots was unreadable, the restaurant shows closed; keep serving the last index that loaded")
	}
	restore()
}

func TestW2B7_ListingDoesNotReadOpenSlotsOnEveryRequest(t *testing.T) {
	const requests = 20
	qs := statements(t, func() {
		for range requests {
			if r := newClient(t).get(listPath(centreLat, centreLng, 5, 20, nil)); r.Status != http.StatusOK {
				t.Fatalf("list: %d %s", r.Status, r.Body)
			}
		}
	})
	n := 0
	for _, q := range qs {
		if strings.Contains(strings.ToLower(q), "open_slots") {
			n++
		}
	}
	if n >= requests/4 {
		t.Fatalf("%d list requests read open_slots %d times; build the index in the background and read it from memory", requests, n)
	}
}

func TestW2B7_AnAtThatIsNotRFC3339Is400(t *testing.T) {
	c := newClient(t)
	for _, at := range []string{"yesterday", "2026-13-01T00:00:00Z", "2026-01-05 10:00", "1767600000"} {
		extra := url.Values{"at": {at}}
		expectError(t, c.get(listPath(centreLat, centreLng, 5, 5, extra)), http.StatusBadRequest, "invalid_parameter", "at")
	}
}

func TestW2B7_WithoutAtIsOpenIsForNow(t *testing.T) {
	db := apiDB(t)
	now := time.Now().In(kolkata)
	m := now.Hour()*60 + now.Minute()
	openNow := withSlots(t, db, "Open now", slot{(m + 1440 - 60) % 1440, (m + 60) % 1440})
	closedNow := withSlots(t, db, "Closed now", slot{(m + 120) % 1440, (m + 180) % 1440})
	deadline := time.Now().Add(3*refreshInterval(t) + time.Second)
	for {
		got := openAt(t, nil)
		if got[openNow] && !got[closedNow] {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("without at: open-now restaurant is_open=%v, closed-now restaurant is_open=%v", got[openNow], got[closedNow])
		}
		time.Sleep(refreshInterval(t) / 4)
	}
}
