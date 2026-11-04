package canteen_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yashdagar/zomato-prep/backend/internal/canteen"
	"github.com/yashdagar/zomato-prep/backend/internal/hours"
)

func TestW7B6_CanteenHasANameAndARealLocation(t *testing.T) {
	p := canteen.Canteen()
	if strings.TrimSpace(p.Name) == "" {
		t.Fatal("the canteen needs a name")
	}
	if p.Lat < -90 || p.Lat > 90 || p.Lng < -180 || p.Lng > 180 || (p.Lat == 0 && p.Lng == 0) {
		t.Fatalf("location %v,%v is not a real coordinate", p.Lat, p.Lng)
	}
}

func TestW7B6_MenuHasEightItemsInThreeCategories(t *testing.T) {
	p := canteen.Canteen()
	cats := map[string]bool{}
	for _, it := range p.Items {
		cats[it.Category] = true
	}
	if len(p.Items) < 8 || len(cats) < 3 {
		t.Fatalf("menu has %d items in %d categories, want at least 8 in 3", len(p.Items), len(cats))
	}
}

func TestW7B6_ItemNamesAreUniqueAndNotBlank(t *testing.T) {
	seen := map[string]bool{}
	for _, it := range canteen.Canteen().Items {
		name := strings.ToLower(strings.TrimSpace(it.Name))
		if name == "" || strings.TrimSpace(it.Category) == "" {
			t.Fatalf("item %+v has a blank name or category", it)
		}
		if seen[name] {
			t.Fatalf("duplicate item %q", it.Name)
		}
		seen[name] = true
	}
}

func TestW7B6_PricesAreWholeRupeesStoredInPaise(t *testing.T) {
	for _, it := range canteen.Canteen().Items {
		if it.PricePaise <= 0 || it.PricePaise > 100000 || it.PricePaise%100 != 0 {
			t.Errorf("%s costs %d paise, want a whole-rupee price between 1 and 1000 rupees in paise", it.Name, it.PricePaise)
		}
	}
}

func TestW7B6_OpeningWindowsAreValidMinutesOfTheDay(t *testing.T) {
	p := canteen.Canteen()
	if len(p.Hours) == 0 {
		t.Fatal("the canteen needs at least one opening window")
	}
	for _, w := range p.Hours {
		if w.OpenMin < 0 || w.OpenMin >= 24*60 || w.CloseMin < 0 || w.CloseMin >= 24*60 || w.OpenMin == w.CloseMin {
			t.Errorf("window %+v is not valid, minutes are 0 to 1439 and open must differ from close", w)
		}
	}
}

func TestW7B6_OpeningWindowsDriveTheWeek2Index(t *testing.T) {
	p := canteen.Canteen()
	idx := hours.Build(p.Slots(7))
	open, closed := -1, -1
	for m := 0; m < 24*60; m++ {
		at := minuteOfDay(m)
		isOpen := len(idx.OpenAt(at)) == 1
		if isOpen && open < 0 {
			open = m
		}
		if !isOpen && closed < 0 {
			closed = m
		}
	}
	if open < 0 {
		t.Fatal("the index never reports the canteen as open")
	}
	if closed < 0 {
		t.Fatal("the canteen is open all 24 hours, use its real opening hours")
	}
	if got := idx.OpenAt(minuteOfDay(open)); len(got) != 1 || got[0] != 7 {
		t.Fatalf("open at minute %d = %v, want [7]", open, got)
	}
}

func TestW7B6_SlotsCarryTheRestaurantID(t *testing.T) {
	for _, s := range canteen.Canteen().Slots(42) {
		if s.RestaurantID != 42 {
			t.Fatalf("slot %+v does not carry restaurant id 42", s)
		}
	}
}

func TestW7B6_HasARestaurantUserAndARiderWithDistinctEmails(t *testing.T) {
	roles := map[string]int{}
	emails := map[string]bool{}
	for _, p := range canteen.Canteen().People {
		roles[p.Role]++
		if !strings.Contains(p.Email, "@") || emails[strings.ToLower(p.Email)] {
			t.Errorf("person %+v has a bad or repeated email", p)
		}
		emails[strings.ToLower(p.Email)] = true
		switch p.Role {
		case "customer", "restaurant", "rider":
		default:
			t.Errorf("person %+v has role %q, want customer, restaurant or rider", p, p.Role)
		}
	}
	if roles["restaurant"] < 1 || roles["rider"] < 1 {
		t.Fatalf("roles = %v, want at least one restaurant and one rider", roles)
	}
}

func TestW7B6_SeedDataCarriesNoPasswords(t *testing.T) {
	typ := reflect.TypeOf(canteen.Person{})
	for i := 0; i < typ.NumField(); i++ {
		if n := strings.ToLower(typ.Field(i).Name); strings.Contains(n, "pass") || strings.Contains(n, "secret") || strings.Contains(n, "token") {
			t.Fatalf("Person has a %s field, passwords belong in the environment, never in seed data", typ.Field(i).Name)
		}
	}
}

func TestW7B6_CanteenIsDeterministic(t *testing.T) {
	if !reflect.DeepEqual(canteen.Canteen(), canteen.Canteen()) {
		t.Fatal("Canteen() returned different data on two calls")
	}
}

func minuteOfDay(m int) time.Time {
	return time.Date(2026, 11, 4, m/60, m%60, 0, 0, time.UTC)
}
