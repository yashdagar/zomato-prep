package order

import (
	"errors"
	"testing"
)

func TestW3B5_Next(t *testing.T) {
	tests := []struct {
		name    string
		from    Status
		ev      Event
		actor   Role
		want    Status
		wantErr any
	}{
		{"restaurant accepts a placed order", Placed, Accept, Restaurant, Accepted, nil},
		{"customer cancels while placed", Placed, Cancel, Customer, Cancelled, nil},
		{"customer cannot cancel while preparing", Preparing, Cancel, Customer, "", &IllegalTransitionError{}},
		{"customer cannot accept", Placed, Accept, Customer, "", &ForbiddenError{}},
		{"rider cannot deliver before pickup", Ready, Deliver, Rider, "", &IllegalTransitionError{}},
		{"full happy path end", PickedUp, Deliver, Rider, Delivered, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Next(tc.from, tc.ev, tc.actor)
			switch want := tc.wantErr.(type) {
			case nil:
				if err != nil || got != tc.want {
					t.Fatalf("Next() = %q, %v; want %q, nil", got, err, tc.want)
				}
			case *IllegalTransitionError:
				var e *IllegalTransitionError
				if !errors.As(err, &e) {
					t.Fatalf("want IllegalTransitionError, got %v", err)
				}
			case *ForbiddenError:
				var e *ForbiddenError
				if !errors.As(err, &e) {
					t.Fatalf("want ForbiddenError, got %v", err)
				}
			default:
				t.Fatalf("bad test case %T", want)
			}
		})
	}
}

func TestW3B5_TerminalStatesHaveNoExits(t *testing.T) {
	for tr := range next {
		if IsTerminal(tr.from) {
			t.Fatalf("terminal state %s has an outgoing transition", tr.from)
		}
	}
}
